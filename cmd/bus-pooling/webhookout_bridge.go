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

	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
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
		Enabled:                 cfg.WebhookEnabled, // 1e-2 P0-1
		Events:                  cfg.WebhookEvents,  // 1e-2 P0-2 · nil = 全订阅兜底
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
//
// **I-02 · 进车后自动推下游**(2026-08-22):
// 号进车 · 若乘客配了 downstream URL + token + push_on_pull=true(默认) ·
// 异步 fire-and-forget 走 pusher.Push。三个场景全走这里:
//   - 建车首次拉(handleCreateBus → decider.Pull)
//   - 自动补车(deathwatch.RefillTick → decider.Pull)
//   - 拼车拉号(handleBusPull → decider.Pull)
// 推池失败落 push_error_code · 用户走 BusDetail 手动重推(§8.44)。
type pullSuccessBridge struct {
	disp        *webhookout.Dispatcher
	pusher      passengerpool.Pusher
	downstreams *downstream.Store
	vendorView  *vendorview.Service // I-13 · vendor label 走匿名映射 · 不透传真名
	db          *sql.DB
	logger      *slog.Logger
}

func (b *pullSuccessBridge) OnPullSucceeded(ctx context.Context,
	passengerID, busID, vendorID, pullRoundID string,
	credentialIDs []string, newKeys int,
) {
	// vendor label · 走 vendorview 匿名(I-13 修 · 之前硬编 "provider" 是内部术语)·
	// 拿不到 vendorView 就退回 vendorID 打码前 3 位 · 保证不出内部术语
	vendorLabel := "vendor"
	if b.vendorView != nil {
		if l := b.vendorView.AnonLabelFor(vendorID); l != "" {
			vendorLabel = l
		}
	}

	// (a) 对外 webhook 通知 · nil 兼容(1a 装配路径)
	if b.disp != nil {
		b.disp.Dispatch(ctx, passengerID, webhookout.EventNewKeysAvailable,
			webhookout.NewKeysAvailablePayload{
				EnvelopeMeta:  buildBoardedEnv(b.disp, passengerID, webhookout.EventNewKeysAvailable),
				BusID:         busID,
				VendorLabel:   vendorLabel,
				NewKeys:       newKeys,
				PullRoundID:   pullRoundID,
				CredentialIDs: credentialIDs,
			})
	}

	// (b) I-02 · 进车 = 自动推下游 · 只对 into_bus 场景(busID 非空) ·
	// 单独拉号(record 待派)不自动推 —— 用户会去 /extract 页选去向
	if busID == "" || len(credentialIDs) == 0 {
		return
	}
	if b.pusher == nil || b.downstreams == nil {
		return // 装配未接下游 · 沉默(测试 / dev 环境)
	}
	// 查乘客 downstream 配置 · 无配 or push_on_pull=false → 跳过
	cfg, err := b.downstreams.Get(ctx, passengerID)
	if err != nil {
		// 乘客没配 downstream · Get 返 err · 正常路径 · 不发警告
		return
	}
	if !cfg.PushOnPull {
		return // 用户显式关了(reasonable · 尊重设置)
	}
	// 异步推 · 别阻塞主链 · 单个号失败不回滚进车(号已在车里 · 推池独立生命周期)
	go b.autoPush(passengerID, credentialIDs, vendorLabel)
}

// AutoPushOnAssign · api handleAssign into_bus 分支调 · 场景 3
// 跟 OnPullSucceeded 里的 autoPush 走同一路径 · 抽公开方法让 api 层能直接注入 hook
func (b *pullSuccessBridge) AutoPushOnAssign(ctx context.Context, passengerID string, credIDs []string) {
	if b.pusher == nil || b.downstreams == nil || len(credIDs) == 0 {
		return
	}
	cfg, err := b.downstreams.Get(ctx, passengerID)
	if err != nil || !cfg.PushOnPull {
		return
	}
	// vendor label 从第一号推断 · 派进车通常同 vendor(mixed 也接受 · label 只是展示)
	vendorLabel := "vendor"
	if b.vendorView != nil {
		var vid string
		if err := b.db.QueryRowContext(ctx,
			`SELECT vendor_id FROM credential_ledger WHERE id = ?`, credIDs[0],
		).Scan(&vid); err == nil {
			if l := b.vendorView.AnonLabelFor(vid); l != "" {
				vendorLabel = l
			}
		}
	}
	go b.autoPush(passengerID, credIDs, vendorLabel)
}

// autoPush · 后台 goroutine · 推池 + 落 push_error_code(失败时) · 让用户手动重推
func (b *pullSuccessBridge) autoPush(passengerID string, credIDs []string, vendorLabel string) {
	// 新 ctx · 主链 ctx 可能已随请求结束 cancel · 用 5min 独立 ctx
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 查各号的 region · pusher 要
	metas := make(map[string]struct{ region string }, len(credIDs))
	for _, cid := range credIDs {
		var region sql.NullString
		if err := b.db.QueryRowContext(ctx,
			`SELECT COALESCE(region, '') FROM credential_ledger WHERE id = ?`, cid,
		).Scan(&region); err == nil {
			metas[cid] = struct{ region string }{region.String}
		}
	}

	creds := make([]passengerpool.PushCredential, 0, len(credIDs))
	for _, cid := range credIDs {
		creds = append(creds, passengerpool.PushCredential{
			CredentialID: cid,
			Region:       metas[cid].region,
			VendorLabel:  vendorLabel,
		})
	}

	result, err := b.pusher.Push(ctx, passengerID, creds)
	if err != nil {
		// 顶层错(拿明文失败 / 拉配置失败 / ErrNoTarget)· 全批标 push_error_code=stream_broken
		b.logger.Warn("I-02 · auto push 顶层失败", "passenger", passengerID, "err", err)
		for _, cid := range credIDs {
			b.markPushError(ctx, cid, "stream_broken", "自动推池失败 · 稍后可手动重推")
		}
		return
	}
	// 成功号 · 标 pushed_to_passengerpool_at
	ok := map[string]bool{}
	for _, id := range result.Success {
		ok[id] = true
	}
	for _, id := range result.Duplicate {
		ok[id] = true // duplicate 视为成功
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, cid := range credIDs {
		if ok[cid] {
			if _, uerr := b.db.ExecContext(ctx, `
				UPDATE credential_ledger
				   SET pushed_to_passengerpool_at = ?,
				       push_error_code = NULL, push_error_status = NULL,
				       push_error_message = NULL, push_error_retriable = NULL,
				       push_attempts = COALESCE(push_attempts, 0) + 1,
				       push_last_attempt_at = ?
				 WHERE id = ?`, now, now, cid); uerr != nil {
				b.logger.Warn("I-02 · 落 pushed_at 失败", "cred", cid, "err", uerr)
			}
		}
	}
	// 失败号 · 落 push_error_* · 让用户去 BusDetail 手动重推
	for _, f := range result.Failed {
		msg := "对家未接受此号"
		if f.Err != nil {
			msg = f.Err.Message
		}
		code := "bad_request"
		if f.Err != nil {
			code = string(f.Err.Kind)
		}
		b.markPushError(ctx, f.CredentialID, code, msg)
	}
	b.logger.Info("I-02 · auto push 完成",
		"passenger", passengerID,
		"success", len(result.Success),
		"duplicate", len(result.Duplicate),
		"failed", len(result.Failed))
}

// markPushError · 落 credential_ledger.push_error_* 六字段(跟手动重推同一路径)
func (b *pullSuccessBridge) markPushError(ctx context.Context, credID, code, msg string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// truncate msg 200 字符 · 防 DoS
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if _, err := b.db.ExecContext(ctx, `
		UPDATE credential_ledger
		   SET push_error_code = ?, push_error_status = NULL,
		       push_error_message = ?, push_error_retriable = 1,
		       push_attempts = COALESCE(push_attempts, 0) + 1,
		       push_last_attempt_at = ?
		 WHERE id = ?`, code, msg, now, credID); err != nil {
		b.logger.Warn("I-02 · 落 push_error 失败", "cred", credID, "err", err)
	}
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
