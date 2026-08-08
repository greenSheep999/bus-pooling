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
	EventTest             EventType = "test"
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
