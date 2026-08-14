package webhookout

import "time"

// Payload 四种事件的 body schema(docs/05-api-contract §11)。
//
// **每种事件独立 struct** — 别复用 providers.WebhookEvent(那是入向的) ·
// 也别把内部字段(kiro_rs_credential_id / current_group / death_source)
// 塞进来 · CLAUDE.md §0.1。
//
// 所有 payload 都带 event / event_id / timestamp / passenger_id 顶层字段 ·
// 各自加事件特有字段。timestamp 冗余带一次 · 防用户拿旧 X-Bus-Timestamp 重放。

// EnvelopeMeta · 所有 payload 都带的顶层字段。
type EnvelopeMeta struct {
	Event       EventType `json:"event"`
	EventID     string    `json:"event_id"`
	Timestamp   int64     `json:"timestamp"` // unix seconds
	PassengerID string    `json:"passenger_id"`
}

// NewKeysAvailablePayload · 拉号成功事件。
//
// **不带**内部字段(kiro_rs_credential_id / group / vendor 真名) ·
// 只给乘客决策要看的:多少号 / 哪家 vendor(打码 label) / 哪辆车。
type NewKeysAvailablePayload struct {
	EnvelopeMeta
	BusID       string   `json:"bus_id,omitempty"`     // 空 = 单独拉号进 record group
	VendorLabel string   `json:"vendor_label"`         // 打码 · 不给真名
	NewKeys     int      `json:"new_keys"`             // 本轮新增号数
	PullRoundID string   `json:"pull_round_id"`        // 前端可回溯
	CredentialIDs []string `json:"credential_ids"`     // 新增号的 uuid(对外可见的)
}

// AllKeysDeadPayload · 车里所有号都死了(**只**针对 bus · record group 号死走
// 事件 credential.dead 单条)。
//
// 触发时机由装配层判断"某 bus 是否全灭"(SELECT COUNT alive WHERE owner_bus_id=?) ·
// 单号死不发这条(那是 credential.dead) —— 阶段 1e-2 简化 · 只发 all_keys_dead。
type AllKeysDeadPayload struct {
	EnvelopeMeta
	BusID      string `json:"bus_id"`
	DiedAt     string `json:"died_at"` // ISO-8601
	VendorLabel string `json:"vendor_label,omitempty"`
}

// WarrantyRefundPayload · 质保退款到账。
type WarrantyRefundPayload struct {
	EnvelopeMeta
	// Amount 退款到账积分数(microunit · CLAUDE.md §7.2)
	Amount int64 `json:"amount"`
	// CredentialID 触发退款的号
	CredentialID string `json:"credential_id"`
	// BusID 所在的车 · 空 = 单独拉号
	BusID string `json:"bus_id,omitempty"`
	// RefundedAt ISO-8601
	RefundedAt string `json:"refunded_at"`
}

// BoardedPayload · 号已交付(去向 ② push_pool 成功 / 去向 ③ handoff Complete)。
//
// **不带明文** — handoff 完成后本地不留明文 · 只带 credential_id 让乘客侧对账。
type BoardedPayload struct {
	EnvelopeMeta
	// CredentialIDs 已交付的号列表
	CredentialIDs []string `json:"credential_ids"`
	// Route 交付方式 · 对外只有两种(§0.1 内部术语不出)：push_pool / handoff
	Route string `json:"route"`
	// VendorLabel 打码 label · 可空
	VendorLabel string `json:"vendor_label,omitempty"`
}

// TestPayload · 用户点"测试 webhook"按钮时发的。
type TestPayload struct {
	EnvelopeMeta
	Note string `json:"note"`
}

// eventAllowed · 检查事件类型是否要给该乘客发。
//
// 规则(docs/05 §11 + decisions §8.25)：
//   - 未配 webhook URL / secret → false(装配层已判 · 这里兜底)
//   - bus_only 开 · 事件不带 bus_id → false(乘客只关心车上的事)
//
// **push_on_pull / resync_on_dead / retry_on_failure**：
// 这些是**拉号策略**字段 · 决定"号死后我方自动补车 / 号交付后自动推 passengerpool" ·
// **跟 webhookout 无关**。webhookout 只按 URL/secret 是否配 + bus_only 判断。
func eventAllowed(cfg DownstreamConfig, evt EventType, hasBusID bool) bool {
	if cfg.WebhookURL == "" || !cfg.WebhookSecretConfigured {
		return false
	}
	if !cfg.Enabled {
		return false // 用户显式关了(1e-2 P0-1)
	}
	if cfg.BusOnly && !hasBusID {
		return false
	}
	// 订阅事件白名单(1e-2 P0-2) · Events==nil 视为全订阅兜底(defaults / 未设)
	// test 事件永远走(handleTestWebhook 独立入口 · 不受订阅白名单控制)
	if evt == EventTest {
		return true
	}
	if len(cfg.Events) > 0 {
		found := false
		for _, e := range cfg.Events {
			if e == string(evt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// buildEnvelope 拼 EnvelopeMeta(所有 payload 共用)。
func buildEnvelope(now time.Time, eventID, passengerID string, evt EventType) EnvelopeMeta {
	return EnvelopeMeta{
		Event:       evt,
		EventID:     eventID,
		Timestamp:   now.UTC().Unix(),
		PassengerID: passengerID,
	}
}
