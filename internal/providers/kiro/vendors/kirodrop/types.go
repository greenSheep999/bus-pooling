package kirodrop

import "encoding/json"

// wire types — vendor 私有，不外暴。

// profileResp · GET /api/my/profile 真实响应（2026-08-15 生产实测 curl 抓的）：
//
//	{"name":"danlio","quota":"0.000000","remaining":"0.000000",
//	 "used_quota":"0.000000","webhook_url":"..."}
//
// **注意**（P0 · 2026-08-15 修）：
//   - 顶层平铺 · **没有** `profile` 嵌套（老 struct 期望嵌套 · 恒解析成零值）
//   - 值是**字符串** · CNY 保留 6 位小数（本 vendor 混币 · balance=CNY 单价=USD）
//   - 余额字段名是 **`remaining`** · 不是 `balance`
//
// Balance() 语义：可用余额 = remaining · 不是总配额 quota。
type profileResp struct {
	Name       string `json:"name"`
	Quota      string `json:"quota"`      // 总配额 · CNY 字符串
	Remaining  string `json:"remaining"`  // ★ 可用余额（Balance() 用这个）
	UsedQuota  string `json:"used_quota"` // 已花（可推 Spent · 但字符串减法怕精度）
	WebhookURL string `json:"webhook_url"`
}

// stockResp 本 vendor /api/me/stock 响应。
//
// **注意 vendor 侧曾变更过 stock 字段类型**：
//   - 旧形状：{"stock": {"public_available": N, "my_private": M}, "zones": [...]}
//   - 新形状：{"balance": "...", "price": "...", "region": "...", "stock": N}
//
// 用 json.RawMessage 接住 stock 字段 · toStockSnapshot 里两种都解一遍 · 兼容双形状。
type stockResp struct {
	Stock  json.RawMessage `json:"stock"`
	Region string          `json:"region"`
	Price  string          `json:"price"` // 新形状是字符串（例 "7.35"）· 旧无此字段
	Zones  []zoneItem      `json:"zones"`
	Max    int             `json:"max"`
	Min    int             `json:"min_per_order"`
	MaxPO  int             `json:"max_per_order"`
	WM     int             `json:"warranty_minutes"`
}

// stockNested 旧形状里 stock 是嵌套对象。
type stockNested struct {
	PublicAvailable int `json:"public_available"`
	MyPrivate       int `json:"my_private"`
}

type zoneItem struct {
	Zone      string `json:"zone"`
	Region    string `json:"region"`
	Available int    `json:"available"`
	UnitPrice int64  `json:"unit_price"`
}

type purchaseReq struct {
	Count         int    `json:"count"`
	Zone          string `json:"zone,omitempty"`
	ClientOrderID string `json:"client_order_id"`
	// MaxTotalCNY 涨价保护 · 单位 CNY（vendor 用 CNY 记账 · price 是 USD 显示 · balance 是 CNY）
	// vendor 侧行为：价格上涨超过这个值时返 409 且不扣款 · 见 docs/vendors/drop-kiro-ss.md
	// 对齐 providers.PurchaseRequest.MaxTotal · 6 家里**只有 kirodrop 有原生支持**
	MaxTotalCNY string `json:"max_total_cny,omitempty"`
}

type purchaseResp struct {
	ClientOrderID string `json:"client_order_id"`
	OrderID       string `json:"order_id"`
	Zone          string `json:"zone"`
	Purchased     int    `json:"purchased"`
	UnitPrice     int64  `json:"unit_price"`
	TotalCredits  int64  `json:"total_credits"`
	Remaining     int64  `json:"remaining"`
	// Status · 订单状态 · vendor 文档明说可能是这三个（本 vendor 独家有部分退款态）：
	//   completed           · 全部成交
	//   partially_refunded  · **部分成交 · 差额已退**
	//   refunded            · 全额退
	// 空表示 vendor 没返（老响应形状）· 按 completed 处理
	Status string `json:"status"`
	// RefundedAmountCNY · 退款金额（CNY 字符串 · 本 vendor 独家）· 部分退款时非空
	RefundedAmountCNY string    `json:"refunded_amount_cny"`
	Keys              []keyItem `json:"keys"`
	WarrantyUntil     string    `json:"warranty_until"`
	WM                int       `json:"warranty_minutes"`
}

type keyItem struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Account       string `json:"account"`
	Password      string `json:"password"`
	IssuerURL     string `json:"issuer_url"`
	Free          bool   `json:"free"`
	Paid          int64  `json:"paid"`
	WarrantyUntil string `json:"warranty_until"`
}

type redeemReq struct {
	Code string `json:"code"`
}

type redeemResp struct {
	Quota   int64 `json:"quota"`
	Balance int64 `json:"balance"`
}

type usageResp struct {
	Usage struct {
		Remaining int64 `json:"remaining"`
		Total     int64 `json:"total"`
		Synced    int   `json:"synced"`
		Keys      int   `json:"keys"`
	} `json:"usage"`
}

type errorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (e *errorResp) msg() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Error
}

// webhookPayload · 本 vendor 的 webhook 载荷。
//
// ⚠️ **本 vendor 的 new_keys_available 是「双区合并通知」**（6 家里唯一）：
// 一次到货**只推 1 条** · 但 body 里带两个区的完整信息（其他 vendor 是分两条推）。
// 所以 `*_by_region` 那几个 map 字段才是权威值 —— 顶级的 `purchase_order_id`
// 在双区场景下**不能当唯一幂等键用**（要按区取）。
//
// `notification_scope == "dual"` 是双区标记。
type webhookPayload struct {
	Event      string `json:"event"`
	EventID    string `json:"event_id"`
	Visibility string `json:"visibility"`
	NewKeys    int    `json:"new_keys"` // 合计（双区时是两区之和）
	Dead       int    `json:"dead"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	OrderID    string `json:"order_id"`
	// DispatchID · 本 vendor 独家 · 批次 id
	DispatchID      string `json:"dispatch_id"`
	PurchaseOrderID string `json:"purchase_order_id"`
	// NotificationScope · "dual" 表示这条是双区合并通知
	NotificationScope string `json:"notification_scope"`
	// Regions · 本次涉及的区（例 ["us-east-1","eu-central-1"]）
	Regions []string `json:"regions"`
	// NewKeysByRegion · 逐区新增数（例 {"us-east-1":3,"eu-central-1":2}）
	NewKeysByRegion map[string]int `json:"new_keys_by_region"`
	// PurchaseOrderIDsByRegion · ★ **逐区幂等键** · 双区场景要按区取这个 · 不是顶级那个
	PurchaseOrderIDsByRegion map[string]string `json:"purchase_order_ids_by_region"`
	// BatchIDsByRegion · 逐区批次 id 列表
	BatchIDsByRegion map[string][]string `json:"batch_ids_by_region"`
	CreatedAt        string              `json:"created_at"`
	RefundedQuota    int64               `json:"refunded_quota"`
	RefundedKeys     int                 `json:"refunded_keys"`
	Reason           string              `json:"reason"`
	// 以下字段本 vendor 实测不发 · 保留是因为 parse 逻辑跟其他家共用形状 · 解不到就是零值
	PoolID    string `json:"pool_id"`
	RoundID   string `json:"round_id"`
	MotherID  string `json:"mother_id"`
	Timestamp int64  `json:"timestamp"`
}
