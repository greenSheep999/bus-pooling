// Package stockwatch · 抢号链缺货挂单 · restock 事件唤醒 fire。
//
// **只 auto 模式挂** —— 用户明确指定 vendor 时缺货直接失败（不代抢）。
//
// 三个入口方法：
//   - Enqueue · 挂单 · decider.Pull 里 auto 模式缺货时调
//   - Notify  · 通知有 restock 事件 · 三源都走这里
//     (vendor webhook new_keys / 探针 stock-delta / xi8 signals)
//   - Sweep   · TTL 扫过期 · janitor 定时调
//
// 幂等保护 · 三个层次：
//  1. intent_id 是主键 · 一 intent 只挂一条
//  2. status 转换用 conditional UPDATE · 防并发多次 fire
//  3. fire 时用 pull_intent.client_order_id · vendor 侧同 order_id 重放
//
// **重要 · 单价上限保护**：Enqueue 落 max_unit_price · Notify 唤醒前校验当前价 · 涨价
// 超上限 · 不 fire · 继续等（涨价可能是稀缺信号 · 也可能是 vendor 侧临时波动）。
package stockwatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Watcher · stockwatch 包对外门面。
type Watcher struct {
	db     *sql.DB
	logger *slog.Logger

	// Firer 是 fire 时的下游 · 通常是 decider.Orchestrator.PullByWatcher（下一个 commit 里加）
	// 允许 nil · Notify 只做 DB 状态转换（测试用）
	firer Firer

	// defaultTTL 挂单默认存活时长 · 到期自动 → expired 退款
	// 决定"用户能等多久" · 太短用户体验差 · 太长占款
	defaultTTL time.Duration

	// mode · 运营态管理器 · Notify 前判断该不该 fire
	// nil 时 Notify 一律 fire（老装配 / 测试 · 保守默认）
	mode *ModeMgr

	// kill · 应急急停（文件哨兵 KILL_PULLS）· 开着时 Notify 全部 skip · 最高优先级
	kill *FileFlag

	// turbo · 人工强制抢（文件哨兵 TURBO_ON）· 开着时**无视 mode 自动判断** · 强制按
	// tight 抢。场景：上游连续几天缺货 · 自动判断可能算成 cool（supply 长期 0 · demand
	// 也不高）· 但运营者知道"还要用 · 有货就抢" · 手工按住。
	turbo *FileFlag

	// 后台 sweeper 运行时 · StartSweeper 后填
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}
}

// Firer · fire 时打这个 · 拿挂单上下文触发一次真拉号
//
// 独立接口避免循环依赖（stockwatch → decider → stockwatch 会环）·
// decider 提供实现（FireWatcher method）· main.go 装配时注入。
type Firer interface {
	// FireWatcher · 用挂单上下文走一次 vendor Purchase
	// 返 nil = 抢到（fulfilled）· ErrStillNoStock = 又抢空（继续 watching）·
	// 其他 err = 硬失败（expired · 释放冻结）
	FireWatcher(ctx context.Context, w WatcherRow) error
}

// WatcherRow · 一条挂单的完整上下文 · fire 时 decider 靠它重建拉号请求。
//
// **自包含** —— 不依赖任何前置表行存在。这样 fire 时不用回查 · 也不会因为
// 前置行被清理导致 fire 失败。
type WatcherRow struct {
	ID             string
	PassengerID    string
	BusID          string // 空 = 提取（record group）
	TargetGroup    string // bus-<id> | record-<pid>
	VendorID       string
	Region         string
	ClientOrderID  string // vendor 幂等键 · 重放不重复扣
	MaxUnitPrice   int64  // 0 = 不限
	Count          int
	ReservedAmount int64
}

// ErrStillNoStock · Firer 返这个说明 vendor 又缺货了 · Watcher 保持 watching 等下次
var ErrStillNoStock = errors.New("stockwatch: fire 后 vendor 仍缺货")

// **为什么这里没有"聚合源 blocked 就别 fire"的 guard**（2026-08-14 用户拍板）：
// xi8 只做**内部对账 / 参考**（看它怎么对齐上游）· **绝不介入采购决策**。能不能买
// 一律以**直接打 vendor** 的响应为准（缺货 vendor 自己返 ErrStillNoStock）· 不看 xi8。
// 让 xi8 veto 真实购买 = 把内部参考源塞进钱路 · xi8 misalign 时会拦掉本可成交的单。

type Config struct {
	DB         *sql.DB
	Firer      Firer
	Logger     *slog.Logger
	DefaultTTL time.Duration // 挂单默认存活时长 · 默认 10min
	Mode       *ModeMgr      // 运营态管理器 · nil = 一律 fire（保守）
	Kill       *FileFlag     // 急停开关（KILL_PULLS 文件）· nil = 从不急停
	Turbo      *FileFlag     // 人工强制抢（TURBO_ON 文件）· nil = 从不强制
}

func New(cfg Config) *Watcher {
	if cfg.DB == nil {
		return nil
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ttl := cfg.DefaultTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Watcher{
		db:         cfg.DB,
		firer:      cfg.Firer,
		logger:     logger,
		defaultTTL: ttl,
		mode:       cfg.Mode,
		kill:       cfg.Kill,
		turbo:      cfg.Turbo,
	}
}

// SetFirer · 装配后补设 Firer · 解决构造环。
//
// **为什么需要**：Watcher 要 decider 当 Firer（fire 时走拉号）· decider 要 Watcher
// 当 Enqueuer（缺货时挂单）· 互相依赖没法一次构造完。装配顺序：
//
//	Watcher{Firer: nil} → decider{Enqueuer: watcher} → watcher.SetFirer(decider)
//
// 只在 main.go 装配时调一次 · 之后不再变（不加锁 —— Start 之前调完）。
func (w *Watcher) SetFirer(f Firer) {
	if w == nil {
		return
	}
	w.firer = f
}

// EnqueueParams · Enqueue 的入参
type EnqueueParams struct {
	ID             string        // 挂单 id · 空则自动生成 uuid v7
	PassengerID    string        // 谁在等 · 必填
	BusID          string        // 进哪辆车 · 空 = 提取（record group）
	TargetGroup    string        // bus-<id> | record-<pid> · 必填
	VendorID       string        // auto-pick 选中的 vendor · 必填
	Region         string        // 特定 region · 空 = 任意
	ClientOrderID  string        // vendor 幂等键 · 必填 · fire 时复用
	Count          int           // 要几个 · 必填
	MaxUnitPrice   int64         // 涨价保护 · microunit · 0 = 不限
	ReservedAmount int64         // 挂单时已冻结的钱 · expired 时释放
	TTL            time.Duration // 存活时长 · 0 用 defaultTTL
}

// Enqueue · 挂单 · decider 缺货时 auto 模式调 · 返挂单 id。
//
// 幂等：(vendor_id, client_order_id) 有 UNIQUE · 同一 client_order_id 重挂视为
// refresh（更新 TTL 和参数 · 保留 fire_count 防 spam retry）。
func (w *Watcher) Enqueue(ctx context.Context, p EnqueueParams) (string, error) {
	if w == nil || w.db == nil {
		return "", errors.New("stockwatch: Watcher 未装配")
	}
	if p.PassengerID == "" || p.VendorID == "" || p.TargetGroup == "" ||
		p.ClientOrderID == "" || p.Count < 1 {
		return "", fmt.Errorf(
			"stockwatch: 参数非法 passenger=%q vendor=%q group=%q order=%q count=%d",
			p.PassengerID, p.VendorID, p.TargetGroup, p.ClientOrderID, p.Count)
	}
	id := p.ID
	if id == "" {
		u, err := uuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("stockwatch: 生成 id: %w", err)
		}
		id = u.String()
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = w.defaultTTL
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)

	_, err := w.db.ExecContext(ctx, `
		INSERT INTO stock_watcher
			(id, passenger_id, bus_id, target_group, vendor_id, region,
			 client_order_id, max_unit_price, count, reserved_amount,
			 started_at, expires_at, status, fire_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'watching', 0)
		ON CONFLICT(vendor_id, client_order_id) DO UPDATE SET
			region          = excluded.region,
			count           = excluded.count,
			max_unit_price  = excluded.max_unit_price,
			reserved_amount = excluded.reserved_amount,
			expires_at      = excluded.expires_at,
			status          = 'watching'
	`, id, p.PassengerID, nullIfEmpty(p.BusID), p.TargetGroup,
		p.VendorID, nullIfEmpty(p.Region), p.ClientOrderID,
		nullIfZeroInt64(p.MaxUnitPrice), p.Count, p.ReservedAmount,
		now.Format(time.RFC3339), expires.Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("stockwatch: enqueue: %w", err)
	}
	w.logger.Info("stockwatch: 缺货挂单",
		"id", id, "passenger", p.PassengerID, "vendor", p.VendorID,
		"count", p.Count, "ttl", ttl, "max_unit_price", p.MaxUnitPrice)
	return id, nil
}

// Cancel · 撤单 · 用户主动取消时调 · 幂等
func (w *Watcher) Cancel(ctx context.Context, id string) error {
	if w == nil || w.db == nil {
		return nil
	}
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'cancelled'
		 WHERE id = ? AND status = 'watching'`, id)
	return err
}

// NotifyParams · Notify 入参
type NotifyParams struct {
	VendorID string // 哪家 vendor restock 了
	Region   string // 具体 region · 空 = 全 region 都算命中
	Count    int    // 这一波新到几个（可选 · 用于 log · 不影响筛选）
	Source   string // webhook / stock_delta / manual  (xi8 不走这条·数据补齐用)
}

// Notify · 三源都调这个 · 有 restock 事件时打过来。
//
// 逻辑：
//  1. **急停 check**：kill switch engaged → 全 skip · log
//  2. **运营态 check**：mode 决定这个 source 该不该 fire
//     - stock_delta 源（我方探针 60s 采样对比）· 只有 ModeTight 才 fire
//     号少时 webhook 常慢/漏 · 探针主动去问是关键补位（docs/15 §3.1）
//     - webhook 源（vendor 主动 push）· ModeTight + ModeBalance 都 fire · 最快信号别浪费
//     - manual 源（CLI 手工调试）· 任何 mode 都 fire · 运营强制路径
//     - ModeCool · 探针/webhook 都不 fire（库存充足 · 用户来了现打）
//     - **xi8 不 fire** · xi8 只写 vendor_probe_zone / vendor_dispatch 数据补齐 · 从不 Notify
//  3. 找 (vendor_id, status='watching') 按 started_at 队列（先挂先抢）
//  4. 每条 · conditional UPDATE 抢到 fired 状态 · 抢到才 fire
//  5. fire 委托到 Firer.FireByIntent（走 decider · 幂等 by client_order_id）
//  6. fire 返 nil → fulfilled；ErrStillNoStock → 回退 watching 等下次；其他 err → expired
//
// **不批量 fire 全部** —— 一次 restock 事件通常只放几个号 · fire 太多会让后到的 intent
// 打空。当前策略：按 count 匹配 · 一次事件只 fire 前 N 条（N = event.count · 未来可调）。
func (w *Watcher) Notify(ctx context.Context, p NotifyParams) error {
	if w == nil || w.db == nil || w.firer == nil {
		return nil
	}
	// 急停 check · 最高优先级
	if w.kill != nil && w.kill.Engaged() {
		w.logger.Info("stockwatch: 急停开关开着 · Notify 全部 skip",
			"vendor", p.VendorID, "source", p.Source)
		return nil
	}
	// 运营态 check · 决定这个 source 要不要 fire
	//
	// **用 INFO 不用 Debug**：生产默认 INFO 级 · 这条是"为什么没 fire"的唯一线索。
	// 打 Debug 的话运维看到"webhook 收到了但没抢"会完全不知道是被 mode 挡了还是代码断了。
	// 频率可控：只有真有 restock 事件时才走到这里（不是每秒轮询）。
	if !w.sourceShouldFire(p.Source) {
		w.logger.Info("stockwatch: 运营态跳过 · 只观测不抢",
			"vendor", p.VendorID, "source", p.Source,
			"mode", w.currentMode(),
			"hint", "cool=无排队需求 · balance=只 webhook 抢 · tight=都抢")
		return nil
	}
	// 查 watching 队列 · 按 started_at 顺序（先挂先抢 · 公平）
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, passenger_id, COALESCE(bus_id, ''), target_group,
		       vendor_id, COALESCE(region, ''), client_order_id,
		       COALESCE(max_unit_price, 0), count, reserved_amount
		  FROM stock_watcher
		 WHERE vendor_id = ? AND status = 'watching'
		   AND (region IS NULL OR ? = '' OR region = ?)
		   AND expires_at > ?
		 ORDER BY started_at ASC`,
		p.VendorID, p.Region, p.Region,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("stockwatch: 查 watching: %w", err)
	}
	var cands []WatcherRow
	for rows.Next() {
		var c WatcherRow
		if err := rows.Scan(
			&c.ID, &c.PassengerID, &c.BusID, &c.TargetGroup,
			&c.VendorID, &c.Region, &c.ClientOrderID,
			&c.MaxUnitPrice, &c.Count, &c.ReservedAmount,
		); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(cands) == 0 {
		return nil
	}
	w.logger.Info("stockwatch: 收到 restock 通知 · 唤醒队列",
		"vendor", p.VendorID, "region", p.Region,
		"event_count", p.Count, "queue_size", len(cands),
		"source", p.Source)

	// 逐个 fire · 按队列顺序 · 中间某个 ErrStillNoStock 就停（vendor 又空了 · 后面不用打）
	fired := 0
	for _, c := range cands {
		// conditional UPDATE 抢 fired 状态 · 防并发多次 fire
		res, err := w.db.ExecContext(ctx, `
			UPDATE stock_watcher
			   SET status = 'fired',
			       fired_at = ?,
			       fired_reason = ?,
			       fire_count = fire_count + 1
			 WHERE id = ? AND status = 'watching'`,
			time.Now().UTC().Format(time.RFC3339), p.Source, c.ID)
		if err != nil {
			w.logger.Warn("stockwatch: 抢 fired 状态失败", "id", c.ID, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // 别的 goroutine 已经抢了
		}

		fireErr := w.firer.FireWatcher(ctx, c)
		switch {
		case fireErr == nil:
			// 抢到号 · 标 fulfilled（decider 里已经把号导进 group + 结算）
			w.markFulfilled(ctx, c.ID)
			fired++
		case errors.Is(fireErr, ErrStillNoStock):
			// 又空了 · 回退 watching 等下次事件 · fire_count 已 +1 便于观察
			w.rewindToWatching(ctx, c.ID)
			w.logger.Info("stockwatch: fire 后仍缺货 · 回退等下次", "id", c.ID)
			// 停 · vendor 已经又空 · 后面的不用试
			return nil
		default:
			// 硬失败（涨价超上限 / vendor 错 / 余额不足等）· 标 expired 让 janitor 释放冻结
			w.markExpired(ctx, c.ID, fireErr.Error())
			w.logger.Warn("stockwatch: fire 失败 · 标 expired", "id", c.ID, "err", fireErr)
		}
	}
	w.logger.Info("stockwatch: 本轮 fire 完成",
		"vendor", p.VendorID, "fired", fired, "queue_size", len(cands))
	return nil
}

func (w *Watcher) markFulfilled(ctx context.Context, id string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'fulfilled'
		 WHERE id = ? AND status = 'fired'`, id)
	if err != nil {
		w.logger.Warn("stockwatch: 标 fulfilled 失败", "id", id, "err", err)
	}
}

func (w *Watcher) rewindToWatching(ctx context.Context, id string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'watching', fired_at = NULL, fired_reason = NULL
		 WHERE id = ? AND status = 'fired'`, id)
	if err != nil {
		w.logger.Warn("stockwatch: 回退 watching 失败", "id", id, "err", err)
	}
}

func (w *Watcher) markExpired(ctx context.Context, id, reason string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'expired', fired_reason = ?
		 WHERE id = ? AND status IN ('fired','watching')`,
		"fire_err:"+reason, id)
	if err != nil {
		w.logger.Warn("stockwatch: 标 expired 失败", "id", id, "err", err)
	}
}

// ListExpiredNeedingRelease · 找已 expired 但冻结还没释放的挂单 · janitor 用。
//
// 返回后由调用方（decider janitor）走 wallet 释放冻结 · 释放完调 MarkReleased。
// 分两步是因为 stockwatch 不该依赖 wallet（层次：wallet 是底层 · 但 stockwatch
// 是抢号链 · 让 janitor 做编排更清晰）。
func (w *Watcher) ListExpiredNeedingRelease(ctx context.Context, limit int) ([]WatcherRow, error) {
	if w == nil || w.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, passenger_id, COALESCE(bus_id, ''), target_group,
		       vendor_id, COALESCE(region, ''), client_order_id,
		       COALESCE(max_unit_price, 0), count, reserved_amount
		  FROM stock_watcher
		 WHERE status = 'expired' AND reserved_amount > 0
		 ORDER BY expires_at ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatcherRow
	for rows.Next() {
		var c WatcherRow
		if err := rows.Scan(
			&c.ID, &c.PassengerID, &c.BusID, &c.TargetGroup,
			&c.VendorID, &c.Region, &c.ClientOrderID,
			&c.MaxUnitPrice, &c.Count, &c.ReservedAmount,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkReleased · 冻结已释放 · 清零 reserved_amount 防重复释放（幂等）
func (w *Watcher) MarkReleased(ctx context.Context, id string) error {
	if w == nil || w.db == nil {
		return nil
	}
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET reserved_amount = 0
		 WHERE id = ? AND status = 'expired'`, id)
	return err
}

// SweepResult · Sweep 一轮的统计
type SweepResult struct {
	Scanned int
	Expired int
	Errors  int
}

// StartSweeper · 起后台 goroutine 定期跑 Sweep（TTL 扫过期挂单）。
//
// **为什么必须有**：不扫的话过期挂单永远停在 watching ·
//  1. 队列越堆越长 · 每次 restock 事件都去 fire 一堆早该失效的
//  2. **ModeMgr 把 watching 计入 demand** → 过期单堆着会让 demand 虚高 →
//     mode 永远判 tight → 探针一直 fire · 白打上游 API
//
// interval <= 0 用默认 60s（TTL 是分钟级 · 秒级扫没意义）。
func (w *Watcher) StartSweeper(ctx context.Context, interval time.Duration) {
	if w == nil || w.db == nil {
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.sweepCancel = cancel
	w.sweepDone = make(chan struct{})

	go func() {
		defer close(w.sweepDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				w.Sweep(runCtx)
			}
		}
	}()
}

// StopSweeper · 停后台 sweeper · timeout 兜底
func (w *Watcher) StopSweeper(timeout time.Duration) {
	if w == nil || w.sweepCancel == nil {
		return
	}
	w.sweepCancel()
	select {
	case <-w.sweepDone:
	case <-time.After(timeout):
		w.logger.Warn("stockwatch: StopSweeper 超时 · 强行返回")
	}
}

// Sweep · TTL 扫过期未 fire 的行 · 标 expired。
//
// 标 expired 之后：
//   - 不再进 Notify 的 fire 队列（WHERE status='watching' 过滤掉）
//   - 不再计入 ModeMgr 的 demand
//   - 若挂单时预冻结过（当前不冻 · reserved_amount 恒 0）· 由
//     ListExpiredNeedingRelease + MarkReleased 走释放
func (w *Watcher) Sweep(ctx context.Context) SweepResult {
	if w == nil || w.db == nil {
		return SweepResult{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'expired'
		 WHERE status = 'watching' AND expires_at <= ?`, now)
	if err != nil {
		w.logger.Warn("stockwatch: Sweep 失败", "err", err)
		return SweepResult{Errors: 1}
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		w.logger.Info("stockwatch: TTL 扫 · 标 expired", "n", n)
	}
	return SweepResult{Scanned: int(n), Expired: int(n)}
}

// sourceShouldFire · 决定这个 source 该不该 fire · 三层优先级：
//
//	① turbo 开着 → **一律 fire**（人工强制覆盖 · 无视 mode）
//	② mode 自动判 → 见下表
//	③ mode 为 nil（老装配 / 测试）→ 保守 fire
//
// source 编码约定（跟 Notify 调用方保持一致）：
//
//	source        | tight | balance | cool | turbo 开
//	--------------|-------|---------|------|---------
//	webhook       |  ✅   |   ✅    |  ❌  |   ✅     vendor 主动 push · 最快
//	stock_delta   |  ✅   |   ❌    |  ❌  |   ✅     我方探针 60s 采样 · 号少时关键补位
//	manual        |  ✅   |   ✅    |  ✅  |   ✅     CLI 调试强制
//
// **xi8 不参与抢号** —— 只写 vendor_probe_zone / vendor_dispatch 数据补齐 · 从不 Notify。
// 老注释里 xi8_signal 是历史残留 · 已删。
//
// **turbo 的意义**：上游连续几天缺货时 · 自动判断可能算成 cool（supply 长期 0 ·
// demand 也不高 · ratio 落 cool 区）· 但运营者知道"还要用 · 有货就抢" · 手工按住。
func (w *Watcher) sourceShouldFire(source string) bool {
	// ① turbo 人工强制 · 最高优先（急停已在 Notify 开头挡过）
	if w.turbo != nil && w.turbo.Engaged() {
		return true
	}
	// manual 源 · CLI 手工触发 · 一律 fire
	if source == "manual" {
		return true
	}
	// ② mode 自动判
	if w.mode == nil {
		return true // 老装配 / 测试 · 保守 fire
	}
	mode := w.mode.Current()
	switch source {
	case "stock_delta":
		return mode == ModeTight // 我方探针 60s 采样对比 · 只在紧张态 fire
	case "webhook":
		return mode == ModeTight || mode == ModeBalance // vendor 主动 push · 除 cool 都 fire
	default:
		return mode != ModeCool // 未知 source（含 manual）· cool 不 fire · 其余保守 fire
	}
}

// currentMode · log 用 · 带 turbo 标记
func (w *Watcher) currentMode() string {
	base := "no-mode-mgr"
	if w.mode != nil {
		base = w.mode.Current().String()
	}
	if w.turbo != nil && w.turbo.Engaged() {
		return base + "+turbo"
	}
	return base
}

// ── 小工具 ──

func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullIfZeroInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
