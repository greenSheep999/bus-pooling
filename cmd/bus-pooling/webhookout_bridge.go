package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/webhookout"
)

// webhookoutBridge · 装配 webhookout 需要的三层 adapter：
//
//   1. downstreamStoreAdapter · downstream.Store 的窄化(缓 URL / 解密 / 落台账)
//   2. httpxAdapter · httpx.Client 的窄化(Do)
//   3. dispatcher_test 用 mockHTTP 直连 httptest · 生产走 httpxAdapter
//
// 加这一层是因为 webhookout 包**不 import** downstream / httpx(避免包依赖循环) ·
// 只声明它需要的接口 · 装配层做类型翻译。

// downstreamStoreAdapter · 把 *downstream.Store 转成 webhookout.DownstreamStore。
type downstreamStoreAdapter struct {
	store *downstream.Store
}

func newDownstreamAdapter(s *downstream.Store) webhookout.DownstreamStore {
	if s == nil {
		return nil
	}
	return &downstreamStoreAdapter{store: s}
}

func (a *downstreamStoreAdapter) Get(ctx context.Context, pid string) (webhookout.DownstreamConfig, error) {
	cfg, err := a.store.Get(ctx, pid)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return webhookout.DownstreamConfig{}, err
	}
	return webhookout.DownstreamConfig{
		PassengerID:             cfg.PassengerID,
		WebhookURL:              cfg.WebhookURL,
		WebhookSecretEncrypted:  cfg.WebhookSecretEncrypted,
		WebhookSecretConfigured: cfg.WebhookSecretConfigured,
		PushOnPull:              cfg.PushOnPull,
		ResyncOnDead:            cfg.ResyncOnDead,
		BusOnly:                 cfg.BusOnly,
	}, nil
}

func (a *downstreamStoreAdapter) DecryptWebhookSecret(b []byte) (string, error) {
	return a.store.DecryptWebhookSecret(b)
}

func (a *downstreamStoreAdapter) InsertDelivery(ctx context.Context, wa webhookout.DeliveryAttempt) (webhookout.DeliveryRow, error) {
	d, err := a.store.InsertDelivery(ctx, downstream.RecordAttempt{
		PassengerID:    wa.PassengerID,
		EventID:        wa.EventID,
		EventType:      wa.EventType,
		TargetURL:      wa.TargetURL,
		Payload:        wa.Payload,
		Attempt:        wa.Attempt,
		Status:         wa.Status,
		ResponseStatus: wa.ResponseStatus,
		ResponseSnip:   wa.ResponseSnip,
		LatencyMs:      wa.LatencyMs,
	})
	if err != nil {
		return webhookout.DeliveryRow{}, err
	}
	return webhookout.DeliveryRow{ID: d.ID}, nil
}

// httpxAdapter · 把 *httpx.Client 转成 webhookout.HTTPDoer。
//
// httpx.Client.Do 走 3 次重试 · webhook 出向不该走这条(retrier 有自己的重试语义) ·
// 装配层用独立 httpx 配 MaxRetries=0 · 保证一次发送只发一次。
type httpxAdapter struct {
	c *httpx.Client
}

func newHTTPXAdapter(c *httpx.Client) webhookout.HTTPDoer {
	if c == nil {
		return nil
	}
	return &httpxAdapter{c: c}
}

func (a *httpxAdapter) Do(ctx context.Context, req *webhookout.HTTPReq) (*webhookout.HTTPResp, error) {
	r, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	// 直接用 httpx.Do · 让统一超时 + 代理生效
	resp, err := a.c.Do(ctx, r)
	if err != nil {
		return nil, err
	}
	return &webhookout.HTTPResp{
		StatusCode: resp.StatusCode,
		Body:       resp.Body,
	}, nil
}

// buildWebhookout · main.go 调 · 装配一个能真发的 Dispatcher。
//
// 返 nil 表示未装配(cipher 未装 · 走 dry-run · api 层 SendTest 走 1a 兼容分支)。
func buildWebhookout(database *sql.DB, dstore *downstream.Store) *webhookout.Dispatcher {
	if dstore == nil {
		return nil
	}
	// webhook 出向独立 httpx · MaxRetries=0(retrier 走 next_retry_at 扫库重试 · 不走 httpx 层)
	hc, err := httpx.New(httpx.Config{
		Timeout:    8 * time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		slog.Warn("webhookout 装 httpx 失败 · Dispatcher 未装配", "err", err)
		return nil
	}
	return webhookout.New(webhookout.Config{
		DB:         database,
		Store:      newDownstreamAdapter(dstore),
		HTTPX:      newHTTPXAdapter(hc),
		Logger:     slog.Default(),
		Timeout:    8 * time.Second,
		MaxRetries: 3,
		Backoffs: []time.Duration{
			3 * time.Second,
			8 * time.Second,
			20 * time.Second,
		},
		QueueSize: 1024,
	})
}

// ── 三个 Notifier 桥 · 装配层实现 · 主链不依赖 webhookout 直接 ─────────

// pullSuccessBridge · decider.PullSuccessNotifier 的实现。
//
// 拉号成功后主链走 · 装配层查是否多人车 · fanout 到 participants。
// 目前先只通知发起人(1e-2 简化) · 阶段 3+ 补 fanout(需要读 pull_round.participants_split_json)。
type pullSuccessBridge struct {
	disp   *webhookout.Dispatcher
	db     *sql.DB
	logger *slog.Logger
}

func (b *pullSuccessBridge) OnPullSucceeded(ctx context.Context,
	passengerID, busID, vendorID, pullRoundID string,
	credentialIDs []string, newKeys int,
) {
	if b.disp == nil {
		return
	}
	// vendor label · 打码 · 阶段 1e-2 简化用固定 "provider"(未来接 vendorview.anon)
	b.disp.Dispatch(ctx, passengerID, webhookout.EventNewKeysAvailable,
		webhookout.NewKeysAvailablePayload{
			EnvelopeMeta:  buildBoardedEnv(b.disp, passengerID, webhookout.EventNewKeysAvailable),
			BusID:         busID,
			VendorLabel:   "provider",
			NewKeys:       newKeys,
			PullRoundID:   pullRoundID,
			CredentialIDs: credentialIDs,
		})
}

// deathBridge · deathwatch.DeathNotifier 的实现。
//
// 每号死一次调用 · 装配层判"该 bus 是否全灭" · 全灭才发 all_keys_dead。
// **单号死不发**(阶段 1e-2 简化) · 单号事件走 credential.dead 一档(待后续 sprint 加)。
type deathBridge struct {
	disp   *webhookout.Dispatcher
	db     *sql.DB
	logger *slog.Logger
}

func (b *deathBridge) OnCredentialDead(ctx context.Context, credentialLedgerID, vendorID string) {
	if b.disp == nil {
		return
	}
	// 反查该号所在的 bus 和 passenger
	var ownerBusID, ownerRecordPassengerID sql.NullString
	err := b.db.QueryRowContext(ctx, `
		SELECT owner_bus_id, owner_record_passenger_id
		  FROM credential_ledger WHERE id = ?`, credentialLedgerID).Scan(&ownerBusID, &ownerRecordPassengerID)
	if err != nil {
		b.logger.Warn("deathBridge: 反查号所在 bus 失败", "cred", credentialLedgerID, "err", err)
		return
	}
	if !ownerBusID.Valid {
		return // 单独拉号 · 不发 all_keys_dead(1e-2 简化)
	}
	// 判该 bus 是否全灭
	var alive int
	err = b.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM credential_ledger
		 WHERE owner_bus_id = ? AND status = 'alive'`, ownerBusID.String).Scan(&alive)
	if err != nil || alive > 0 {
		return
	}
	// 全灭 · 通知车主(bus.creator_passenger_id 是发起人 · 简化:只通知车主)
	var creatorID string
	err = b.db.QueryRowContext(ctx, `
		SELECT creator_passenger_id FROM bus WHERE id = ?`, ownerBusID.String).Scan(&creatorID)
	if err != nil || creatorID == "" {
		return
	}
	b.disp.Dispatch(ctx, creatorID, webhookout.EventAllKeysDead,
		webhookout.AllKeysDeadPayload{
			EnvelopeMeta: buildBoardedEnv(b.disp, creatorID, webhookout.EventAllKeysDead),
			BusID:        ownerBusID.String,
			DiedAt:       time.Now().UTC().Format(time.RFC3339),
			VendorLabel:  "provider",
		})
}

// refundBridge · deathwatch.RefundNotifier 的实现。
//
// RefundOnce 里每笔退款(每 passenger)一次调用。
type refundBridge struct {
	disp *webhookout.Dispatcher
	db   *sql.DB
}

func (b *refundBridge) OnRefundIssued(ctx context.Context,
	passengerID string, amount int64,
	credentialLedgerID, busID string,
) {
	if b.disp == nil {
		return
	}
	b.disp.Dispatch(ctx, passengerID, webhookout.EventWarrantyRefund,
		webhookout.WarrantyRefundPayload{
			EnvelopeMeta: buildBoardedEnv(b.disp, passengerID, webhookout.EventWarrantyRefund),
			Amount:       amount,
			CredentialID: credentialLedgerID,
			BusID:        busID,
			RefundedAt:   time.Now().UTC().Format(time.RFC3339),
		})
}

// janitorBoardedBridge · handoff.BoardedNotifier 的装配层实现。
//
// 卡单重试成功也走 boarded · 跟 api handler confirm 成功共用同一份 · 用户体验一致。
type janitorBoardedBridge struct {
	disp *webhookout.Dispatcher
}

func (b *janitorBoardedBridge) NotifyBoarded(ctx context.Context, passengerID string, credentialIDs []string, route string) {
	if b.disp == nil {
		return
	}
	b.disp.NotifyBoarded(ctx, passengerID, credentialIDs, route)
}

// buildBoardedEnv · 便捷拼 EnvelopeMeta · 只在 bridge 用(避免各处重复代码)
func buildBoardedEnv(disp *webhookout.Dispatcher, passengerID string, evt webhookout.EventType) webhookout.EnvelopeMeta {
	// Dispatcher 内部会用自己的 NewEventID · 但 payload 顶层要 event_id · 装配层给一次
	// (Dispatcher 消费时会用 item.eventID 走 header · payload 里的 event_id 只作冗余)
	return webhookout.EnvelopeMeta{
		Event:       evt,
		EventID:     disp.NewEventID(),
		Timestamp:   time.Now().UTC().Unix(),
		PassengerID: passengerID,
	}
}

// _ · io 保留占位 · 未来 stream 分片时可能用
var _ = io.EOF
