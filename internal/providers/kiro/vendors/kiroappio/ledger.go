package kiroappio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ListLedger · GET /api/me/ledger · vendor 侧积分流水（交叉对账 · docs/20 §1）。
//
// **外层形状实测确认**（2026-08-14 · vendor-probe）：分页信封 · 跟本家 orders/keys 一套：
//
//	{"items":[...], "page":1, "page_size":50, "pages":0, "total":0,
//	 "summary":{"total_in":0, "total_out":0}}
//
// **⚠️ item 字段是推断的** —— 抓的时候我方账户 0 笔流水（items 空）· 没见到真 item。
// 按本家惯例（snake_case · RFC3339 时间 · order_id）+ 档案"8+ 种 type" 推断字段名 ·
// **永远存 Raw** · 有真数据后核 vendor_ledger.raw 再收紧（用 vendor-probe 复查）。
func (a *Adapter) ListLedger(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorLedgerEntry], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/me/ledger", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappio: ledger: http %d", resp.StatusCode)
	}
	var wrap paged[ioLedgerRow]
	if err := json.Unmarshal(resp.Body, &wrap); err != nil {
		return nil, fmt.Errorf("kiroappio: ledger 解析: %w", err)
	}

	out := make([]providers.VendorLedgerEntry, 0, len(wrap.Items))
	for _, r := range wrap.Items {
		raw, _ := json.Marshal(r)
		rawType := r.Type
		if rawType == "" {
			rawType = r.Reason // 兜底别名
		}
		reason := normalizeIoType(rawType)

		mag := r.Amount
		if mag < 0 {
			mag = -mag
		}
		micro := mag * 1_000_000
		if reason == providers.LedgerPurchase {
			micro = -micro
		}
		orderID := r.OrderID
		if orderID == "" {
			orderID = r.ClientOrderID
		}
		entryID := r.ID
		if entryID == "" {
			entryID = ioLedgerFingerprint(rawType, orderID, r.CreatedAt, micro)
		}
		out = append(out, providers.VendorLedgerEntry{
			EntryID:      entryID,
			OrderID:      orderID,
			Reason:       reason,
			RawReason:    rawType,
			Amount:       providers.Money{Amount: micro, Currency: providers.CurrencyCredit},
			BalanceAfter: providers.Money{Amount: r.BalanceAfter * 1_000_000, Currency: providers.CurrencyCredit},
			CreatedAt:    parseHistTime(r.CreatedAt),
			Raw:          raw,
		})
	}
	return &providers.HistoryPage[providers.VendorLedgerEntry]{Items: out}, nil
}

// ioLedgerRow · item 字段**推断**（抓时 items 空）· 多个可能名都留 · 有真数据再收紧
type ioLedgerRow struct {
	ID            string `json:"id"`
	Type          string `json:"type"`   // 档案称 "8+ 种 type"
	Reason        string `json:"reason"` // 兜底别名
	Amount        int64  `json:"amount"`
	BalanceAfter  int64  `json:"balance_after"`
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	CreatedAt     string `json:"created_at"`
}

// normalizeIoType · kiroappio type → 我方 6 类（推断 · 有真数据再补全）
func normalizeIoType(t string) string {
	switch t {
	case "purchase", "claim", "claim_key", "buy":
		return providers.LedgerPurchase
	case "recharge", "topup", "redeem":
		return providers.LedgerRecharge
	case "refund", "warranty", "warranty_refund":
		return providers.LedgerRefund
	case "income", "payout":
		return providers.LedgerIncome
	case "adjust", "admin_adjust":
		return providers.LedgerAdjust
	default:
		return providers.LedgerOther
	}
}

func ioLedgerFingerprint(typ, orderID, created string, micro int64) string {
	return fmt.Sprintf("fp-%s-%s-%d", typ, orderID, micro) + "-" + created
}
