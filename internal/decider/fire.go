package decider

// 抢号链的 fire 入口 · 实现 stockwatch.Firer 接口。
//
// 调用链：
//
//	vendor restock 事件（webhook / 探针 stock-delta / xi8 signal）
//	  → stockwatch.Watcher.Notify
//	    → conditional UPDATE 抢 fired 状态（只一个 goroutine 中）
//	      → Orchestrator.FireWatcher（本文件）
//	        → Orchestrator.Pull（走完整拉号链 · 冻结/扣款/入池/落账全套）
//
// **为什么不直接调 vendor.Purchase**：拉号不只是"买"· 还要冻结、按 share_pct 分摊、
// 入 housepool、落 pull_round、结算多退少补。Pull 已经把这些串好且有崩溃恢复
// （09-transactions §2）· 绕过它等于重写一遍状态机。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
	"github.com/google/uuid"
)

// FireWatcher · 抢号链唤醒后走一次真拉号。实现 stockwatch.Firer。
//
// 返回值语义（stockwatch 按这个决定挂单的下一个状态）：
//   - nil                        → 抢到了 · 挂单标 fulfilled
//   - stockwatch.ErrStillNoStock → vendor 又空了 · 挂单回退 watching 等下次
//   - 其他 error                 → 硬失败 · 挂单标 expired（janitor 释放冻结）
//
// **余额不足算硬失败**：用户挂单期间把钱花在别处了 · 这轮抢不成。标 expired 让他
// 重新挂（而不是一直占着队列位置反复失败）。
func (o *Orchestrator) FireWatcher(ctx context.Context, w stockwatch.WatcherRow) error {
	if o == nil {
		return errors.New("decider: Orchestrator 未装配")
	}

	// fire 不是 HTTP 请求 · 没有客户端传的 X-Idempotency-Key · 自己合成一条
	// idempotency_record（Pull 要求 IdempotencyRecordID 非空 · FK 到那张表）。
	recordID, err := o.ensureFireIdempotencyRecord(ctx, w)
	if err != nil {
		return fmt.Errorf("decider: fire 建幂等记录: %w", err)
	}

	_, err = o.Pull(ctx, PullInput{
		PassengerID:         w.PassengerID,
		BusID:               w.BusID,
		Count:               w.Count,
		Zone:                providers.Zone(w.Region),
		IdempotencyRecordID: recordID,
		VendorID:            providers.VendorID(w.VendorID),
		// 复用挂单时定的 client_order_id · vendor 侧幂等 · 防"回退后再 fire"重复扣
		ClientOrderID: w.ClientOrderID,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoStock):
		// vendor 又空了（探针看到的库存已经被别人抢走）· 让挂单继续等
		return stockwatch.ErrStillNoStock
	case errors.Is(err, ErrRateLimited):
		// 被限流 · 也当"再等等" · 下次事件再试（不浪费队列位置）
		return stockwatch.ErrStillNoStock
	case errors.Is(err, ErrPartialFill):
		// 部分成交不是错（差额已退）· 拿到号了就算成
		return nil
	default:
		return err
	}
}

// maybeEnqueueOnNoStock · 缺货时挂单等补货 · 返 true 表示已挂上。
//
// **三道门 · 全过才挂**：
//
//	① enqueuer 装配了（nil = 老行为 · 缺货直接失败）
//	② auto 模式 —— in.VendorID 空。用户明确指定 vendor 时不代抢：他要等的是那家 ·
//	   给他挂到别家违背意图（decisions §11.15）
//	③ 不是 fire 自己触发的 —— in.ClientOrderID 非空说明这轮就是 fire 来的 ·
//	   再挂会把刚 fire 的挂单复位成 watching · 死循环
//
// **不预冻结**：挂单时不冻钱 · fire 时走完整 Pull 现冻现扣。理由：
//   - 缺货时拿不到真单价（stock=0 的 zone 没有 unit_price）· 冻多少无依据
//   - 冻 10 分钟不给用户用 · 但 fire 时可能又抢不到 · 白占款
//   - 代价：fire 时余额可能已被花掉 → 那轮硬失败标 expired（可接受 · 用户重挂即可）
//
// 挂单失败只 log 不影响主流程 —— 缺货本来就是要返 ErrNoStock 的 · 挂单是增值尝试。
func (o *Orchestrator) maybeEnqueueOnNoStock(
	ctx context.Context, in PullInput, vendorID providers.VendorID,
) bool {
	if o.enqueuer == nil || in.VendorID != "" || in.ClientOrderID != "" {
		return false
	}
	// 挂单自己的 vendor 幂等键 · fire 时复用它调 Pull
	clientOrderID, err := newClientOrderID()
	if err != nil {
		return false
	}
	id, err := o.enqueuer.Enqueue(ctx, stockwatch.EnqueueParams{
		PassengerID:   in.PassengerID,
		BusID:         in.BusID,
		TargetGroup:   groupFor(in.BusID, in.PassengerID),
		VendorID:      string(vendorID),
		Region:        string(in.Zone),
		ClientOrderID: clientOrderID,
		Count:         in.Count,
		MaxUnitPrice:  in.MaxUnitPrice,
	})
	if err != nil {
		return false
	}
	_ = id
	return true
}

// ensureFireIdempotencyRecord · 为一次 fire 合成 idempotency_record。
//
// **key 用挂单 id 派生 · 确定性**：同一个挂单反复 fire（回退 watching 后再来）
// 会命中同一条 idempotency_record · 不会每次 fire 都堆一条垃圾行。
//
// 但注意 —— 这里的幂等**不是**拉号幂等的主保障。真正防重复扣款靠
// `client_order_id`（vendor 侧）+ `pending_purchase.UNIQUE(vendor_id, client_order_id)`。
// 这条记录只是满足 Pull 的 FK 要求 + 给运维一个"这轮是抢号链触发的"痕迹。
func (o *Orchestrator) ensureFireIdempotencyRecord(
	ctx context.Context, w stockwatch.WatcherRow,
) (string, error) {
	// 32 hex · 跟 api 层的 hexKeyRe 同格式（虽然这条不过那层校验 · 保持一致便于排查）
	sum := sha256.Sum256([]byte("stockwatch-fire:" + w.ID))
	key := hex.EncodeToString(sum[:16])

	const path = "/internal/stockwatch/fire"
	fingerprint := hex.EncodeToString(sum[16:])
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	res, err := o.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, 'POST', ?, ?, ?, ?)`,
		id, w.PassengerID, path, key, fingerprint, now)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return id, nil
	}
	// 已存在（同挂单之前 fire 过）· 取回那条的 id
	var existingID string
	err = o.db.QueryRowContext(ctx, `
		SELECT id FROM idempotency_record
		 WHERE passenger_id = ? AND path = ? AND idempotency_key = ?`,
		w.PassengerID, path, key).Scan(&existingID)
	if err != nil {
		return "", err
	}
	return existingID, nil
}
