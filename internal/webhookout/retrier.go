package webhookout

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// runRetrier · 每 5s 扫 outbound_webhook_delivery 状态 pending + next_retry_at 到期 ·
// 走 attempt++ · 3 次后置 failed。
//
// **单节点扫描** — SQLite 单节点没有多机竞争 · 未来扩多实例要加 SELECT FOR UPDATE
// (SQLite 用 BEGIN IMMEDIATE + 条件 UPDATE 抢锁)。
func (d *Dispatcher) runRetrier(ctx context.Context) {
	defer close(d.retrierDone)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.retryTick(ctx); err != nil {
				d.logger.Warn("webhookout: retrier 扫描出错", "err", err)
			}
		}
	}
}

// retryTick · 一次扫描 · 处理到期的 pending 行。
//
// 每行:
//  1. attempt++ 后如果 <= MaxRetries · 重发一次 · 落新一行台账
//  2. 达到 MaxRetries · 标 failed 停扫
//
// **不批量**: 扫最多 50 行 · 免得单轮拉太多把整轮 tick 卡住。
func (d *Dispatcher) retryTick(ctx context.Context) error {
	rows, err := d.cfg.DB.QueryContext(ctx, `
		SELECT id, passenger_id, event_id, event_type, target_url, payload, attempt
		  FROM outbound_webhook_delivery
		 WHERE status = 'pending'
		   AND next_retry_at IS NOT NULL
		   AND next_retry_at <= ?
		 ORDER BY next_retry_at
		 LIMIT 50`, d.cfg.Now().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	defer rows.Close()

	type pendingRow struct {
		id, passengerID, eventID, eventType, targetURL, payload string
		attempt                                                 int
	}
	var todo []pendingRow
	for rows.Next() {
		var pr pendingRow
		if err := rows.Scan(&pr.id, &pr.passengerID, &pr.eventID, &pr.eventType,
			&pr.targetURL, &pr.payload, &pr.attempt); err != nil {
			return err
		}
		todo = append(todo, pr)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, pr := range todo {
		// 抢锁 · 把 next_retry_at 清 NULL(条件 UPDATE) · 防两个 tick 重投同一行
		// (status 保持 pending · 免得撞 CHECK 约束 · migration 003 只允许四种 status)
		res, err := d.cfg.DB.ExecContext(ctx, `
			UPDATE outbound_webhook_delivery
			   SET next_retry_at = NULL
			 WHERE id = ? AND next_retry_at IS NOT NULL`, pr.id)
		if err != nil {
			d.logger.Warn("webhookout: 抢锁 pending 失败", "id", pr.id, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// 已被别人抢过 · 跳过
			continue
		}
		// 抢到 · 走重发 · attempt+1
		if err := d.retryOne(ctx, pr.passengerID, pr.eventID, EventType(pr.eventType),
			pr.payload, pr.attempt+1); err != nil {
			d.logger.Warn("webhookout: 重发失败", "id", pr.id, "err", err)
		}
	}
	return nil
}

// retryOne · 重发一次 · payload 从台账读回来重新签名。
//
// **不重新走 runSend** — runSend 会重新拉 cfg / 解密 secret · 但已经知道 URL 存在
// (第一次入库时校验过) · 重新拉是保险(secret 可能轮换过)。
func (d *Dispatcher) retryOne(
	ctx context.Context,
	passengerID, eventID string, eventType EventType,
	payloadRaw string, attempt int,
) error {
	// 从台账重放 · payload 字节直接用(签名基于原字节 · 别重新 marshal)
	if d.cfg.Store == nil {
		return sql.ErrNoRows // dummy · 装配错
	}
	cfg, err := d.cfg.Store.Get(ctx, passengerID)
	if err != nil {
		return err
	}
	if cfg.WebhookURL == "" || !cfg.WebhookSecretConfigured {
		// 用户在两次重试之间删了 webhook cfg · 直接标 failed
		_ = d.markFailed(ctx, eventID, attempt-1) // 前一 attempt · 因为本轮还没落新行
		return nil
	}
	secret, err := d.cfg.Store.DecryptWebhookSecret(cfg.WebhookSecretEncrypted)
	if err != nil || secret == "" {
		_ = d.markFailed(ctx, eventID, attempt-1)
		return err
	}

	now := d.cfg.Now()
	body := []byte(payloadRaw)
	res := d.sendOnce(ctx, cfg, secret, eventID, eventType, body, now)

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
	// 落新一行(每次尝试独立一行 · retry_count 通过 attempt 反映)
	_, err = d.cfg.Store.InsertDelivery(ctx, DeliveryAttempt{
		PassengerID:    passengerID,
		EventID:        eventID,
		EventType:      string(eventType),
		TargetURL:      cfg.WebhookURL,
		Payload:        payloadRaw,
		Attempt:        attempt,
		Status:         status,
		ResponseStatus: respStatus,
		ResponseSnip:   res.BodySnip,
		LatencyMs:      &latency,
	})
	if err != nil {
		return err
	}
	if status == "pending" && attempt-1 < len(d.cfg.Backoffs) {
		nextAt := now.Add(d.cfg.Backoffs[attempt-1])
		_, _ = d.cfg.DB.ExecContext(ctx, `
			UPDATE outbound_webhook_delivery
			   SET next_retry_at = ?
			 WHERE event_id = ? AND attempt = ?`,
			nextAt.Format(time.RFC3339Nano), eventID, attempt)
	}
	return nil
}

// markFailed · 把某 event_id + attempt 那行标 failed(retrier 撤退用)。
func (d *Dispatcher) markFailed(ctx context.Context, eventID string, attempt int) error {
	_, err := d.cfg.DB.ExecContext(ctx, `
		UPDATE outbound_webhook_delivery
		   SET status = 'failed', next_retry_at = NULL
		 WHERE event_id = ? AND attempt = ?`, eventID, attempt)
	return err
}

// jsonMarshal · 让 dispatcher.go 能用 · 独立函数避免直接 import encoding/json
// (dispatcher.go 不 import encoding/json 保持接口纯净)
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
