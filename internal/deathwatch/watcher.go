// Package deathwatch 后台扫号池 · 把死号在 credential_ledger 打上 dead_at + death_source。
//
// 本包**只**负责标死 · 不做补车（那是 1d 才做的事，sprint-1a-backend Iss #12）·
// 不做 webhook 上报 · 不做数据统计。
//
// 判死规则钉死不能弄错（docs/08-housepool-contract.md §DisabledReason 判据）：
//
//   - `Manual` → 跳过（我方主动 disable：拉号记录待派 / handoff 待确认 / 成员挂起）
//   - `Suspended` / `QuotaExceeded` / `InvalidRefreshToken` → 判死
//   - 其余 disabled → TestCredential 复核，返 error 才判死
//   - `disabled=false` → 不动（本轮不做主动抽查探活；那会打爆号池）
//
// **不能**只凭 `disabled=true` 就判死 —— `record-<pid>` 里的号按设计就是 disabled，
// 那样会把全部待派号误判成死号。
package deathwatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// Pool 是 deathwatch 用到的号池能力子集。窄接口方便 mock。
type Pool interface {
	ListCredentials(ctx context.Context, filter housepool.CredentialFilter) ([]housepool.Credential, *housepool.PoolSnapshot, error)
	TestCredential(ctx context.Context, id housepool.CredentialID) error
}

// Watcher 后台扫号池 · 单 process 一个实例。
type Watcher struct {
	db   *sql.DB
	pool Pool
	// interval 两轮扫描间隔 · 0 = 默认 5 分钟（sprint-1a-backend Iss #12）
	interval time.Duration
	// probeTimeout 单次 TestCredential 复核的超时 · 0 = 默认 10s
	probeTimeout time.Duration
	log          *slog.Logger
	// now 便于测试注入时钟
	now func() time.Time
	// refunds 质保退款的库操作 · nil = 不跑退款（老装配 / 测试）
	refunds RefundStore
	// refillPuller · Step 2 真调拉号· nil = 走 Step 1 只 log 不真拉
	// 装配层实现 · 避免 deathwatch → decider 硬依赖
	refillPuller RefillPuller
}

// RefillPuller · 号死后触发新一轮拉号的抽象
//
// 实现方 decider.Orchestrator.Pull 的一个薄封装 · 从 pending_refill 一条记录
// 重构造 decider.PullInput 并调用。
//
// 返 (fulfilled bool, err error)：
//   fulfilled=true · err=nil  → 补车成功 · pending_refill 标 fulfilled
//   fulfilled=false · err=nil → vendor 缺货 · 挂号或跳过 · 保 pending 等下轮
//   fulfilled=?    · err!=nil → 硬错 · attempts++ · 3 次后 expired
type RefillPuller interface {
	Refill(ctx context.Context, req RefillRequest) (fulfilled bool, err error)
}

// RefillRequest · 从 pending_refill 一条记录拿出的字段 · 上层装配 puller 用
type RefillRequest struct {
	RefillID     string             // pending_refill.id · 幂等键
	PassengerID  string
	BusID        string             // 空 = 单独提取
	Count        int
	VendorID     string             // 可空 · 让 decider auto-pick
}

// Config 装配参数。零值全部走默认。
type Config struct {
	DB           *sql.DB
	Pool         Pool
	Interval     time.Duration
	ProbeTimeout time.Duration
	Logger       *slog.Logger
	// Now 只在测试里覆盖；生产用默认 time.Now
	Now func() time.Time
	// Refunds 质保退款库操作 · nil = 不跑退款（1c 起装 NewSQLRefundStore(db)）
	Refunds RefundStore
	// RefillPuller · Step 2 · 号死后真调拉号· nil = Step 1 只 log
	RefillPuller RefillPuller
}

// New 构造 Watcher。DB 和 Pool 必须非空。
func New(cfg Config) *Watcher {
	w := &Watcher{
		db:           cfg.DB,
		pool:         cfg.Pool,
		interval:     cfg.Interval,
		probeTimeout: cfg.ProbeTimeout,
		log:          cfg.Logger,
		now:          cfg.Now,
		refunds:      cfg.Refunds,
		refillPuller: cfg.RefillPuller,
	}
	if w.interval <= 0 {
		w.interval = 5 * time.Minute
	}
	if w.probeTimeout <= 0 {
		w.probeTimeout = 10 * time.Second
	}
	if w.log == nil {
		w.log = slog.Default()
	}
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	return w
}

// Run 阻塞循环 · ctx 结束即返回。
//
// 装配位置：main.serve 里跟 janitor 一样 `go w.Run(ctx)`（sprint-1a-backend Iss #12）。
func (w *Watcher) Run(ctx context.Context) {
	// 启动时先扫一次 —— 别等 5 分钟才第一次工作
	w.sweepAndRefund(ctx)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.sweepAndRefund(ctx)
		}
	}
}

// sweepAndRefund 标死号 + 跑质保退款（00 §7.5 规则 B）。
//
// 顺序有讲究：先标死再退款 —— 退款要求号已经是 dead 状态。
// 退款失败不影响标死（各自独立事务）。
func (w *Watcher) sweepAndRefund(ctx context.Context) {
	w.SweepOnce(ctx)
	if w.refunds == nil {
		return
	}
	rep := w.RefundOnce(ctx, 100)
	if rep.Refunded > 0 || rep.Errors > 0 {
		w.log.Info("质保退款一轮",
			"scanned", rep.Scanned, "refunded", rep.Refunded,
			"skipped", rep.Skipped, "errors", rep.Errors,
			"credits", rep.TotalCredits)
	}
}

// SweepReport 一次扫描的统计（测试断言用）。
type SweepReport struct {
	// Scanned 号池返回的号总数（含 disabled）
	Scanned int
	// Skipped Manual / disabled=false 等不做处理的
	Skipped int
	// Probed 走了 TestCredential 复核的
	Probed int
	// MarkedDead 本轮新标死的（credential_ledger 有更新的行数）
	MarkedDead int
	// AlreadyDead 号池说死了但 credential_ledger 里已经是 dead —— 幂等 no-op
	AlreadyDead int
	// UnknownCredential 号池里有但 credential_ledger 找不到 —— 记 warn 不 fail
	UnknownCredential int
	// Errors 扫描过程中记录到但没让整轮挂掉的错（探活失败 / SQL 失败等）
	Errors int
	// RefillEnqueued P6 · 本轮标死后成功塞进 pending_refill 的条数（幂等 skip 不计）
	RefillEnqueued int
}

// SweepOnce 拉一次号池全量、按判据分流、批量标死。
//
// 号池全量拉在阶段 1a 号量不大的前提下没问题；号量上去后需要按 group 分批或者
// 直接切成 push 模型（webhook_in），那是后续版本的事。
func (w *Watcher) SweepOnce(ctx context.Context) SweepReport {
	var rep SweepReport

	// IncludeDisabled=true —— 死号一定是 disabled，不带这个开关就啥也扫不到
	creds, _, err := w.pool.ListCredentials(ctx, housepool.CredentialFilter{IncludeDisabled: true})
	if err != nil {
		w.log.Error("deathwatch 拉号池列表失败", "err", err)
		rep.Errors++
		return rep
	}
	rep.Scanned = len(creds)

	for i := range creds {
		if ctx.Err() != nil {
			return rep
		}
		cred := &creds[i]
		w.classify(ctx, cred, &rep)
	}
	return rep
}

// classify 按 §DisabledReason 判据把一个号分到"跳过 / 判死 / 探活"三条路里。
func (w *Watcher) classify(ctx context.Context, cred *housepool.Credential, rep *SweepReport) {
	// 活着的号不参与本轮 —— 主动探活会打爆号池，靠后台被动信号即可
	if !cred.Disabled {
		rep.Skipped++
		return
	}

	// Manual 是我方主动 disable 的语义（拉号记录待派 / handoff 待确认 / 成员挂起）·
	// **绝不能**判死，否则整批待派号会被误标（Iss #12 DoD 里的回归点）
	if cred.DisabledReason == housepool.ReasonManual {
		rep.Skipped++
		return
	}

	// 明确失效枚举：直接判死
	if housepool.IsDeadReason(cred.DisabledReason) {
		w.markDead(ctx, cred, rep)
		return
	}

	// 其余（TooManyFailures / TooManyRefreshFailures / AutoThrottled / InvalidConfig / 其他未知值）
	// 需要 TestCredential 复核 —— 号池会自愈，光看 reason 会误判
	rep.Probed++
	probeCtx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	err := w.pool.TestCredential(probeCtx, cred.ID)
	cancel()
	if err == nil {
		// 复核活着 · 有可能是号池自愈中，等下一轮再看
		rep.Skipped++
		return
	}
	w.log.Info("deathwatch 探活失败", "cred_id", uint64(cred.ID), "reason", cred.DisabledReason, "probe_err", err)
	w.markDead(ctx, cred, rep)
}

// markDead 把这个号在 credential_ledger 里标为 dead。
//
// **条件 UPDATE**（status='alive'）防止对已经 dead 或 handed_off 的行重复写 —— 死号是终态。
// 用 kiro_rs_credential_id 匹配，因为号池只知道自己那侧的 u64 id。
func (w *Watcher) markDead(ctx context.Context, cred *housepool.Credential, rep *SweepReport) {
	now := w.now().UTC()
	res, err := w.db.ExecContext(ctx, `
		UPDATE credential_ledger
		   SET status = 'dead',
		       dead_at = ?,
		       death_source = 'housepool_probe'
		 WHERE kiro_rs_credential_id = ?
		   AND status = 'alive'
	`, formatTime(now), uint64(cred.ID))
	if err != nil {
		w.log.Error("deathwatch 标死 SQL 失败", "cred_id", uint64(cred.ID), "err", err)
		rep.Errors++
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// 两种情况：credential_ledger 里根本没这条 · 或者已经 dead/handed_off
		// 分开报告：找不到 = 数据不一致（要告警），已死 = 正常幂等
		if exists, err := w.credentialExists(ctx, cred.ID); err != nil {
			w.log.Warn("deathwatch 查 credential_ledger 失败", "cred_id", uint64(cred.ID), "err", err)
			rep.Errors++
		} else if !exists {
			w.log.Warn("deathwatch 号池有但账里没", "cred_id", uint64(cred.ID), "reason", cred.DisabledReason)
			rep.UnknownCredential++
		} else {
			rep.AlreadyDead++
		}
		return
	}
	rep.MarkedDead++
	w.log.Info("deathwatch 标死", "cred_id", uint64(cred.ID), "reason", cred.DisabledReason)

	// **P6 · 自动补车**（2026-08-14）：标死后往 pending_refill 塞一条待补记录 ·
	// worker 消费真拉（Step 1 只 log · Step 2 真 fire · 见 refill.go 注释）。
	// 幂等 · 失败只 log 不影响标死本身（补车是增值 · 死号该标还得标）。
	//
	// 拿我方 credential_ledger.id · 得先反查（markDead 是用 kiro_rs_credential_id 匹配）
	var ourID string
	if err := w.db.QueryRowContext(ctx,
		`SELECT id FROM credential_ledger WHERE kiro_rs_credential_id = ?`,
		uint64(cred.ID)).Scan(&ourID); err == nil && ourID != "" {
		if inserted, rerr := w.enqueueRefill(ctx, ourID, "dead"); rerr != nil {
			w.log.Warn("塞 pending_refill 失败（不影响标死）",
				"cred_id", uint64(cred.ID), "err", rerr)
		} else if inserted {
			rep.RefillEnqueued++
		}
	}
}

// credentialExists 查这个 kiro_rs_credential_id 是否在账里（不管什么状态）。
func (w *Watcher) credentialExists(ctx context.Context, id housepool.CredentialID) (bool, error) {
	var one int
	err := w.db.QueryRowContext(ctx,
		`SELECT 1 FROM credential_ledger WHERE kiro_rs_credential_id = ?`,
		uint64(id)).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("查 credential_ledger: %w", err)
}

// timeLayout / formatTime 跟 decider / wallet / passenger 里那份保持一致。
//
// 定宽 ISO-8601 而非 RFC3339Nano —— 字符串比较排序稳定（wallet.go 有同样注释）。
const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }
