package downstream

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Delivery 是一条 webhook 投递台账。
//
// 对外可见的字段跟 WebhookDelivery TS 类型对齐（web/src/types/index.ts:466）：
//
//	id / event / ok / status_code / attempt / latency_ms / created_at
//
// 内部字段 payload / target_url / response_body_snippet 只做排查用 —— 不出响应体。
type Delivery struct {
	ID             string
	PassengerID    string
	EventID        string
	EventType      string
	TargetURL      string
	Attempt        int
	Status         string // pending | delivered | failed | dropped
	ResponseStatus *int
	LatencyMs      *int
	CreatedAt      time.Time
	DeliveredAt    *time.Time
}

// OK 是对外的收敛结果（CLAUDE.md §12.5）：
// delivered → true · 其余全部 false（pending / failed / dropped 用户视角都是"没成功"）。
func (d Delivery) OK() bool {
	return d.Status == StatusDelivered
}

// 状态枚举（内部）
const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusDropped   = "dropped"
)

// RecordAttempt 是插一条投递记录用的入参。
type RecordAttempt struct {
	PassengerID    string
	EventID        string
	EventType      string
	TargetURL      string
	Payload        string // 已序列化好的 JSON
	Attempt        int
	Status         string
	ResponseStatus *int
	ResponseSnip   string // 长度上限由调用方裁到 512
	LatencyMs      *int
}

// InsertDelivery 落一条投递台账。
//
// 用在两处：
//   - webhook 出向 worker 每次尝试后（无论成功失败都落一条）
//   - POST /me/downstream/webhook/test（一条 event_type = "test" 的伪投递）
func (s *Store) InsertDelivery(ctx context.Context, a RecordAttempt) (Delivery, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	var deliveredAt sql.NullString
	if a.Status == StatusDelivered {
		deliveredAt = sql.NullString{String: now.Format(time.RFC3339Nano), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO outbound_webhook_delivery
		  (id, passenger_id, event_id, event_type, target_url, payload,
		   attempt, status, response_status, response_body_snippet, latency_ms,
		   delivered_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, a.PassengerID, a.EventID, a.EventType, a.TargetURL, a.Payload,
		a.Attempt, a.Status, nullIntPtr(a.ResponseStatus), nullIfEmpty(a.ResponseSnip),
		nullIntPtr(a.LatencyMs), deliveredAt, now.Format(time.RFC3339Nano))
	if err != nil {
		return Delivery{}, fmt.Errorf("downstream: 写投递台账: %w", err)
	}
	return Delivery{
		ID: id, PassengerID: a.PassengerID, EventID: a.EventID,
		EventType: a.EventType, TargetURL: a.TargetURL,
		Attempt: a.Attempt, Status: a.Status,
		ResponseStatus: a.ResponseStatus, LatencyMs: a.LatencyMs,
		CreatedAt: now,
	}, nil
}

// ListDeliveries 分页读投递台账 · 按 created_at 倒序。
//
// 阶段 1a 前端就是一个简单列表，limit 默认 50 已经够看。
func (s *Store) ListDeliveries(ctx context.Context, passengerID string, limit, offset int) ([]Delivery, error) {
	if limit < 1 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_id, event_type, target_url, attempt, status,
		       response_status, latency_ms, delivered_at, created_at
		  FROM outbound_webhook_delivery
		 WHERE passenger_id = ?
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`, passengerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("downstream: 读投递台账: %w", err)
	}
	defer rows.Close()

	var out []Delivery
	for rows.Next() {
		var (
			d           Delivery
			respStatus  sql.NullInt64
			latencyMs   sql.NullInt64
			deliveredAt sql.NullString
			createdAt   string
		)
		if err := rows.Scan(&d.ID, &d.EventID, &d.EventType, &d.TargetURL,
			&d.Attempt, &d.Status, &respStatus, &latencyMs, &deliveredAt, &createdAt); err != nil {
			return nil, fmt.Errorf("downstream: scan 投递: %w", err)
		}
		d.PassengerID = passengerID
		if respStatus.Valid {
			n := int(respStatus.Int64)
			d.ResponseStatus = &n
		}
		if latencyMs.Valid {
			n := int(latencyMs.Int64)
			d.LatencyMs = &n
		}
		d.CreatedAt = parseTime(createdAt)
		if deliveredAt.Valid {
			t := parseTime(deliveredAt.String)
			d.DeliveredAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func nullIntPtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
