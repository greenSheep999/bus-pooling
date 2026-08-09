package topup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Janitor 定期扫 pending_topup 卡单·把中间态推进或转 pending_manual。
//
// 场景（09-transactions §6）：
//   - initial 超过 initialTimeout（默认 5min）· gateway 还没调过 · 双表 expire
//   - gateway_creating 卡后 · 用 client_order_id 反查 gateway（POST replay CreatePayment）：
//     - 反查确认 gateway 已建（200 replay / 201 新建）→ 回填 gateway_payment_id + 推 gateway_ordered
//     - 反查失败（网络错 / 5xx / POST 4xx **含 404**）→ 累计 poll_fail_count · 到上限转 pending_manual · **绝不 expire**
//     - 反查能力缺失（snapshot 空 / callback 未装配）→ 立即 pending_manual · **绝不 expire**
//     · 特别注意：POST 404 ≠ "payment 不存在"·POST 是**写**语义·404 是端点错
//   - gateway_ordered 超过 pollAfter · **主动** GET gateway 覆盖 webhook 丢失（P0-3 修）：
//     - state=settled → 触发 MarkPaid + 一路推 completed
//     - state=pending → 保留（下轮再 poll）
//     - state=expired/cancelled/failed → 双表 expire
//     - **GET 404** → 该 gateway_payment_id 明确不存在（读语义）→ 双表 expire
//     - poll 网络错 → 累计 poll_fail_count · 不 expire · 到上限转 manual
//   - gateway_paid 卡多轮（webhook 到了但 MarkPaid 内部失败）· 重试 MarkPaid
//   - credited 卡多轮（MarkPaid 成功但没推 completed）· 直推 completed
//
// **"未知不等于失败"**：poll 失败 / 反查能力缺失 / POST 404 都不能推断为 expired。
// 只有三条**明确读语义**信号才当作"确认无单"·允许 expire：
//   1. GET /payments/{id} 返 404（读语义 · 该 payment id 明确不存在）
//   2. gateway 显式状态 = expired / cancelled / failed
//   3. initial 超时（本地压根没发 CreatePayment）
type Janitor struct {
	orders    *Store
	pending   *PendingStore
	completer PendingCompleter // MarkPaid 的注入（janitor 不该自己调 Store 的 MarkPaid · 用抽象接口）
	poller    GatewayPoller    // nil 时不 poll（DRY_RUN / gateway 未装配）
	interval  time.Duration
	// 卡单阈值 · 从 updated_at 起算
	initialTimeout        time.Duration // initial → expired（默认 5min）
	gatewayOrderedTimeout time.Duration // gateway_ordered → 查 gateway·超时 expired（默认 15min · 与 TTL 一致）
	gatewayPaidTimeout    time.Duration // gateway_paid → 重试 MarkPaid（默认 2min · 内部错快恢复）
	creditedTimeout       time.Duration // credited → completed（默认 30s · 已经入账·就一步推进）
	// pollAfter · gateway_ordered 卡多久开始 poll gateway（默认 60s · P0-3）
	pollAfter time.Duration
	// batchLimit · 单次扫描处理上限（防止一轮阻塞太久）。转 pending_manual 的
	// 计数逻辑在 pending_topup.poll_fail_count 列 · 上限见 maxCreatingPollFails。
	batchLimit int
	log        *slog.Logger
}

// PendingCompleter · janitor 想推 gateway_paid → credited 时调 · 用 Store.MarkPaid。
// 抽象出来让测试能 mock。
type PendingCompleter interface {
	MarkPaid(ctx context.Context, orderID string) (Order, error)
}

// GatewayPoller · P0-3 修：janitor 主动查 gateway 状态 · 覆盖 webhook 丢失。
// 抽象方法 · 返回 gateway 侧当前 state / kind + gateway_payment_id。
// 生产装 paymentgw.Client 的适配 · 测试 mock。
type GatewayPoller interface {
	// PollByGatewayPaymentID · 按 gateway 侧的 payment id 查。
	PollByGatewayPaymentID(ctx context.Context, gatewayPaymentID string) (state string, err error)
	// FindByClientOrderID · P0 gateway_creating 反查用：本地无 gateway_payment_id
	// 时·用我方发的 client_order_id 反查 gateway 是否已建。找不到返 (nil, ErrNotFound)。
	FindByClientOrderID(ctx context.Context, clientOrderID string) (*GatewayPayment, error)
}

// GatewayPayment · Poller 反查返回的最小视图（跟 paymentgw.Payment 对齐）
type GatewayPayment struct {
	ID          string
	State       string // pending | settled | expired | cancelled | failed
	CheckoutURL string
	QRContent   string
}

// ErrGatewayNotFound · gateway 侧没找到这个 client_order_id
var ErrGatewayNotFound = errors.New("topup: gateway 侧未找到 client_order_id")

type JanitorConfig struct {
	Orders                *Store
	Pending               *PendingStore
	Completer             PendingCompleter
	Poller                GatewayPoller // P0-3 · nil 时退化为原来的"不 poll"行为
	Interval              time.Duration
	InitialTimeout        time.Duration
	GatewayOrderedTimeout time.Duration
	GatewayPaidTimeout    time.Duration
	CreditedTimeout       time.Duration
	// PollAfter · gateway_ordered 卡多久开始 poll（默认 60s · 大于典型 webhook 到达）
	PollAfter  time.Duration
	BatchLimit int
	Logger     *slog.Logger
}

func NewJanitor(cfg JanitorConfig) *Janitor {
	j := &Janitor{
		orders:                cfg.Orders,
		pending:               cfg.Pending,
		completer:             cfg.Completer,
		poller:                cfg.Poller,
		interval:              cfg.Interval,
		initialTimeout:        cfg.InitialTimeout,
		gatewayOrderedTimeout: cfg.GatewayOrderedTimeout,
		gatewayPaidTimeout:    cfg.GatewayPaidTimeout,
		creditedTimeout:       cfg.CreditedTimeout,
		pollAfter:             cfg.PollAfter,
		batchLimit:            cfg.BatchLimit,
		log:                   cfg.Logger,
	}
	if j.pollAfter <= 0 {
		j.pollAfter = 60 * time.Second
	}
	if j.interval <= 0 {
		j.interval = 30 * time.Second
	}
	if j.initialTimeout <= 0 {
		j.initialTimeout = 5 * time.Minute
	}
	if j.gatewayOrderedTimeout <= 0 {
		j.gatewayOrderedTimeout = 15 * time.Minute
	}
	if j.gatewayPaidTimeout <= 0 {
		j.gatewayPaidTimeout = 2 * time.Minute
	}
	if j.creditedTimeout <= 0 {
		j.creditedTimeout = 30 * time.Second
	}
	if j.batchLimit <= 0 {
		j.batchLimit = 50
	}
	if j.log == nil {
		j.log = slog.Default()
	}
	return j
}

// Run 阻塞循环·ctx 结束即返回。
func (j *Janitor) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.SweepOnce(ctx)
		}
	}
}

// SweepReport 一轮扫描统计。
type SweepReport struct {
	InitialExpired        int
	GatewayCreatingReco   int // P0 · gateway_creating 卡后 · 反查 gateway 侧已建单 → 回填 gateway_payment_id
	GatewayCreatingExpire int // P0 · gateway_creating 卡后 · 反查确认 gateway 侧未建 → 双表 expire
	GatewayCreatingManual int // P0 · gateway_creating 卡后 · 反查失败多轮 → 转 pending_manual（不 expire）
	GatewayOrderedExpired int
	GatewayOrderedPolled  int // P0-3 · poll gateway 主动推进的行数
	GatewayPaidRetried    int
	CreditedCompleted     int
	PollFailed            int // P1 · poll gateway 失败计数（不算处理·下轮再试）
	Failed                int
}

// SweepOnce 扫一轮。
func (j *Janitor) SweepOnce(ctx context.Context) SweepReport {
	var r SweepReport
	now := time.Now().UTC()

	// initial 太久 · 极可能 gateway.CreatePayment 崩溃后 · 转 expired
	// （gateway 没建·就没 webhook 会来）
	rows, err := j.pending.FindStuck(ctx, now.Add(-j.initialTimeout), j.batchLimit)
	if err != nil {
		j.log.Error("topup janitor 扫卡单失败", "err", err)
		return r
	}
	for _, p := range rows {
		if ctx.Err() != nil {
			return r
		}
		switch p.Status {
		case PendingInitial:
			// gateway 还没调·转 expired（不进 pending_manual · 简单 · 乘客重试新单即可）
			// **P1-1 修**：同步 topup_order.status='expired'（不然双表分叉：pending=expired · order=pending）
			if err := j.expireBoth(ctx, p.ID, p.TopupOrderID, PendingInitial); err != nil {
				r.Failed++
				j.log.Warn("initial 双表过期失败", "id", p.ID, "err", err)
				continue
			}
			r.InitialExpired++
			j.log.Info("pending_topup initial 超时 → expired（双表同步）",
				"id", p.ID, "order", p.TopupOrderID)
		case PendingGatewayCreating:
			// **P0 修**：CreatePayment 与 AttachGateway 之间崩溃 · 用 client_order_id
			// 反查 gateway 是否已建单：
			//   - 反查成功·gateway 已有单 → 回填 gateway_payment_id + 推 gateway_ordered
			//   - 反查成功·gateway 没有单 → 双表 expire（可能是 CreatePayment 请求就没到）
			//   - 反查失败（网络 / 超时 / 5xx）→ 累计 poll_fail_count · 多轮失败转 pending_manual
			//     **绝不 expire** —— 未知不等于失败（P1 修）
			if p.UpdatedAt.After(now.Add(-j.pollAfter)) {
				continue
			}
			outcome := j.recoverGatewayCreating(ctx, p)
			switch outcome {
			case "recovered":
				r.GatewayCreatingReco++
			case "expired":
				r.GatewayCreatingExpire++
			case "manual":
				r.GatewayCreatingManual++
			case "retry":
				r.PollFailed++
			}
		case PendingGatewayOrdered:
			// **P0-3 修**：gateway_ordered 卡 > pollAfter 时·先 poll gateway 兜底
			//    · webhook 丢失场景：gateway 已 settled 但我方没收到 · 主动 poll 触发 MarkPaid
			//    · gateway 也没 settled 且已过 TTL · 才 expire 双表
			if p.UpdatedAt.After(now.Add(-j.pollAfter)) {
				continue // 还没到 poll 阈值
			}
			// 先 poll · 覆盖 webhook 丢失
			gwState, polled := j.tryPoll(ctx, p.TopupOrderID)
			if polled {
				switch gwState {
				case "settled":
					// gateway 已收款 · 我方走 MarkPaid（兜底）· 状态一路推 completed
					if j.completer != nil {
						if _, err := j.completer.MarkPaid(ctx, p.TopupOrderID); err != nil && !errors.Is(err, ErrOrderNotPending) {
							j.log.Warn("poll 后 MarkPaid 失败", "id", p.ID, "err", err)
							continue
						}
						if err := j.pending.EnsureAtLeast(ctx, p.TopupOrderID, PendingCompleted); err != nil {
							j.log.Warn("poll 后推 completed 失败", "id", p.ID, "err", err)
						}
					}
					r.GatewayOrderedPolled++
					j.log.Info("pending_topup poll 发现 gateway settled · 已入账",
						"id", p.ID, "order", p.TopupOrderID)
					continue
				case "expired", "cancelled", "failed":
					// gateway 也过期 · 双表同步 expire（下面 expireBoth 走）
				default:
					// pending / observed_only · gateway 还没到账 ·
					// **poll 成功说明 gateway 还留着这单** · 不 expire · 下轮再 poll
					continue
				}
			} else {
				// **P1 修**（审计发现）：poll 失败 ≠ gateway 侧过期 · 不能 TTL expire。
				// 三种情况区分：
				//   - poller = nil（未装配）：走老 TTL expire（DRY_RUN 模式）
				//   - poller 装了但 gateway 报错 / 超时：累计 poll_fail_count · 到上限转 manual
				//   - TTL 未到：continue 等下轮
				if j.poller == nil {
					if p.UpdatedAt.After(now.Add(-j.gatewayOrderedTimeout)) {
						continue
					}
					// poller 未装配的 fallback expire · 继续下面 expireBoth 走
				} else {
					// poller 装了但报错 · 累计失败计数 · **不 expire**
					attempts, ferr := j.pending.IncrPollFailCount(ctx, p.ID)
					if ferr != nil {
						j.log.Warn("gateway_ordered 累计 poll_fail_count 失败",
							"id", p.ID, "err", ferr)
						r.PollFailed++
						continue
					}
					if attempts >= maxCreatingPollFails {
						reason := fmt.Sprintf("gateway_ordered poll 反复失败 %d 次 · 转人工",
							attempts)
						if merr := j.pending.MarkManual(ctx, p.ID, reason); merr != nil {
							j.log.Warn("gateway_ordered 转 manual 失败", "id", p.ID, "err", merr)
						}
					} else {
						j.log.Warn("gateway_ordered poll 失败·下轮再试",
							"id", p.ID, "attempts", attempts)
					}
					r.PollFailed++
					continue
				}
			}
			if err := j.expireBoth(ctx, p.ID, p.TopupOrderID, PendingGatewayOrdered); err != nil {
				r.Failed++
				j.log.Warn("gateway_ordered 双表过期失败", "id", p.ID, "err", err)
				continue
			}
			r.GatewayOrderedExpired++
			j.log.Info("pending_topup gateway_ordered 超时 → expired（双表同步）",
				"id", p.ID, "order", p.TopupOrderID)
		case PendingGatewayPaid:
			// webhook 到了但 MarkPaid 卡了 · 重试
			if p.UpdatedAt.After(now.Add(-j.gatewayPaidTimeout)) {
				continue
			}
			if j.completer == nil {
				j.log.Warn("gateway_paid 卡单但 completer=nil", "id", p.ID)
				continue
			}
			if _, err := j.completer.MarkPaid(ctx, p.TopupOrderID); err != nil {
				// ErrOrderNotPending 说明另一路径已 paid · 推 credited
				if errors.Is(err, ErrOrderNotPending) {
					_ = j.pending.Advance(ctx, p.ID, PendingGatewayPaid, PendingCredited)
					r.GatewayPaidRetried++
					continue
				}
				r.Failed++
				j.log.Warn("gateway_paid 重试 MarkPaid 失败", "id", p.ID, "err", err)
				// 下轮再试 · MarkPaid 内部错（wallet ledger 冲突 / DB 短暂锁）通常几轮内自愈；
				// 与 gateway_creating / gateway_ordered 不同 · 这里 **不累计 poll_fail_count** —
				// 失败原因是本地 DB 错·跟外部反查不是一类·靠 gatewayPaidTimeout 反复触发即可。
				continue
			}
			if err := j.pending.Advance(ctx, p.ID, PendingGatewayPaid, PendingCredited); err != nil && !errors.Is(err, ErrStaleTransition) {
				r.Failed++
				j.log.Warn("gateway_paid → credited 失败", "id", p.ID, "err", err)
				continue
			}
			r.GatewayPaidRetried++
			j.log.Info("pending_topup gateway_paid 重试 MarkPaid 成功", "id", p.ID)
		case PendingCredited:
			// wallet 已入账·就差状态推 completed
			if p.UpdatedAt.After(now.Add(-j.creditedTimeout)) {
				continue
			}
			if err := j.pending.Advance(ctx, p.ID, PendingCredited, PendingCompleted); err != nil && !errors.Is(err, ErrStaleTransition) {
				r.Failed++
				j.log.Warn("credited → completed 失败", "id", p.ID, "err", err)
				continue
			}
			r.CreditedCompleted++
			j.log.Info("pending_topup credited → completed", "id", p.ID)
		}
	}
	return r
}

// expireBoth · 双表同步过期 · 防"pending=expired · order=pending"分叉（P1-1）
func (j *Janitor) expireBoth(ctx context.Context, pendingID, orderID string, from PendingStatus) error {
	_, err := j.pending.ExpireBoth(ctx, pendingID, orderID, from)
	return err
}

// recoverGatewayCreating · P0 修 · 用 client_order_id 反查 gateway。
// outcome:
//   - "recovered"  · gateway 已有单 · 回填 gateway_payment_id + 推 gateway_ordered
//   - "expired"    · gateway 侧没有 · 双表 expire（乘客可重试新单）
//   - "manual"     · poll 失败到上限 · 转 pending_manual（**不 expire** · 保守·让人工查）
//   - "retry"      · poll 失败但未到上限 · 累计后下轮再试
const maxCreatingPollFails = 5

func (j *Janitor) recoverGatewayCreating(ctx context.Context, p Pending) string {
	if j.poller == nil {
		// 无 poller · 反查不了·当作"未建"处理 · 转 pending_manual 保守（不 expire · 可能已建）
		if err := j.pending.MarkManual(ctx, p.ID,
			"gateway_creating 卡住 · 无 gateway poller 无法反查"); err != nil {
			j.log.Warn("gateway_creating 转 manual 失败", "id", p.ID, "err", err)
		}
		return "manual"
	}
	gwp, err := j.poller.FindByClientOrderID(ctx, p.TopupOrderID)
	if err != nil {
		if errors.Is(err, ErrGatewayFindUnavailable) {
			// 反查能力**不存在** · 未知不等于失败·直接转 pending_manual 保守
			// gateway 未提供 by-client_order_id 反查端点前·所有 gateway_creating
			// 卡单都走这条 · 让运营手工核对（**不 expire** · 不丢单）。
			if err := j.pending.MarkManual(ctx, p.ID,
				"gateway_creating 卡住 · gateway 未提供反查端点 · 需人工核对"); err != nil {
				j.log.Warn("gateway_creating 转 manual 失败", "id", p.ID, "err", err)
				return "retry"
			}
			return "manual"
		}
		if errors.Is(err, ErrGatewayNotFound) {
			// **仅当反查方明确使用只读语义端点**（未来 gateway 加"只读 client-order 反查"）
			// 才可能走到这条分支 · 明确 404 = 该 client_order_id 不存在（读语义）· expire 安全。
			//
			// 当前 LoadRequestSnapshot 走 POST /payments 幂等重发 · POST 404 是端点错·
			// 不映射到这个 sentinel（在 gwpoller.go FindByClientOrderID 里保证不 map）·
			// 所以当前生产**不会**触发这条 · 但保留分支给未来"只读反查"接口用。
			if eerr := j.expireBoth(ctx, p.ID, p.TopupOrderID, PendingGatewayCreating); eerr != nil {
				j.log.Warn("gateway_creating expire 失败", "id", p.ID, "err", eerr)
				return "retry"
			}
			j.log.Info("gateway_creating 只读反查确认无单 → expired（双表同步）",
				"id", p.ID, "order", p.TopupOrderID)
			return "expired"
		}
		// **网络 / 5xx / 超时 / POST 4xx（含 404）**：**绝不 expire** —— 未知不等于失败
		attempts, ferr := j.pending.IncrPollFailCount(ctx, p.ID)
		if ferr != nil {
			j.log.Warn("gateway_creating 累计 poll_fail_count 失败",
				"id", p.ID, "err", ferr)
			return "retry"
		}
		if attempts >= maxCreatingPollFails {
			reason := fmt.Sprintf("gateway_creating 反查 %d 次失败 · 最后一次 err=%v",
				attempts, err)
			if merr := j.pending.MarkManual(ctx, p.ID, reason); merr != nil {
				j.log.Warn("gateway_creating 转 manual 失败", "id", p.ID, "err", merr)
			}
			return "manual"
		}
		j.log.Warn("gateway_creating 反查失败·下轮再试",
			"id", p.ID, "attempts", attempts, "err", err)
		return "retry"
	}
	// gateway 已有单 · 回填 + 推 gateway_ordered
	if err := j.orders.AttachGateway(ctx, p.TopupOrderID, gwp.ID, gwp.CheckoutURL, gwp.QRContent); err != nil {
		j.log.Warn("gateway_creating 回填 AttachGateway 失败", "id", p.ID, "err", err)
		return "retry"
	}
	if err := j.pending.EnsureAtLeast(ctx, p.TopupOrderID, PendingGatewayOrdered); err != nil {
		j.log.Warn("gateway_creating 推 gateway_ordered 失败", "id", p.ID, "err", err)
		return "retry"
	}
	// gateway 侧可能已经 settled · 让后续 sweep 一并处理
	// 但此处若 gwp.State='settled' · 立刻走一次 MarkPaid 兜底
	if gwp.State == "settled" && j.completer != nil {
		if _, err := j.completer.MarkPaid(ctx, p.TopupOrderID); err != nil && !errors.Is(err, ErrOrderNotPending) {
			j.log.Warn("gateway_creating 反查已 settled · MarkPaid 失败", "id", p.ID, "err", err)
			return "retry"
		}
		_ = j.pending.EnsureAtLeast(ctx, p.TopupOrderID, PendingCompleted)
	}
	j.log.Info("gateway_creating 反查恢复 · 回填 gateway_payment_id",
		"id", p.ID, "order", p.TopupOrderID, "gateway_state", gwp.State)
	return "recovered"
}

// tryPoll · gateway_ordered 卡时主动查 gateway。
//
// 三态返回（审计要求）：
//   - state != ""      · poll 成功·state = settled / pending / expired / cancelled / failed
//   - state == "expired" 也可能来自"gateway 明确 404"（PollByGatewayPaymentID 内部翻译）
//   - polled=false     · poller 未装配 / order 无 gateway_payment_id / poll 错误
//                       上层不能推断"确认无单"·只能累计 poll_fail_count
func (j *Janitor) tryPoll(ctx context.Context, orderID string) (state string, polled bool) {
	if j.poller == nil || j.orders == nil {
		return "", false
	}
	o, err := j.orders.getBy(ctx, orderID)
	if err != nil || o.GatewayPaymentID == "" {
		return "", false
	}
	st, err := j.poller.PollByGatewayPaymentID(ctx, o.GatewayPaymentID)
	if err != nil {
		// gateway 明确 404 · 视同"expired"（gateway 侧无单）· 允许双表 expire
		if errors.Is(err, ErrGatewayNotFound) {
			return "expired", true
		}
		j.log.Warn("topup janitor poll gateway 失败", "order", orderID, "err", err)
		return "", false
	}
	return st, true
}

// 内部 helper（跟 topup.go 里的 formatTime / parseTime 共用）
var _ = fmt.Sprintf
