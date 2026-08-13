package providers

import (
	"encoding/json"
	"net/http"
	"time"
)

type EventType string

const (
	EventNewKeysAvailable EventType = "new_keys_available"
	EventAllKeysDead      EventType = "all_keys_dead"
	EventKeyRevokedAbuse  EventType = "key_revoked_abuse"
	EventKeySuspect       EventType = "key_suspect"
	EventWarrantyRefund   EventType = "warranty_refund"
	// EventReservedKeysDelivered · 包量预留已按合约单价交付 · **钱已扣 · 号已是我方的**。
	//
	// ⚠️ **跟 EventNewKeysAvailable 处理方式完全相反**（上游档案明确警告）：
	//   new_keys_available      · 有货了 · 去买（拿 purchase_order_id 当幂等键调 Purchase）
	//   reserved_keys_delivered · 已经买好了 · **拿 OrderID 调补拉取正文**
	//
	// 对这条**再调 Purchase 会按公共价再买一批**。而且上游只给前缀不给正文 ——
	// 这条通知里的 order_id 是取到 key 正文的**唯一入口** · 漏处理 = 钱扣了拿不到号。
	EventReservedKeysDelivered EventType = "reserved_keys_delivered"
	EventTest                  EventType = "test"
)

type WebhookEvent struct {
	VendorID        VendorID
	EventID         string
	EventType       EventType
	OrderID         string
	PurchaseOrderID string
	NewKeys         int
	DeadKeys        int
	RefundAmount    *Money
	Zone            Zone
	ReceivedAt      time.Time
	RawPayload      json.RawMessage
}

type WebhookParser interface {
	VerifySignature(secret string, headers http.Header, rawBody []byte) error
	Parse(rawBody []byte, headers http.Header) (*WebhookEvent, error)
}
