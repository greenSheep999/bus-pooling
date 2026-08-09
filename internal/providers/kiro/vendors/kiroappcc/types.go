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

func (e *errorResp) code() string    { return e.Error.Type }
func (e *errorResp) msg() string     { return e.Error.Message }

// webhookPayload —— vendor 档案 §10：**payload schema 未公开**。
//
// 骨架先按 6 家共性放一组常见字段，实际字段名等对接联调时再修。当前只有一句话：
// "有新库存时主动推一条 JSON 到你的地址"。字段全 optional / omitempty。
type webhookPayload struct {
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	NewKeys         int    `json:"new_keys"`
	PurchaseOrderID string `json:"purchase_order_id"`
	Timestamp       int64  `json:"timestamp"`
}
