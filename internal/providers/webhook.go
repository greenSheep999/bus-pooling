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
	// PerZone · 逐区拆分（**只有发「双区合并通知」的 vendor 会填** · 其他家恒 nil）。
	//
	// 那家 vendor 一次到货只推 1 条 webhook · 但 body 里带两个区的完整信息 ·
	// 且**幂等键是按区分开的**。非空时上层要**逐区**落 dispatch + 逐区唤醒抢号链 ——
	// 拿顶级 NewKeys / PurchaseOrderID 当一条处理会：
	//   · 只落一条 dispatch（另一区的开号批次丢了）
	//   · 只唤醒一次抢号链（挂在另一区的挂单收不到通知）
	//   · 用错幂等键去 Purchase（可能拉错区）
	PerZone []ZoneDelivery
}

// ZoneDelivery · 双区合并通知里的一个区
type ZoneDelivery struct {
	Zone Zone
	// Region vendor 原文（"us-east-1"）· 留痕用
	Region  string
	NewKeys int
	// PurchaseOrderID ★ **该区专用的幂等键** · 拿它去 Purchase 才不会拉错区
	PurchaseOrderID string
	// BatchIDs 该区的批次 id 列表（对账用）
	BatchIDs []string
}

type WebhookParser interface {
	VerifySignature(secret string, headers http.Header, rawBody []byte) error
	Parse(rawBody []byte, headers http.Header) (*WebhookEvent, error)
}
