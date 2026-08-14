package webhookout

import (
	"context"
	"time"
)

// Start · 起 consume 循环 + retrier goroutine。
//
// **不阻塞** · 装配层调完 Start 立刻返回。
// ctx 结束时优雅退出(consume 走完 in-flight event 再退)。
func (d *Dispatcher) Start(ctx context.Context) {
	go d.consume(ctx)
	go d.runRetrier(ctx)
}

// Stop · 优雅退出 · 等 timeout 内消费完队列。
//
// 调用后 Dispatch 变 no-op(chan 关闭) · 主链继续跑不影响。
func (d *Dispatcher) Stop(timeout time.Duration) {
	select {
	case <-d.stopCh:
		return // 已停
	default:
		close(d.stopCh)
	}
	// 等 consume 收到 stopCh 并跑完 · timeout 到就强退
	select {
	case <-d.done:
	case <-time.After(timeout):
		d.logger.Warn("webhookout: consume 未在超时内退出 · 强退")
	}
	select {
	case <-d.retrierDone:
	case <-time.After(timeout):
		d.logger.Warn("webhookout: retrier 未在超时内退出 · 强退")
	}
}

// Dispatch · 主链调入口 · **非阻塞入队** · 满则 dropped 落台账。
//
// 触发源(decider.settle / deathwatch.markDead / deathwatch.RefundOnce /
// handoff.Complete / pullrecord.Assign push_pool)成功后调 · 主链不等结果。
//
// **fire-and-forget** — 内部消费失败只 log · 不回滚主 tx。
// 静默失败监控靠 status=pending / dropped 越堆越多的行来发现(admin health 端点)。
func (d *Dispatcher) Dispatch(ctx context.Context, passengerID string, evt EventType, payload any) {
	if d == nil {
		return
	}
	eventID := d.cfg.NewEventID()
	item := queueItem{
		passengerID: passengerID,
		eventType:   evt,
		payload:     payload,
		eventID:     eventID,
	}
	select {
	case d.queue <- item:
		// 入队成功 · consume 会捞
	default:
		// 队列满 · 落一条 dropped 台账让运维能查
		d.logger.Warn("webhookout: 队列满 · 事件被 dropped",
			"passenger", passengerID, "event", evt, "event_id", eventID)
		if d.cfg.Store != nil {
			body := marshalPayload(payload)
			_, _ = d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
				PassengerID: passengerID,
				EventID:     eventID,
				EventType:   string(evt),
				TargetURL:   "",
				Payload:     body,
				Attempt:     0,
				Status:      "dropped",
			})
		}
	}
}

// NotifyBoarded · push_pool 或 handoff 成功后 · 便捷入口(装配层用)。
//
// 组好 BoardedPayload · Dispatch 入队 · 非阻塞返。
func (d *Dispatcher) NotifyBoarded(ctx context.Context, passengerID string, credentialIDs []string, route string) {
	if d == nil {
		return
	}
	eventID := d.cfg.NewEventID()
	d.Dispatch(ctx, passengerID, EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(d.cfg.Now(), eventID, passengerID, EventBoarded),
		CredentialIDs: credentialIDs,
		Route:         route,
	})
}

// SendTest · 用户点"测试 webhook"按钮时同步调 · 返 (delivered, status_code, latency_ms, error)。
//
// **同步** — 用户 UI 转圈圈等着 · Retrier 不参与(测试事件 attempt 恒 1 · 失败就失败)。
// 主链 Dispatch 是异步 · 这个是给 handleTestWebhook 用的同步入口。
func (d *Dispatcher) SendTest(ctx context.Context, passengerID string) (bool, int, int, string) {
	if d == nil || d.cfg.Store == nil {
		return false, 0, 0, "webhookout 未装配"
	}
	cfg, err := d.cfg.Store.Get(ctx, passengerID)
	if err != nil {
		return false, 0, 0, "读 downstream cfg 失败"
	}
	if cfg.WebhookURL == "" || !cfg.WebhookSecretConfigured {
		return false, 0, 0, "webhook 未配 · 请先填 URL + 生成 secret"
	}
	secret, err := d.cfg.Store.DecryptWebhookSecret(cfg.WebhookSecretEncrypted)
	if err != nil || secret == "" {
		return false, 0, 0, "解密 secret 失败"
	}

	now := d.cfg.Now()
	eventID := d.cfg.NewEventID()
	payload := TestPayload{
		EnvelopeMeta: buildEnvelope(now, eventID, passengerID, EventTest),
		Note:         "webhook 测试 · 收到这条说明配置正确",
	}
	body := marshalPayloadBytes(payload)
	res := d.sendOnce(ctx, cfg, secret, eventID, EventTest, body, now)

	// 落一条台账让 handleListDeliveries 立即能看到
	status := "failed"
	if res.Delivered {
		status = "delivered"
	}
	var respStatus *int
	if res.Status > 0 {
		s := res.Status
		respStatus = &s
	}
	latency := res.LatencyMs
	_, _ = d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
		PassengerID:    passengerID,
		EventID:        eventID,
		EventType:      string(EventTest),
		TargetURL:      cfg.WebhookURL,
		Payload:        string(body),
		Attempt:        1,
		Status:         status,
		ResponseStatus: respStatus,
		ResponseSnip:   res.BodySnip,
		LatencyMs:      &latency,
	})

	errMsg := ""
	if !res.Delivered {
		if res.Err != nil {
			errMsg = "连不上目标地址"
		} else {
			errMsg = "目标未返 2xx"
		}
	}
	return res.Delivered, res.Status, res.LatencyMs, errMsg
}

// consume · 内部循环 · 从 queue 捞 item · 调 runSend。
//
// **不并发发送** — 单 goroutine 消费 · 保证同 passenger 事件按入队顺序发。
// 需要提升吞吐时改成 worker pool · 但阶段 1e 单节点单实例 · 单 goroutine 够。
func (d *Dispatcher) consume(ctx context.Context) {
	defer close(d.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case item := <-d.queue:
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := d.runSend(runCtx, item.passengerID, item.eventID, item.eventType,
				item.payload, 1); err != nil {
				d.logger.Warn("webhookout: 首次发送失败",
					"passenger", item.passengerID, "event", item.eventType, "err", err)
			}
			cancel()
		}
	}
}

// marshalPayload · 便捷把 payload 转 JSON string(dropped 台账用)。
// 失败时返空串(dropped 台账允许空 payload · 上层能看到 dropped 就够查了)。
func marshalPayload(payload any) string {
	return string(marshalPayloadBytes(payload))
}

// marshalPayloadBytes · 编码为字节(签名 + 台账都要用同一份字节)
func marshalPayloadBytes(payload any) []byte {
	if payload == nil {
		return []byte("{}")
	}
	// 走 encoding/json 编码 · 失败退回 "{}"
	b, err := jsonMarshal(payload)
	if err != nil {
		return []byte("{}")
	}
	return b
}
