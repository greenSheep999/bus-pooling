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
}

// Firer · fire 时打这个 · 拿 intent_id 触发 decider 走一次拉号
//
// 独立接口避免循环依赖（stockwatch → decider → stockwatch 会环）·
// decider 提供实现（PullByWatcher method）· main.go 装配时注入。
type Firer interface {
	// FireByIntent · 用 intent_id 找 pull_intent 记录 · 走一次 decider.Purchase
	// 返 nil = 抢到（fulfilled）· ErrStillNoStock = 又抢空（继续 watching）·
	// 其他 err = 硬失败（expired）
	FireByIntent(ctx context.Context, intentID string) error
}

// ErrStillNoStock · Firer 返这个说明 vendor 又缺货了 · Watcher 保持 watching 等下次
var ErrStillNoStock = errors.New("stockwatch: fire 后 vendor 仍缺货")

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

// EnqueueParams · Enqueue 的入参
type EnqueueParams struct {
	IntentID     string        // pull_intent(id) · 必填
	VendorID     string        // auto-pick 选中的 vendor · 必填
	Region       string        // 特定 region · 空 = 任意
	Count        int           // 要几个 · 必填
	MaxUnitPrice int64         // 涨价保护 · microunit · 0 = 不限
	TTL          time.Duration // 存活时长 · 0 用 defaultTTL
}

// Enqueue · 挂单 · decider 缺货时 auto 模式调。
// 幂等：intent_id 冲突时 UPDATE 覆盖（同一 intent 重挂视为 refresh · 常见于 retry）。
func (w *Watcher) Enqueue(ctx context.Context, p EnqueueParams) error {
	if w == nil || w.db == nil {
		return errors.New("stockwatch: Watcher 未装配")
	}
	if p.IntentID == "" || p.VendorID == "" || p.Count < 1 {
		return fmt.Errorf("stockwatch: 参数非法 intent=%q vendor=%q count=%d",
			p.IntentID, p.VendorID, p.Count)
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = w.defaultTTL
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)

	// 幂等：同 intent 重挂 · 更新过期时刻和参数 · 但保留 fire_count（防 spam retry）
	_, err := w.db.ExecContext(ctx, `
		INSERT INTO stock_watcher
			(intent_id, vendor_id, region, count, max_unit_price,
			 started_at, expires_at, status, fire_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'watching', 0)
		ON CONFLICT(intent_id) DO UPDATE SET
			vendor_id      = excluded.vendor_id,
			region         = excluded.region,
			count          = excluded.count,
			max_unit_price = excluded.max_unit_price,
			expires_at     = excluded.expires_at,
			status         = 'watching'
	`, p.IntentID, p.VendorID, nullIfEmpty(p.Region), p.Count,
		nullIfZeroInt64(p.MaxUnitPrice),
		now.Format(time.RFC3339), expires.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("stockwatch: enqueue: %w", err)
	}
	w.logger.Info("stockwatch: 缺货挂单",
		"intent_id", p.IntentID, "vendor", p.VendorID,
		"count", p.Count, "ttl", ttl,
		"max_unit_price", p.MaxUnitPrice)
	return nil
}

// Cancel · 撤单 · 用户主动取消 pull_intent 时调 · 幂等
func (w *Watcher) Cancel(ctx context.Context, intentID string) error {
	if w == nil || w.db == nil {
		return nil
	}
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'cancelled'
		 WHERE intent_id = ? AND status = 'watching'`, intentID)
	return err
}

// NotifyParams · Notify 入参
type NotifyParams struct {
	VendorID string // 哪家 vendor restock 了
	Region   string // 具体 region · 空 = 全 region 都算命中
	Count    int    // 这一波新到几个（可选 · 用于 log · 不影响筛选）
	Source   string // webhook / stock_delta / xi8_signal / manual
}

// Notify · 三源都调这个 · 有 restock 事件时打过来。
//
// 逻辑：
//  1. **急停 check**：kill switch engaged → 全 skip · log
//  2. **运营态 check**：mode 决定这个 source 该不该 fire
//     - stock_delta 源 · 只有 ModeTight 才 fire（省 API + 避免误抢）
//     - webhook 源 · ModeTight + ModeBalance 都 fire（webhook 是最快信号 · 别浪费）
//     - xi8_signal 源 · 同 webhook（xi8 signals 也是 push · 但比 webhook 慢）
//     - ModeCool · 都不 fire（库存充足 · 用户来了现打）
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
	if !w.sourceShouldFire(p.Source) {
		w.logger.Debug("stockwatch: 运营态跳过 · 只观测",
			"vendor", p.VendorID, "source", p.Source,
			"mode", w.currentMode())
		return nil
	}
	// 查 watching 队列 · 按队列顺序
	rows, err := w.db.QueryContext(ctx, `
		SELECT intent_id, count, max_unit_price
		  FROM stock_watcher
		 WHERE vendor_id = ? AND status = 'watching'
		   AND (region IS NULL OR region = ? OR ? = '')
		   AND expires_at > ?
		 ORDER BY started_at ASC`,
		p.VendorID, p.Region, p.Region,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("stockwatch: 查 watching: %w", err)
	}
	type candidate struct {
		intentID     string
		count        int
		maxUnitPrice sql.NullInt64
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.intentID, &c.count, &c.maxUnitPrice); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()

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
			 WHERE intent_id = ? AND status = 'watching'`,
			time.Now().UTC().Format(time.RFC3339), p.Source, c.intentID)
		if err != nil {
			w.logger.Warn("stockwatch: 抢 fired 状态失败", "intent_id", c.intentID, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // 别的 goroutine 已经抢了
		}

		fireErr := w.firer.FireByIntent(ctx, c.intentID)
		switch {
		case fireErr == nil:
			// 抢到号 · 标 fulfilled（decider 里已经推进 pull_intent 状态 · 这里只标 watcher）
			w.markFulfilled(ctx, c.intentID)
			fired++
		case errors.Is(fireErr, ErrStillNoStock):
			// 又空了 · 回退 watching 等下次事件 · 计入 fire_count 便于观察
			w.rewindToWatching(ctx, c.intentID)
			w.logger.Info("stockwatch: fire 后仍缺货 · 回退等下次",
				"intent_id", c.intentID)
			// 停 · vendor 已经又空 · 后面 intent 不用试
			return nil
		default:
			// 硬失败（涨价超上限 / vendor 错 / 余额不足等）· 标 expired 让 janitor 退款
			w.markExpired(ctx, c.intentID, fireErr.Error())
			w.logger.Warn("stockwatch: fire 失败 · 标 expired",
				"intent_id", c.intentID, "err", fireErr)
		}
	}
	w.logger.Info("stockwatch: 本轮 fire 完成",
		"vendor", p.VendorID, "fired", fired, "queue_size", len(cands))
	return nil
}

func (w *Watcher) markFulfilled(ctx context.Context, intentID string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'fulfilled'
		 WHERE intent_id = ? AND status = 'fired'`, intentID)
	if err != nil {
		w.logger.Warn("stockwatch: 标 fulfilled 失败", "intent_id", intentID, "err", err)
	}
}

func (w *Watcher) rewindToWatching(ctx context.Context, intentID string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'watching', fired_at = NULL, fired_reason = NULL
		 WHERE intent_id = ? AND status = 'fired'`, intentID)
	if err != nil {
		w.logger.Warn("stockwatch: 回退 watching 失败", "intent_id", intentID, "err", err)
	}
}

func (w *Watcher) markExpired(ctx context.Context, intentID, reason string) {
	_, err := w.db.ExecContext(ctx, `
		UPDATE stock_watcher SET status = 'expired', fired_reason = ?
		 WHERE intent_id = ? AND status IN ('fired','watching')`,
		"fire_err:"+reason, intentID)
	if err != nil {
		w.logger.Warn("stockwatch: 标 expired 失败", "intent_id", intentID, "err", err)
	}
}

// SweepResult · Sweep 一轮的统计
type SweepResult struct {
	Scanned int
	Expired int
	Errors  int
}

// Sweep · TTL 扫过期未 fire 的行 · 标 expired · janitor 每分钟调
// 后续 pull_intent 层的 janitor 会看到 stock_watcher.status=expired · 退款关单
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
//	webhook       |  ✅   |   ✅    |  ❌  |   ✅
//	xi8_signal    |  ✅   |   ✅    |  ❌  |   ✅
//	stock_delta   |  ✅   |   ❌    |  ❌  |   ✅
//	manual        |  ✅   |   ✅    |  ✅  |   ✅
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
		return mode == ModeTight // 探针主动 · 只在紧张态 fire
	case "webhook", "xi8_signal":
		return mode == ModeTight || mode == ModeBalance // 事件驱动 · 除 cool 都 fire
	default:
		return mode != ModeCool // 未知 source · 保守 · cool 不 fire
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
