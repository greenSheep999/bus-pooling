package kiroceo

// wire types — vendor 私有，不外暴。

type profileResp struct {
	Profile struct {
		Balance          int64 `json:"balance"`
		Spent            int64 `json:"spent"`
		Earned           int64 `json:"earned"`
		MaxKeysHeld      int   `json:"max_keys_held"`
		HoldCapEffective int   `json:"hold_cap_effective"`
		KeysHeld         int   `json:"keys_held"`
	} `json:"profile"`
}

// stockResp · GET /api/my/stock 真实响应（2026-08-15 实测确认 · docs/vendors/_endpoints-2026-08-12/kiroceo_api_my_stock.json）：
//
//	{"max":0,"max_purchase":10,"min":1,"quota":0,"reserved":0,
//	 "zones":[{"zone":"us","label":"美国区","unit_price":100,"enabled":true,
//	           "available":0,"max":0,"stock":0}, ...]}
//
// **P0 · 2026-08-15 修**：老 struct 有 `stock: {public_available, my_private}` 嵌套 ·
// **vendor 真实响应根本没这个字段** · sr.Stock.PublicAvailable 恒 0 → snap.Available 恒 0 ·
// decider 判有货偏错。档案 §2.2 早就写了"实际用 max"· 我一直没改。
//
// vendor 档案 §2.2 语义：`max` = 当前可一次性提取的最大数量（= 各 zone 汇总 · 受账户上限约束）·
// 就是 Available 该用的字段。
type stockResp struct {
	// Max ★ Available 的真实来源（各 zone 汇总 · 受账户上限约束）
	Max int `json:"max"`
	// 单次提货上下限（账户维度）· `min`/`max_purchase`（跟 zone 内的 max 不是一个东西）
	Min         int `json:"min"`
	MaxPurchase int `json:"max_purchase"`
	// Quota / Reserved · 账户维度（不参与 stock 判断）
	Quota    int `json:"quota"`
	Reserved int `json:"reserved"`
	// 逐 zone 数据
	Zones []zoneItem `json:"zones"`
	// **注意**：本 vendor 无顶层 warranty_minutes 字段（其他家有 · 这家档案 §5.2 说恒 10min · 硬编码）·
	// 老 struct 的 WM 保留占位 · JSON 解到就填 · 解不到 = 0 · 上层看 Capability.WarrantyMinutes（=10）
	WM int `json:"warranty_minutes"`
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
}

type purchaseResp struct {
	ClientOrderID string    `json:"client_order_id"`
	OrderID       string    `json:"order_id"`
	Zone          string    `json:"zone"`
	Purchased     int       `json:"purchased"`
	UnitPrice     int64     `json:"unit_price"`
	TotalCredits  int64     `json:"total_credits"`
	Remaining     int64     `json:"remaining"`
	Keys          []keyItem `json:"keys"`
	WarrantyUntil string    `json:"warranty_until"`
	WM            int       `json:"warranty_minutes"`
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

type webhookPayload struct {
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	Visibility      string `json:"visibility"`
	NewKeys         int    `json:"new_keys"`
	Dead            int    `json:"dead"`
	Zone            string `json:"zone"`
	OrderID         string `json:"order_id"`
	PurchaseOrderID string `json:"purchase_order_id"`
	PoolID          string `json:"pool_id"`
	RoundID         string `json:"round_id"`
	MotherID        string `json:"mother_id"`
	RefundedQuota   int64  `json:"refunded_quota"`
	RefundedKeys    int    `json:"refunded_keys"`
	Reason          string `json:"reason"`
	Timestamp       int64  `json:"timestamp"`
}
