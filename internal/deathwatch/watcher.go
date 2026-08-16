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
	// refillDecider · 第三刀 · Decide 前置判据 · nil = 老行为(直接 puller)
	// 装配层实现 · 避免 deathwatch → decider 硬依赖
	refillDecider RefillDecide
	// refillEnqueuer · 挂 stockwatch 桥 · nil = Enqueue 分支只 reschedule
	// 装配层实现 · 避免 deathwatch → stockwatch 硬依赖
	refillEnqueuer RefillEnqueuer
	// deathNotifier · 1e-2 · markDead 触发 · 装配层桥到 webhookout · nil = 不通知
	deathNotifier DeathNotifier
	// refundNotifier · 1e-2 · RefundOnce 每笔退款触发 · 装配层桥到 webhookout · nil = 不通知
	refundNotifier RefundNotifier
}

// DeathNotifier · 号死事件通知(装配层桥到 webhookout · 避免包依赖循环)。
//
// **只在**某 bus 全灭时才发对外 webhook(all_keys_dead) · 装配层判"某 bus 是否全灭" ·
// 单号死不发(单号死走 credential.dead · 阶段 1e-2 简化不发单号事件)。
type DeathNotifier interface {
	OnCredentialDead(ctx context.Context, credentialLedgerID string, vendorID string)
}

// RefundNotifier · 质保退款事件通知。
//
// RefundOnce 里每笔退款(每号)一次调用 · plan 里每个 passenger 一份。
type RefundNotifier interface {
	OnRefundIssued(ctx context.Context, passengerID string, amount int64, credentialLedgerID, busID string)
}

// SetDeathNotifier · 装配层注入。
func (w *Watcher) SetDeathNotifier(n DeathNotifier) {
	if w == nil {
		return
	}
	w.deathNotifier = n
}

// SetRefundNotifier · 装配层注入。
func (w *Watcher) SetRefundNotifier(n RefundNotifier) {
	if w == nil {
		return
	}
	w.refundNotifier = n
}

// SetRefillDecider · 装配层注入 · 必须在 Start 前调用。
func (w *Watcher) SetRefillDecider(d RefillDecide) {
	if w == nil {
		return
	}
	w.refillDecider = d
}

// SetRefillEnqueuer · 装配层注入 · 必须在 Start 前调用。
func (w *Watcher) SetRefillEnqueuer(e RefillEnqueuer) {
	if w == nil {
		return
	}
	w.refillEnqueuer = e
}

// RefillPuller · 号死后触发新一轮拉号的抽象
//
// 实现方 decider.Orchestrator.Pull 的一个薄封装 · 从 pending_refill 一条记录
// 重构造 decider.PullInput 并调用。
//
// 返 (fulfilled bool, err error)：
//
//	fulfilled=true · err=nil  → 补车成功 · pending_refill 标 fulfilled
//	fulfilled=false · err=nil → vendor 缺货 · 挂号或跳过 · 保 pending 等下轮
//	fulfilled=?    · err!=nil → 硬错 · attempts++ · 3 次后 expired
type RefillPuller interface {
	Refill(ctx context.Context, req RefillRequest) (fulfilled bool, err error)
}

// RefillRequest · 从 pending_refill 一条记录拿出的字段 · 上层装配 puller 用
type RefillRequest struct {
	RefillID    string // pending_refill.id · 幂等键
	PassengerID string
	BusID       string // 空 = 单独提取
	Count       int
	VendorID    string // 可空 · 让 decider auto-pick
	// v1d-3 · 从 Decide 传下来的护栏字段
	MaxUnitPrice int64 // microunit · 0 = 不限
}

// RefillDecide · 第三刀 · Decide 接口注入(避免 deathwatch → decider 硬依赖)
//
// 装配层实现:调 decider.Decide(source=death_refill) 并返 verdict。
// nil = 老行为(直接 puller.Refill · 不过决策器) · 保 1a-1c 回归。
//
// 语义:
//   - Verdict=Reject → RefillTick 跳过这条 · 标 skipped · 记 reason
//   - Verdict=Pull   → RefillTick 用 verdict 里的 Count/VendorID/MaxPrice 调 puller
//   - Verdict=Enqueue → RefillTick 调 refillEnqueuer 挂 stockwatch · 挂完标 fulfilled
type RefillDecide interface {
	Decide(ctx context.Context, req RefillRequest) RefillVerdict
}

// RefillEnqueuer · 装配层注入 stockwatch.Enqueue 桥。
//
// **挂意图不预冻结** —— fire 时走 decider.Pull 完整钱包事务。
// nil = Enqueue 分支只 reschedule(旧行为·会无限 pending)。
type RefillEnqueuer interface {
	Enqueue(ctx context.Context, req RefillEnqueueRequest) error
}

// RefillEnqueueRequest · deathwatch → stockwatch 桥的入参
type RefillEnqueueRequest struct {
	RefillID        string
	PassengerID     string
	BusID           string
	Count           int
	PreferredVendor string
	MaxUnitPrice    int64
}

// RefillVerdict · Decide 输出给 RefillTick 的结果
type RefillVerdict struct {
	Action       RefillAction
	Reason       string
	PullCount    int
	PullVendor   string
	PullMaxPrice int64
}

// RefillAction · 三态
type RefillAction int

const (
	RefillReject RefillAction = iota
	RefillPull
	RefillEnqueue
)

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
		// 落 credential_usage_snapshot（§12.5a · 前端号详情进度条数据源）·
		// 顺手兜底 credential_ledger.subscription（NULL 时才写）·
		// **不写 credits_used** —— 那列由 markDead 一次性快照（§12.5c）
		w.refreshUsageSnapshot(ctx, cred)
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
	// A · 死号那一刻的用量快照（docs/06-db-schema §12.5c）· 落 credential_ledger.credits_used
	// 之后**永不变更**（快照语义）· 用于事后归因"死时用到多少"、算平均寿命对应用量。
	// housepool 侧 Balance.currentUsage 单位是"vendor 侧积分"（float）· 转 microunit 存。
	var usedMicro int64
	if cred.Balance != nil {
		usedMicro = int64(cred.Balance.CurrentUsage * 1_000_000)
	}
	res, err := w.db.ExecContext(ctx, `
		UPDATE credential_ledger
		   SET status = 'dead',
		       dead_at = ?,
		       death_source = 'housepool_probe',
		       credits_used = ?
		 WHERE kiro_rs_credential_id = ?
		   AND status = 'alive'
	`, formatTime(now), usedMicro, uint64(cred.ID))
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

	// **1e-2** · 通知对外 webhook(装配层判是否 bus 全灭 · 只发 all_keys_dead)
	// 通知失败只 log · 不影响标死本身
	if w.deathNotifier != nil {
		// 查我方 credential.id + vendor 让装配层能反查 bus + 判全灭
		var ourID, vendorID string
		if err := w.db.QueryRowContext(ctx,
			`SELECT id, COALESCE(vendor_id, '') FROM credential_ledger WHERE kiro_rs_credential_id = ?`,
			uint64(cred.ID)).Scan(&ourID, &vendorID); err == nil && ourID != "" {
			w.deathNotifier.OnCredentialDead(ctx, ourID, vendorID)
		}
	}

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
