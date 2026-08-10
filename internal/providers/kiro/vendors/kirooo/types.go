package kirooo

import "encoding/json"

// wire types — vendor 私有，不外暴。

type profileResp struct {
	Profile struct {
		// 本 vendor 用 `credits` 字段名（跟其他家的 balance 命名不同 · 见 mapper）
		Credits int64 `json:"credits"`
		Spent   int64 `json:"spent"`
		Earned  int64 `json:"earned"`
	} `json:"profile"`
}

// stockResp /api/my/stock 响应。
//
// **注意上游变更过 stock 字段类型**（跟其他几家一样的坑）：
//   - 旧形状：{"stock": {"claimable": N, "public_available": M}, "zones": [...]}
//   - 新形状：{"stock": 7, "claimable": 7, "max": 7, "unit_price": 50, "credits": 70}
//
// 用 json.RawMessage 接 stock · toStockSnapshot 里两种都解一遍。
// 不兼容会导致 Stock() 一直 unmarshal 失败 → 探针 alive=0 → status 页显示
// "0% uptime"（但 backfill 其实好的）—— 2026-08-11 踩过。
type stockResp struct {
	Stock     json.RawMessage `json:"stock"`
	Claimable int             `json:"claimable"`
	UnitPrice int64           `json:"unit_price"`
	Zones     []zoneItem      `json:"zones"`
	Max       int             `json:"max"`
	Min       int             `json:"min_per_order"`
	MaxPO     int             `json:"max_per_order"`
	WM        int             `json:"warranty_minutes"`
}

// stockNested 旧形状里 stock 是嵌套对象。
type stockNested struct {
	Claimable       int `json:"claimable"`
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

// webhookPayload — 本 vendor webhook 载荷（vendor 档案 §11）
//
// 事件类型：`new_keys_available` / `all_keys_dead` / `test`（**不叫 webhook_test**）·
// `client_order_id` 与 `purchase_order_id` 字面同值（后者是老名字，档案 §11）·
// **不带签名**（档案 §11：不签名，靠不可猜 URL 路径当口令）
type webhookPayload struct {
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	ClientOrderID   string `json:"client_order_id"`
	OrderID         string `json:"order_id"`
	PurchaseOrderID string `json:"purchase_order_id"`
	NewKeys         int    `json:"new_keys"`
	Dead            int    `json:"dead"`
	ClaimHint       string `json:"claim_hint"`
	Message         string `json:"message"`
	Time            string `json:"time"`
}
