package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ListLedger · GET /api/my/credits · vendor 侧积分流水（交叉对账 · docs/23-endpoints-todo §1）。
//
// **响应形状实测确认**（2026-08-14 · vendor-probe 抓的真实响应 · 不是猜的）：
//
//	{"credits":70,
//	 "ledger":[
//	   {"id":686,"kind":"claim_key","amount":-30,"balance_after":70,
//	    "ref_id":"88234229...","note":"欧洲区自助提货 Key #4415","created_at":"2026-08-09 17:14:05"},
//	   {"id":131,"kind":"recharge","amount":100,"balance_after":100,
//	    "ref_id":"KA135...","note":"支付宝充值 100.00 元到账","created_at":"2026-08-06 17:00:28"}],
//	 "master_price":500, "risk_flag":0, ...}
//
// 要点（都对着真实响应）：
//   - 外层是 `credits`(余额) + `ledger`[] · 不是另一家的 `entries`
//   - `amount` **已带符号**（claim_key -30 · recharge +100）· 但我方仍按 reason 定方向（统一口径）
//   - `ref_id` 是对账 join 键（跟我方 pull_round 对）
//   - `created_at` 是**北京墙钟无 tz** —— 走 parseKiroooTime（跟 history.go 同一个时区修正）
//   - 单位是积分（1 积分 = 1 CNY = 1 RMB · §1.4）→ ×1_000_000 到 microunit
func (a *Adapter) ListLedger(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorLedgerEntry], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/credits", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo: credits: http %d", resp.StatusCode)
	}
	var wrap creditsResp
	if err := json.Unmarshal(resp.Body, &wrap); err != nil {
		return nil, fmt.Errorf("kirooo: credits 解析: %w", err)
	}

	out := make([]providers.VendorLedgerEntry, 0, len(wrap.Ledger))
	for _, r := range wrap.Ledger {
		raw, _ := json.Marshal(r)
		reason := normalizeKiroooKind(r.Kind)

		// amount 已带符号（credits 单位）· 取绝对值再按 reason 定方向（跟另一家口径一致）
		mag := r.Amount
		if mag < 0 {
			mag = -mag
		}
		micro := int64(mag) * 1_000_000
		if reason == providers.LedgerPurchase {
			micro = -micro
		}

		out = append(out, providers.VendorLedgerEntry{
			EntryID:      strconv.FormatInt(r.ID, 10),
			OrderID:      r.RefID,
			Reason:       reason,
			RawReason:    r.Kind,
			Amount:       providers.Money{Amount: micro, Currency: providers.CurrencyCredit},
			BalanceAfter: providers.Money{Amount: int64(r.BalanceAfter) * 1_000_000, Currency: providers.CurrencyCredit},
			CreatedAt:    parseKiroooTime(r.CreatedAt),
			Raw:          raw,
		})
	}
	return &providers.HistoryPage[providers.VendorLedgerEntry]{Items: out}, nil
}

// creditsResp · /api/my/credits 响应（实测形状）
type creditsResp struct {
	Credits int          `json:"credits"`
	Ledger  []creditsRow `json:"ledger"`
}

type creditsRow struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Amount       int    `json:"amount"` // 带符号 · 积分
	BalanceAfter int    `json:"balance_after"`
	RefID        string `json:"ref_id"`
	Note         string `json:"note"`
	CreatedAt    string `json:"created_at"` // 北京墙钟无 tz
}

// normalizeKiroooKind · kirooo kind → 我方 6 类。
//
// 实测见过 claim_key / recharge · 其余按语义映（保守 · 没见过的归 other · 别硬猜）。
func normalizeKiroooKind(kind string) string {
	switch kind {
	case "claim_key":
		return providers.LedgerPurchase // 提货扣费
	case "recharge":
		return providers.LedgerRecharge
	case "refund", "warranty", "warranty_refund":
		return providers.LedgerRefund
	case "adjust", "admin_adjust":
		return providers.LedgerAdjust
	default:
		return providers.LedgerOther
	}
}
