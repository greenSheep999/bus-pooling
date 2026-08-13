package kiroappcc

// wire types — vendor 私有，不外暴。
//
// 本 vendor 与其他 5 家最大差别（vendor 档案 §14）：
//   - **camelCase 字段** —— `availableKeys` / `keyPrice` / `pointsCost` / `retryAfter`
//   - **无幂等键** —— claim 请求没有 client_order_id，响应也没有 order_id
//   - **key payload 极简** —— 单个 `{key}`；批量 `{keys: [...]}`
//   - **错误 envelope 嵌套** —— `{error: {type, message}}`

// balanceResp `GET /openapi/balance` —— 只有一个 balance 字段（vendor 档案 §5）。
// **注意**：本 vendor 是 6 家里唯一没有 `/openapi/profile` 的，spent/earned 拿不到。
type balanceResp struct {
	Balance int64 `json:"balance"`
}

// stockResp `GET /openapi/stock` —— camelCase，且无区域字段（vendor 档案 §6）。
type stockResp struct {
	AvailableKeys int   `json:"availableKeys"`
	KeyPrice      int64 `json:"keyPrice"`
}

// claimReq `POST /openapi/claim` —— 没有 client_order_id / zone。
// **count 省略时 vendor 默认取 1**（vendor 档案 §7）；我方总是显式传。
type claimReq struct {
	Count int `json:"count"`
}

// claimResp `POST /openapi/claim` 的合并形态。
//
// 实际返回是两种形态之一（vendor 档案 §7）：
//   - 单个：`{key: "..."}`
//   - 批量：`{keys: [...]}`
//
// 加上车主自取时才附带的 `pointsCost`（0 = 拉自己产出的号 · vendor 档案 §7）。
// 用一个 struct 收，`Key` / `Keys` 二选一填 —— 靠 mapper 归一化到 KeyPayload 列表。
type claimResp struct {
	Key        string   `json:"key,omitempty"`
	Keys       []string `json:"keys,omitempty"`
	PointsCost int64    `json:"pointsCost"`
}

// errorResp `error` 是**嵌套对象**（vendor 档案 §12 —— 6 家里独家结构）。
//
//	{"error": {"type": "rate_limit_exceeded", "message": "..."}, "retryAfter": 180}
//
// `retryAfter` 是顶层 · 单位秒；限流时也会带 Retry-After header。
type errorResp struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	RetryAfter int `json:"retryAfter"`
}

func (e *errorResp) code() string { return e.Error.Type }
func (e *errorResp) msg() string  { return e.Error.Message }

// webhookPayload —— 本家 webhook 载荷。
//
// **真实形状**（2026-08-13 生产实测抓到 · vendor 从不公开 schema）：
//
//	{"available":50,"count":50,"event":"stock","id":"evt_BsawZMiNERBGITaBl5DcGNwV",
//	 "price":100,"time":"2026-08-13T15:30:39Z"}
//
// 跟 6 家共性字段**一个都对不上** —— 老结构按 `event_id` / `new_keys` /
// `order_id` 解析 · 全部落空 · dispatcher 因缺 event_id 直接丢弃。
// 后果：这家的 webhook 从接通起 100% 丢失（实测一天 21+ 条）。
//
// 下面两组字段都留着：`id`/`count`/`time` 是实测形状 · `event_id`/`new_keys`/
// `order_id` 是共性别名（vendor 改版靠回名的话仍能接住）。
type webhookPayload struct {
	Event string `json:"event"`

	// —— 实测字段 ——
	// ID 形如 "evt_xxx" · 本家唯一的去重 id（无独立 order_id）
	ID string `json:"id"`
	// Count 本批新增数
	Count int `json:"count"`
	// Available 推送时刻的当前库存（不是增量）
	Available int `json:"available"`
	// Price 单价（积分）· 本家 webhook 独有 · stock 端点之外的第二价格源
	Price float64 `json:"price"`
	// Time RFC3339 UTC · vendor 侧事件时刻（比我方收到早几百 ms）
	Time string `json:"time"`

	// —— 共性别名 · vendor 改版兜底 ——
	EventID         string `json:"event_id"`
	NewKeys         int    `json:"new_keys"`
	OrderID         string `json:"order_id"`
	PurchaseOrderID string `json:"purchase_order_id"`
	Timestamp       int64  `json:"timestamp"`
}

// eventID 去重 id · 实测字段优先 · 回落共性名
func (w *webhookPayload) eventID() string {
	if w.ID != "" {
		return w.ID
	}
	return w.EventID
}

// newKeys 本批新增数 · 实测字段优先 · 回落共性名
func (w *webhookPayload) newKeys() int {
	if w.Count > 0 {
		return w.Count
	}
	return w.NewKeys
}
