// Package webhookin · vendor 推来的 webhook 事件的分派器。
//
// 位置：webhook 通道最后一环 · api/vendor_webhook.go 验签 + Parse 拿到归一化的
// providers.WebhookEvent 后 · 交给这里处理。
//
// **职责边界**：
//   - 幂等去重（(vendor_id, event_id) 是主键 · vendor retry 同 id 会 UPSERT 不重投）
//   - 按 EventType 分派：
//       new_keys_available → 落 vendor_dispatch（fleet 视图）
//       all_keys_dead      → 触发 deathwatch.SweepOnce 提前号死处理
//       warranty_refund    → 直接调 deathwatch.RefundOnce · 走已有的 bus_member 分摊
//       webhook_test       → 只落 event log · 不动业务
//       其他               → 记 event log 兜底 · 未来 EventType 扩了不用改这里
//
// **不做**：
//   - 不解析原始 body（那是 vendor adapter 的 Parse() 干的活）
//   - 不做退款金额分摊（deathwatch.planRefund 已经做过 · 我方只触发）
//   - 不改 credential 状态（deathwatch 的 SweepOnce 拉 pool 判定 · 我方触发它）
package webhookin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// SweepTrigger · deathwatch 的对外接口（避免包依赖循环）。
//
// SweepOnce 让 all_keys_dead 事件到达时立即扫号池 · 不用等 5min ticker。
// RefundOnce 让 warranty_refund 立刻走已有的分摊逻辑。
type SweepTrigger interface {
	SweepOnce(ctx context.Context) SweepReport
	RefundOnce(ctx context.Context, limit int) RefundReport
}

// SweepReport / RefundReport · deathwatch 定义的形状 · 这里只用来 log · 不 assert 字段
type SweepReport struct {
	Scanned    int
	MarkedDead int
	Errors     int
}
type RefundReport struct {
	Scanned  int
	Refunded int
	Errors   int
}

// DispatchStore · vendor_dispatch 表写入接口（避免包依赖循环）。
// 只需要 UpsertDispatches · 从 vendorview.OrderKeyStore 传进来。
type DispatchStore interface {
	UpsertDispatches(ctx context.Context, vendorID string, ds []providers.VendorDispatch) error
}

// Dispatcher 分派器 · 各字段允许 nil（老装配 / 测试兼容）。
type Dispatcher struct {
	db            *sql.DB
	dispatchStore DispatchStore
	deathwatch    SweepTrigger
	logger        *slog.Logger
}

type Config struct {
	DB            *sql.DB
	DispatchStore DispatchStore
	Deathwatch    SweepTrigger
	Logger        *slog.Logger
}

func New(cfg Config) *Dispatcher {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		db:            cfg.DB,
		dispatchStore: cfg.DispatchStore,
		deathwatch:    cfg.Deathwatch,
		logger:        logger,
	}
}

// Handle · 处理一个已归一化的事件 · 主入口。
//
// 幂等：先查 inbound_webhook_event · 命中同 (vendor_id, event_id) 直接返 skip · 不重投。
// 收到 nil event · 返 skip · 不算错。
// 分派出错不吞 · 返错 · receiver 决定是否把 HTTP 状态传回给 vendor（让它 retry）。
func (d *Dispatcher) Handle(ctx context.Context, e *providers.WebhookEvent) error {
	if e == nil {
		return nil
	}
	if e.EventID == "" || e.VendorID == "" {
		return errors.New("webhookin: 缺 vendor_id 或 event_id · vendor adapter 的 Parse() 应该填")
	}

	vendorID := string(e.VendorID)
	eventType := string(e.EventType)
	if eventType == "" {
		eventType = "other"
	}

	// 幂等检查 · 主键 (vendor_id, event_id) · 命中就跳
	if d.db != nil {
		var count int
		err := d.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM inbound_webhook_event WHERE vendor_id=? AND event_id=?`,
			vendorID, e.EventID).Scan(&count)
		if err == nil && count > 0 {
			d.logger.Debug("webhook 事件已处理过 · 幂等跳过",
				"vendor", vendorID, "event_id", e.EventID, "event_type", eventType)
			return nil
		}
	}

	// 落 event log · 先记 pending · 后面派单结果补 dispatch_status
	receivedAt := e.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	var refundMicro *int64
	if e.RefundAmount != nil && e.RefundAmount.Amount != 0 {
		v := e.RefundAmount.Amount
		refundMicro = &v
	}
	if d.db != nil {
		_, err := d.db.ExecContext(ctx, `
			INSERT INTO inbound_webhook_event
			  (vendor_id, event_id, event_type, order_id, purchase_order_id,
			   new_keys, dead_keys, refund_micro, zone, received_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(vendor_id, event_id) DO NOTHING
		`, vendorID, e.EventID, eventType,
			nullStr(e.OrderID), nullStr(e.PurchaseOrderID),
			nullInt(e.NewKeys), nullInt(e.DeadKeys), refundMicro,
			nullStr(string(e.Zone)), receivedAt.UTC().Format(time.RFC3339))
		if err != nil {
			d.logger.Warn("webhookin: 落 event log 失败",
				"vendor", vendorID, "event_id", e.EventID, "err", err)
		}
	}

	// 分派 · 按事件类型走不同处理
	status, dispatchErr := d.dispatchByType(ctx, e)
	d.markDispatched(ctx, vendorID, e.EventID, status, dispatchErr)
	return dispatchErr
}

// dispatchByType · 按 EventType 走不同分支 · 返 (status, err)。
// status: ok | skipped | error（同 event log 的 dispatch_status 字段）
func (d *Dispatcher) dispatchByType(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	switch e.EventType {
	case providers.EventNewKeysAvailable:
		return d.onNewKeys(ctx, e)
	case providers.EventAllKeysDead:
		return d.onAllKeysDead(ctx, e)
	case providers.EventWarrantyRefund:
		return d.onWarrantyRefund(ctx, e)
	case providers.EventTest:
		d.logger.Info("webhookin: test 事件 · 只 log", "vendor", e.VendorID, "event_id", e.EventID)
		return "skipped", nil
	default:
		// 未识别的类型 · 兜底 log · 未来 vendor 加新类型不 crash
		d.logger.Info("webhookin: 未识别事件类型 · 只 log",
			"vendor", e.VendorID, "event_type", e.EventType, "event_id", e.EventID)
		return "skipped", nil
	}
}

// onNewKeys · new_keys_available · 落 vendor_dispatch · 让 /status 页立即见到
func (d *Dispatcher) onNewKeys(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	if d.dispatchStore == nil {
		return "skipped", nil
	}
	if e.OrderID == "" {
		d.logger.Warn("webhookin: new_keys_available 缺 order_id · 跳过",
			"vendor", e.VendorID, "event_id", e.EventID)
		return "skipped", nil
	}
	dispatch := providers.VendorDispatch{
		DispatchKey:  e.OrderID,             // vendor 侧稳定 · 幂等主键
		DispatchedAt: e.ReceivedAt,          // 精度到我方收到时刻 · vendor finished_at 不同家不一样
		Count:        e.NewKeys,
		Alive:        e.NewKeys,             // 刚开号 · 假设全 alive
		Region:       string(e.Zone),
		Status:       "running",
		Raw:          e.RawPayload,
	}
	if err := d.dispatchStore.UpsertDispatches(ctx, string(e.VendorID),
		[]providers.VendorDispatch{dispatch}); err != nil {
		return "error", fmt.Errorf("upsert vendor_dispatch: %w", err)
	}
	d.logger.Info("webhookin: 新开号事件 · 已落 vendor_dispatch",
		"vendor", e.VendorID, "order_id", e.OrderID, "count", e.NewKeys)
	return "ok", nil
}

// onAllKeysDead · vendor 整批号死 · 立即触发 deathwatch 扫号池
// （不等 5min ticker · 用户能更快看到"号死了 + 已退款"）
func (d *Dispatcher) onAllKeysDead(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	if d.deathwatch == nil {
		return "skipped", nil
	}
	rep := d.deathwatch.SweepOnce(ctx)
	d.logger.Info("webhookin: all_keys_dead · 触发 deathwatch 扫描",
		"vendor", e.VendorID, "event_id", e.EventID,
		"scanned", rep.Scanned, "marked_dead", rep.MarkedDead, "errors", rep.Errors)
	if rep.Errors > 0 {
		return "error", fmt.Errorf("deathwatch.SweepOnce 有 %d 个错误", rep.Errors)
	}
	return "ok", nil
}

// onWarrantyRefund · vendor 侧退了钱给我方 · 我方要退回给拼车成员
// （多人车按 bus_member.share_pct 分摊 · 单人车全额给 owner）
//
// 具体分摊逻辑在 deathwatch.RefundOnce 里 · 从 credential_ledger 找 FindRefundable
// 的 candidate · 每一个用 planRefund 分摊 · 落 wallet_ledger。这里只触发。
func (d *Dispatcher) onWarrantyRefund(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	if d.deathwatch == nil {
		return "skipped", nil
	}
	rep := d.deathwatch.RefundOnce(ctx, 100) // 一次最多退 100 个 candidate
	d.logger.Info("webhookin: warranty_refund · 触发退款",
		"vendor", e.VendorID, "event_id", e.EventID,
		"scanned", rep.Scanned, "refunded", rep.Refunded, "errors", rep.Errors)
	if rep.Errors > 0 {
		return "error", fmt.Errorf("deathwatch.RefundOnce 有 %d 个错误", rep.Errors)
	}
	return "ok", nil
}

// markDispatched · 把派单结果补回 inbound_webhook_event 的 dispatch_* 列
func (d *Dispatcher) markDispatched(ctx context.Context, vendorID, eventID, status string, dispatchErr error) {
	if d.db == nil {
		return
	}
	errStr := ""
	if dispatchErr != nil {
		errStr = dispatchErr.Error()
		if len(errStr) > 200 {
			errStr = errStr[:200]
		}
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE inbound_webhook_event
		   SET dispatched_at = ?, dispatch_status = ?, dispatch_error = ?
		 WHERE vendor_id = ? AND event_id = ?
	`, time.Now().UTC().Format(time.RFC3339), status, nullStr(errStr), vendorID, eventID)
	if err != nil {
		d.logger.Warn("webhookin: 更新 dispatch_status 失败", "err", err)
	}
}

// nullStr / nullInt 小工具
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
