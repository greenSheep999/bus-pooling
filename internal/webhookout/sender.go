package webhookout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Sender · 单次发送(签名 + POST + 落台账)。**每次一条**。
//
// **不做重试** — 一次调 sendOnce 只落一行台账;
// 失败时 status=pending + next_retry_at 让 Retrier 后续扫库重试。
//
// 主链走 Dispatch 入队 · consume 循环里调 sendOnce。

// sendResult · sendOnce 返 · 内部用(不出包)
type sendResult struct {
	// Delivered · 2xx 时 true
	Delivered bool
	// Retriable · 5xx / timeout · true(下次到期重试) · 4xx 或签名错 · false(直接 failed)
	Retriable bool
	// Status · HTTP 状态码 · 0 = 未连上
	Status int
	// LatencyMs · 请求耗时
	LatencyMs int
	// BodySnip · 响应体前 512 字节(排查用)
	BodySnip string
	// Err · 网络层错误 · 只 log · 不外传
	Err error
}

// sendOnce 发一次 · 返 sendResult。
//
// 参数:
//   - cfg · downstream cfg(URL + secret 已解密)
//   - eventID / eventType / body(已 JSON 编码 · signer 用)
//   - timestamp · 签名时间 · 也是台账 created_at
func (d *Dispatcher) sendOnce(
	ctx context.Context,
	cfg DownstreamConfig,
	secret string,
	eventID string, eventType EventType,
	body []byte, timestamp time.Time,
) sendResult {
	// 签名
	sig := SignPayload(secret, timestamp, body)

	// 组装请求
	req := &HTTPReq{
		URL:    cfg.WebhookURL,
		Method: "POST",
		Headers: map[string]string{
			"Content-Type":    "application/json",
			"X-Bus-Event":     string(eventType),
			"X-Bus-Event-Id":  eventID,
			"X-Bus-Timestamp": fmt.Sprintf("%d", timestamp.UTC().Unix()),
			"X-Bus-Signature": sig,
		},
		Body: body,
	}

	sendCtx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()

	start := time.Now()
	resp, err := d.cfg.HTTPX.Do(sendCtx, req)
	latency := int(time.Since(start).Milliseconds())

	if err != nil {
		return sendResult{
			Delivered: false, Retriable: true,
			LatencyMs: latency, Err: err,
		}
	}
	snip := ""
	if len(resp.Body) > 512 {
		snip = string(resp.Body[:512])
	} else {
		snip = string(resp.Body)
	}
	res := sendResult{
		Status:    resp.StatusCode,
		LatencyMs: latency,
		BodySnip:  snip,
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.Delivered = true
	case resp.StatusCode >= 500 || resp.StatusCode == 429 || resp.StatusCode == 408:
		res.Retriable = true
	default:
		// 4xx(除 408/429)不重试 · 通常是配错 · 重试改不了结果
		res.Retriable = false
	}
	return res
}

// runSend 执行一次完整投递(签名 + 发送 + 落台账 + 状态推进)。
//
// **每次调用落一行 outbound_webhook_delivery**(attempt = 传入值 · retrier 用 attempt+1)。
//
// 成功 · 状态 delivered · retry 停。
// 失败 · retriable=true · 状态 pending + next_retry_at = now + backoff[attempt-1]。
// 失败 · retriable=false 或 attempts 用尽 · 状态 failed · retry 停。
func (d *Dispatcher) runSend(
	ctx context.Context,
	passengerID, eventID string, eventType EventType,
	payload any, attempt int,
) error {
	if d.cfg.Store == nil {
		return errors.New("webhookout: store 未装配")
	}
	cfg, err := d.cfg.Store.Get(ctx, passengerID)
	if err != nil {
		// 拉不到 cfg · 落一条 dropped 台账让运维能看到
		body, _ := json.Marshal(payload)
		_, _ = d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
			PassengerID: passengerID,
			EventID:     eventID,
			EventType:   string(eventType),
			TargetURL:   "",
			Payload:     string(body),
			Attempt:     attempt,
			Status:      "dropped",
		})
		return fmt.Errorf("webhookout: 拉 cfg: %w", err)
	}
	// 白名单过滤(URL/secret 已配 + bus_only 检查)
	hasBusID := false
	if m, ok := payload.(hasBus); ok {
		hasBusID = m.HasBusID()
	}
	if !eventAllowed(cfg, eventType, hasBusID) {
		return nil // 不发 · 不落台账(避免污染 retrier)
	}

	secret, err := d.cfg.Store.DecryptWebhookSecret(cfg.WebhookSecretEncrypted)
	if err != nil || secret == "" {
		body, _ := json.Marshal(payload)
		_, _ = d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
			PassengerID: passengerID,
			EventID:     eventID,
			EventType:   string(eventType),
			TargetURL:   cfg.WebhookURL,
			Payload:     string(body),
			Attempt:     attempt,
			Status:      "dropped",
		})
		return fmt.Errorf("webhookout: 解密 secret: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhookout: 编码 payload: %w", err)
	}

	now := d.cfg.Now()
	res := d.sendOnce(ctx, cfg, secret, eventID, eventType, body, now)

	// 落台账 + 状态推进
	status := "failed"
	if res.Delivered {
		status = "delivered"
	} else if res.Retriable && attempt < d.cfg.MaxRetries {
		status = "pending"
	}
	var respStatus *int
	if res.Status > 0 {
		s := res.Status
		respStatus = &s
	}
	latency := res.LatencyMs
	_, insertErr := d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
		PassengerID:    passengerID,
		EventID:        eventID,
		EventType:      string(eventType),
		TargetURL:      cfg.WebhookURL,
		Payload:        string(body),
		Attempt:        attempt,
		Status:         status,
		ResponseStatus: respStatus,
		ResponseSnip:   res.BodySnip,
		LatencyMs:      &latency,
	})
	if insertErr != nil {
		d.logger.Warn("webhookout: 落台账失败(不影响主链)",
			"passenger", passengerID, "event_id", eventID, "err", insertErr)
	}

	// 若 pending · 落 next_retry_at 给 retrier 扫
	if status == "pending" && attempt-1 < len(d.cfg.Backoffs) {
		nextAt := now.Add(d.cfg.Backoffs[attempt-1])
		if _, err := d.cfg.DB.ExecContext(ctx, `
			UPDATE outbound_webhook_delivery
			   SET next_retry_at = ?
			 WHERE event_id = ? AND attempt = ?`,
			nextAt.Format(time.RFC3339Nano), eventID, attempt); err != nil {
			d.logger.Warn("webhookout: 落 next_retry_at 失败",
				"event_id", eventID, "err", err)
		}
	}
	return nil
}

// hasBus · 接口标记 · 让 sender 知道 payload 是否带 bus_id(bus_only 过滤用)
type hasBus interface {
	HasBusID() bool
}

// HasBusID 让四种 payload 各自实现
func (p NewKeysAvailablePayload) HasBusID() bool { return p.BusID != "" }
func (p AllKeysDeadPayload) HasBusID() bool      { return p.BusID != "" }
func (p WarrantyRefundPayload) HasBusID() bool   { return p.BusID != "" }
func (p BoardedPayload) HasBusID() bool          { return false } // handoff / push_pool 不涉及 bus
func (p TestPayload) HasBusID() bool             { return false }
