package kirooo

import "encoding/json"

// wire types — vendor 私有，不外暴。

// profileResp · GET /api/my/profile 真实响应（2026-08-15 生产实测）：
//
//	{"quota":0,"remaining":0,"used_quota":0,"claimable":0,
//	 "auto_fleet":false,"is_fleet_owner":false,"is_super":false,
//	 "min_reserve":1,"reserve_count":0,
//	 "risk_at":"","risk_flag":0,"risk_rate":0,"risk_threshold":100,
//	 "name":"...","user_no":"U100135","username":"...","role":"",
//	 "needs_2fa":false,"twofa_ok":true,"webhook_url":"..."}
//
// **P0 · 2026-08-15 修**：老 struct 期望 `profile.credits` 嵌套 · 但 vendor 真实响应
// 顶层平铺 · **也没有 credits 字段**（余额是 `remaining`）· Balance 恒解析成 0。
type profileResp struct {
	Name          string `json:"name"`
	Username      string `json:"username"`
	UserNo        string `json:"user_no"`
	Role          string `json:"role"`
	Quota         int64  `json:"quota"`      // 总配额
	Remaining     int64  `json:"remaining"`  // ★ 可用余额（Balance() 用这个）
	UsedQuota     int64  `json:"used_quota"` // 已花
	Claimable     int    `json:"claimable"`  // 当前可领数量
	MinReserve    int    `json:"min_reserve"`
	ReserveCount  int    `json:"reserve_count"`
	AutoFleet     bool   `json:"auto_fleet"`
	IsFleetOwner  bool   `json:"is_fleet_owner"`
	IsSuper       bool   `json:"is_super"`
	Needs2FA      bool   `json:"needs_2fa"`
	TwoFAOK       bool   `json:"twofa_ok"`
	RiskAt        string `json:"risk_at"`
	RiskFlag      int    `json:"risk_flag"`
	RiskRate      int    `json:"risk_rate"`
	RiskThreshold int    `json:"risk_threshold"`
	WebhookURL    string `json:"webhook_url"`
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
