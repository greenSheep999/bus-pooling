// Package webhookin · vendor 推来的 webhook 事件的分派器。
//
// 位置：webhook 通道最后一环 · api/vendor_webhook.go 验签 + Parse 拿到归一化的
// providers.WebhookEvent 后 · 交给这里处理。
//
// **职责边界**：
//   - 幂等去重（(vendor_id, event_id) 是主键 · vendor retry 同 id 会 UPSERT 不重投）
//   - 按 EventType 分派：
//     new_keys_available → 落 vendor_dispatch（fleet 视图）
//     all_keys_dead      → 触发 deathwatch.SweepOnce 提前号死处理
//     warranty_refund    → 直接调 deathwatch.RefundOnce · 走已有的 bus_member 分摊
//     webhook_test       → 只落 event log · 不动业务
//     其他               → 记 event log 兜底 · 未来 EventType 扩了不用改这里
//
// **不做**：
//   - 不解析原始 body（那是 vendor adapter 的 Parse() 干的活）
//   - 不做退款金额分摊（deathwatch.planRefund 已经做过 · 我方只触发）
//   - 不改 credential 状态（deathwatch 的 SweepOnce 拉 pool 判定 · 我方触发它）
package webhookin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
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
//
// source 参数 · 一律传 "vendor_self"（webhook 来自 vendor 自己 · 是权威源 ·
// 跟 backfiller 从 fleet 端点拉的走同一源 · xi8 是内部专用不走这里）。
type DispatchStore interface {
	UpsertDispatches(ctx context.Context, vendorID, source string, ds []providers.VendorDispatch) error
}

// RestockNotifier · 抢号链通知口（实现方 stockwatch.Watcher）· nil = 不通知。
//
// 定在消费侧避免 webhookin → stockwatch 硬依赖 · 也便于测试 mock。
type RestockNotifier interface {
	Notify(ctx context.Context, p stockwatch.NotifyParams) error
}

// ProbeZoneSink · v4.4 · 部分 vendor webhook 带 price/available · 顺手落
// vendor_probe_zone（source='webhook'）· 补 60s 探针间隙 + 前端 price-trend 多一路。
// nil = 不落价 · 事件流不受影响。
type ProbeZoneSink interface {
	InsertWebhook(ctx context.Context, vendorID, zone string, priceCredits int64, available int, at time.Time) error
}

// Dispatcher 分派器 · 各字段允许 nil（老装配 / 测试兼容）。
type Dispatcher struct {
	db            *sql.DB
	dispatchStore DispatchStore
	deathwatch    SweepTrigger
	notifier      RestockNotifier
	probeZone     ProbeZoneSink
	logger        *slog.Logger
}

type Config struct {
	DB            *sql.DB
	DispatchStore DispatchStore
	Deathwatch    SweepTrigger
	// Notifier 抢号链通知口 · new_keys 到时唤醒挂单 · nil = 不通知
	Notifier RestockNotifier
	// ProbeZone v4.4 · webhook 带 price/available 时顺手落 · nil = 不落
	ProbeZone ProbeZoneSink
	Logger    *slog.Logger
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
		notifier:      cfg.Notifier,
		probeZone:     cfg.ProbeZone,
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

// RecordRejected · 记一条**没能进入分派的** webhook。
//
// 为什么要落库（2026-08-13 实测教训）：解析失败的包只在容器日志里留一行 WARN ·
// 容器一重建就没了。结果是"上游推了多少 / 我方接住多少"这个最基本的问题 ·
// 只能靠翻反代访问日志人工比对才答得上来（实测有一家 22 条全丢 · 查了半天）。
//
// 落进 inbound_webhook_event · 用合成 event_id（`reject-<body 指纹>`）：
//   - 同一个 body 重推会撞主键 · 天然去重 · 不会把表刷爆
//   - `event_type='rejected'` 一眼跟正常事件分得开
//   - 只存指纹和原因 · **不存 body**（跟本表既定口径一致 · body 里可能有明文 key）
//
// 记录失败只 log · 不往上抛 —— 这条路径本身就是兜底 · 不能再制造新的失败。
func (d *Dispatcher) RecordRejected(ctx context.Context, vendorID, reason string, rawBody []byte) {
	if d.db == nil || vendorID == "" {
		return
	}
	sum := sha256.Sum256(rawBody)
	eventID := "reject-" + hex.EncodeToString(sum[:])[:16]
	now := time.Now().UTC().Format(time.RFC3339)
	if len(reason) > 200 {
		reason = reason[:200]
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO inbound_webhook_event
		  (vendor_id, event_id, event_type, received_at, dispatch_status, dispatch_error)
		VALUES (?, ?, 'rejected', ?, 'rejected', ?)
		ON CONFLICT(vendor_id, event_id) DO NOTHING
	`, vendorID, eventID, now, reason)
	if err != nil {
		d.logger.Warn("webhookin: 记录丢弃事件失败", "vendor", vendorID, "err", err)
		return
	}
	d.logger.Error("webhook 被丢弃 · 上游推了但我方没接住",
		"vendor", vendorID, "reason", reason,
		"body_fingerprint", eventID, "body_bytes", len(rawBody),
		"how_to", "对着 docs/19-fields.md §11 核这家的载荷字段名")
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
	case providers.EventReservedKeysDelivered:
		return d.onReservedKeysDelivered(ctx, e)
	case providers.EventKeyRevokedAbuse:
		return d.onKeyRevokedAbuse(ctx, e)
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
	// **双区合并通知**（某家 vendor 独家 · 一次到货只推 1 条但带两区信息）·
	// 逐区处理 —— 拿顶级字段当一条会漏掉另一区的开号批次和挂单唤醒
	if len(e.PerZone) > 0 {
		return d.onNewKeysPerZone(ctx, e)
	}
	// 幂等主键选择（2026-08-12 / 08-13 两轮生产实测踩坑修）：
	//   - 部分 vendor webhook 只发 client_order_id / purchase_order_id 不发独立 order_id ·
	//     档明说这就是幂等主键 · 值稳定 · 每批唯一
	//   - 部分 vendor 发 order_id 是"开号批次 id" · 跟 client_order_id 不同语义
	//   - **有一家一个订单号字段都不发**（只给 "evt_xxx" 去重 id）· 前两级全空 ·
	//     再 skip 就等于这家 webhook 永久静默（实测丢了一整天）
	// fallback 三级：OrderID → PurchaseOrderID → EventID。
	// EventID 当 key 安全：vendor 重推同一事件带同一 id（去重语义本就如此）· upsert 幂等。
	dispatchKey := e.OrderID
	if dispatchKey == "" {
		dispatchKey = e.PurchaseOrderID
	}
	if dispatchKey == "" {
		dispatchKey = e.EventID
	}
	if dispatchKey == "" {
		d.logger.Warn("webhookin: new_keys_available 无任何可用幂等键 · 跳过",
			"vendor", e.VendorID)
		return "skipped", nil
	}
	dispatch := providers.VendorDispatch{
		DispatchKey:  dispatchKey,  // vendor 侧稳定 · 幂等主键（order_id 优先 · fallback purchase_order_id）
		DispatchedAt: e.ReceivedAt, // 精度到我方收到时刻 · vendor finished_at 不同家不一样
		Count:        e.NewKeys,
		Alive:        e.NewKeys, // 刚开号 · 假设全 alive
		Region:       string(e.Zone),
		Status:       "running",
		Raw:          e.RawPayload,
	}
	if err := d.dispatchStore.UpsertDispatches(ctx, string(e.VendorID),
		"vendor_self", []providers.VendorDispatch{dispatch}); err != nil {
		return "error", fmt.Errorf("upsert vendor_dispatch: %w", err)
	}
	d.logger.Info("webhookin: 新开号事件 · 已落 vendor_dispatch",
		"vendor", e.VendorID, "dispatch_key", dispatchKey, "count", e.NewKeys)

	// 唤醒抢号链 · **这是最快的信号**（vendor push 到我方 200ms-2s · 探针 60s 才轮到）
	// 抢号能不能抢到主要靠这条路径（decisions §11.15）。
	//
	// 通知失败只 log · 不影响 webhook 返 200 —— vendor 侧看到非 2xx 会重推 ·
	// 而 dispatch 已经落库了 · 重推会走幂等 upsert 但也会重复 Notify（可接受 ·
	// 挂单侧有 conditional UPDATE 保证只 fire 一次）。
	if d.notifier != nil {
		// **region 归一化**（docs/16 缺口 5）· e.Zone 可能是 "us"/"us-east-1"/"美国区" ·
		// stock_watcher.region 语义定死 zone 名 · 走 ZoneOf 归一保 SQL 匹配一致
		if err := d.notifier.Notify(ctx, stockwatch.NotifyParams{
			VendorID: string(e.VendorID),
			Region:   string(providers.ZoneOf(string(e.Zone))),
			Count:    e.NewKeys,
			Source:   "webhook",
		}); err != nil {
			d.logger.Warn("webhookin: 唤醒抢号链失败",
				"vendor", e.VendorID, "dispatch_key", dispatchKey, "err", err)
		}
	}

	// v4.4 · 部分 vendor webhook 带 price/available · 顺手落 vendor_probe_zone
	// source='webhook'· 补 60s 探针间隙 + 前端 price-trend 多一路第三源
	d.recordWebhookPrice(ctx, e)
	return "ok", nil
}

// recordWebhookPrice · e.UnitPrice / e.Available 都为空 → 无 op · 只对带 price/available 字段的 vendor 生效
func (d *Dispatcher) recordWebhookPrice(ctx context.Context, e *providers.WebhookEvent) {
	if d.probeZone == nil {
		return
	}
	if e.UnitPrice == nil && e.Available == nil {
		return
	}
	var priceCredits int64
	if e.UnitPrice != nil {
		// UnitPrice.Amount 是 microunit · 侧表 our_unit_credits 也是 microunit
		priceCredits = e.UnitPrice.Amount
	}
	var avail int
	if e.Available != nil {
		avail = *e.Available
	}
	zone := string(providers.ZoneOf(string(e.Zone)))
	if err := d.probeZone.InsertWebhook(ctx, string(e.VendorID), zone, priceCredits, avail, e.ReceivedAt); err != nil {
		d.logger.Warn("webhookin: 写 vendor_probe_zone 失败", "vendor", e.VendorID, "err", err)
	}
}

// onNewKeysPerZone · 双区合并通知的逐区处理。
//
// 那家 vendor 一次到货只推 1 条 webhook（`notification_scope: "dual"`）· 但 body 里
// 带两个区的完整信息 · **且幂等键按区分开**（`purchase_order_ids_by_region`）。
//
// 老代码只认顶级字段 · 后果：
//
//	· 只落 1 条 dispatch —— 另一区的开号批次在 /status 页上完全看不到
//	· 只 Notify 1 次 —— 挂在另一区的挂单收不到唤醒 · 那区抢号率恒 0
//	· 幂等键用顶级那个 —— fire 时可能拉错区
//
// 逐区落 dispatch（key 用该区的 purchase_order_id · 天然按区唯一）+ 逐区 Notify。
func (d *Dispatcher) onNewKeysPerZone(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	dispatches := make([]providers.VendorDispatch, 0, len(e.PerZone))
	for _, z := range e.PerZone {
		dispatches = append(dispatches, providers.VendorDispatch{
			DispatchKey:  z.PurchaseOrderID, // 该区专用幂等键 · 按区唯一
			DispatchedAt: e.ReceivedAt,
			Count:        z.NewKeys,
			Alive:        z.NewKeys,
			Region:       string(z.Zone), // 归一后的 zone（vendor_dispatch.region 语义）
			Status:       "running",
			Raw:          e.RawPayload,
		})
	}
	if err := d.dispatchStore.UpsertDispatches(ctx, string(e.VendorID),
		"vendor_self", dispatches); err != nil {
		return "error", fmt.Errorf("upsert 双区 dispatch: %w", err)
	}
	d.logger.Info("webhookin: 双区合并通知 · 已逐区落 dispatch",
		"vendor", e.VendorID, "zones", len(e.PerZone), "total_keys", e.NewKeys)

	// 逐区唤醒抢号链 —— 挂在哪个区的挂单就该被哪个区的到货唤醒
	if d.notifier != nil {
		for _, z := range e.PerZone {
			if z.NewKeys <= 0 {
				continue // 该区这次没到货 · 不唤醒
			}
			if err := d.notifier.Notify(ctx, stockwatch.NotifyParams{
				VendorID: string(e.VendorID),
				Region:   string(z.Zone),
				Count:    z.NewKeys,
				Source:   "webhook",
			}); err != nil {
				d.logger.Warn("webhookin: 逐区唤醒抢号链失败",
					"vendor", e.VendorID, "zone", z.Zone, "err", err)
			}
		}
	}
	return "ok", nil
}

// onReservedKeysDelivered · 包量预留已按合约单价交付 · **钱已扣 · 号已是我方的**。
//
// ⚠️ **绝不能调 Purchase** —— 那会按公共价再买一批（上游档案明确警告）。
// 这条通知里的 `order_id` 是取到 key 正文的**唯一入口**（上游 keys 列表只给前缀）·
// 漏处理的后果是：钱扣了 · 号记在我方名下 · 但程序永远拿不到。
//
// **当前策略：落 dispatch + 高声告警 · 不自动补拉**。理由：
//   - 包量预留是**运营侧签协议**触发的 · 不对应任何 pull_intent · 拿到 key 也不知道该进谁的车
//   - 自动补拉再自动派发 = 在没有用户意图的情况下动号池 · 风险高于收益
//   - 我方当前**没签包量协议** · 这条事件不该出现；真出现了说明协议生效了 · 需要人接手
//
// 所以这里用 ERROR 级别 log（会进告警）· 把 order_id 留在 vendor_dispatch 里 ·
// 运维拿它调 `GET /api/my/orders/{order_id}/keys` 手工取号。
// 未来若常态化跑包量协议 · 再把补拉 + 派发接成自动的。
func (d *Dispatcher) onReservedKeysDelivered(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	// order_id 是这条事件的关键 —— 没它就永远拿不到号 · 必须喊出来
	if e.OrderID == "" {
		d.logger.Error("webhookin: 包量预留交付但缺 order_id · 号可能永久拿不到 · 立即人工介入",
			"vendor", e.VendorID, "event_id", e.EventID, "new_keys", e.NewKeys)
		return "error", fmt.Errorf("reserved_keys_delivered 缺 order_id（vendor=%s event=%s）",
			e.VendorID, e.EventID)
	}

	// 落 dispatch 留痕 · source 标 reserved 区分于普通开号批次
	if d.dispatchStore != nil {
		dispatch := providers.VendorDispatch{
			DispatchKey:  "reserved-" + e.OrderID,
			DispatchedAt: e.ReceivedAt,
			Count:        e.NewKeys,
			Alive:        e.NewKeys,
			Region:       string(providers.ZoneOf(string(e.Zone))),
			Status:       "running",
			Raw:          e.RawPayload,
		}
		if err := d.dispatchStore.UpsertDispatches(ctx, string(e.VendorID),
			"vendor_self", []providers.VendorDispatch{dispatch}); err != nil {
			return "error", fmt.Errorf("upsert reserved dispatch: %w", err)
		}
	}

	// **ERROR 级别** —— 这是"钱已经扣了 · 号在上游等着取"的状态 · 必须有人看到
	d.logger.Error("webhookin: 包量预留已交付 · 钱已扣 · 需人工补拉取号",
		"vendor", e.VendorID,
		"order_id", e.OrderID,
		"new_keys", e.NewKeys,
		"zone", e.Zone,
		"how_to", "GET /api/my/orders/{order_id}/keys 取正文 · 再手工入池",
	)
	return "ok", nil
}

// onKeyRevokedAbuse · 上游主动收回已售 key（用量异常判定滥用）· **且不退积分**。
//
// **为什么必须处理**：号已经被上游作废 · 但我方 credential 还是 alive ·
// 用户去用就是废号。漏处理 = 用户拿到不能用的号还以为是我方的问题。
//
// 处理方式跟 all_keys_dead 一样触发 deathwatch 全池扫描 —— 让探活确认这把号已死 ·
// 走正常的 dead 标记链路（同时会评估质保退款 · 虽然上游对 revoked 不退 ·
// 我方对乘客的质保是我方自己的承诺 · 由 deathwatch 按我方规则判）。
//
// **不做精确到单把 key 的处理**：载荷只给 `key_prefix`（不给完整 key 或我方 credential id）·
// 按前缀反查要扫全池 · 跟直接触发全池扫描等价 · 后者还能顺带发现同批其他死号。
func (d *Dispatcher) onKeyRevokedAbuse(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	// 先把事件本身喊出来 —— 滥用判定是个信号：说明有号被公开分发了
	d.logger.Warn("webhookin: 上游收回已售号（判定用量滥用）· 触发全池探活",
		"vendor", e.VendorID, "event_id", e.EventID,
		"note", "上游对 revoked 不退积分 · 我方质保按自己规则判")

	if d.deathwatch == nil {
		return "skipped", nil
	}
	rep := d.deathwatch.SweepOnce(ctx)
	d.logger.Info("webhookin: key_revoked_abuse · deathwatch 扫描完成",
		"vendor", e.VendorID,
		"scanned", rep.Scanned, "marked_dead", rep.MarkedDead, "errors", rep.Errors)
	if rep.Errors > 0 {
		return "error", fmt.Errorf("deathwatch.SweepOnce 有 %d 个错误", rep.Errors)
	}
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
// **两步**：
//  1. 先把对应 pull_round 从 completed/partial → refunded
//     （deathwatch.FindRefundable 只扫 status='refunded' 的候选 · 少这一步候选集恒空）
//  2. 再调 deathwatch.RefundOnce · 分摊落 wallet_ledger
//
// **关联键优先级**：vendor_order_id > client_order_id (PurchaseOrderID)
// （part 1: WebhookEvent.OrderID 是 vendor 侧订单号 · pull_round.vendor_order_id 就是它 ·
//   part 2: 少数 vendor 只发 purchase_order_id · 那个就是我方 client_order_id）
//
// **幂等**：UPDATE 加 `WHERE status IN ('completed','partial')` · 已经 refunded 的不重刷 ·
// 重放 webhook 不会造成重复退款（deathwatch 也有 warranty_refunded_at 幂等锚 · 双保险）。
func (d *Dispatcher) onWarrantyRefund(ctx context.Context, e *providers.WebhookEvent) (string, error) {
	if d.deathwatch == nil {
		return "skipped", nil
	}

	// 1. 标 pull_round.status='refunded' · 让 deathwatch.FindRefundable 能扫到
	marked, markErr := d.markRoundRefunded(ctx, e)
	if markErr != nil {
		// 标失败也要继续 · deathwatch 兜底还是要跑（可能之前已经标过）
		d.logger.Warn("webhookin: 标 pull_round refunded 失败 · 仍尝试 RefundOnce",
			"vendor", e.VendorID, "event_id", e.EventID,
			"order_id", e.OrderID, "purchase_order_id", e.PurchaseOrderID,
			"err", markErr)
	}

	// 2. 触发退款分摊
	rep := d.deathwatch.RefundOnce(ctx, 100)
	d.logger.Info("webhookin: warranty_refund · 已处理",
		"vendor", e.VendorID, "event_id", e.EventID,
		"marked_refunded", marked, "scanned", rep.Scanned,
		"refunded", rep.Refunded, "errors", rep.Errors)
	if rep.Errors > 0 {
		return "error", fmt.Errorf("deathwatch.RefundOnce 有 %d 个错误", rep.Errors)
	}
	return "ok", nil
}

// markRoundRefunded · 按事件里的 order_id / purchase_order_id 标 pull_round refunded。
//
// 返回被 UPDATE 的行数 · 0 表示没找到匹配的（可能是重放 · 或者 vendor 用了非标记键 ·
// 或者是"包量预留"这类不走 pull_round 的路径）· 上层 log WARN 但不阻塞 RefundOnce。
func (d *Dispatcher) markRoundRefunded(ctx context.Context, e *providers.WebhookEvent) (int64, error) {
	if d.db == nil {
		return 0, nil
	}
	// 至少要有一个键 · 都空的话根本没法定位（vendor 契约违反 · 提前返 0 记 log）
	if e.OrderID == "" && e.PurchaseOrderID == "" {
		return 0, nil
	}

	// vendor_order_id 优先（vendor 侧稳定单号）· client_order_id 兜底（我方幂等键 = vendor 侧 purchase_order_id）
	// 一次 UPDATE 匹配任一：
	res, err := d.db.ExecContext(ctx, `
		UPDATE pull_round
		   SET status = 'refunded',
		       completed_at = COALESCE(completed_at, ?)
		 WHERE vendor_id = ?
		   AND status IN ('completed', 'partial')
		   AND (
		         (? != '' AND vendor_order_id = ?)
		      OR (? != '' AND client_order_id = ?)
		       )
	`,
		time.Now().UTC().Format(time.RFC3339),
		string(e.VendorID),
		e.OrderID, e.OrderID,
		e.PurchaseOrderID, e.PurchaseOrderID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
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
