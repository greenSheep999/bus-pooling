package kiroceo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 编译期保证 *Adapter 满足 LedgerLister · backfiller 靠 runtime 断言接线（签名错这里先炸）。
var _ providers.LedgerLister = (*Adapter)(nil)

// ListLedger · GET /api/me/ledger · vendor 侧积分流水（交叉对账 · docs/20 §1）。
//
// **外层形状实测确认**（vendor-probe）：分页信封 · 跟本家 orders 列表同源：
//
//	{"items":[...], "page":1, "page_size":50, "pages":0, "total":0}
//
// **⚠️ item 字段是推断的** —— 抓的时候我方账户 0 笔流水（items 空）· 没见到真 item。
// 按本家惯例（snake_case · 北京墙钟时间 · order_id）+ 账单页 tab（兑换 / 提货 / 管理员调整 /
// 退款）推断字段名 · **永远存 Raw** · 有真数据后核 vendor_ledger.raw 再收紧。
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
		return nil, fmt.Errorf("kiroceo: ledger: http %d", resp.StatusCode)
	}
	var wrap ledgerResp
	if err := json.Unmarshal(resp.Body, &wrap); err != nil {
		return nil, fmt.Errorf("kiroceo: ledger 解析: %w", err)
	}

	out := make([]providers.VendorLedgerEntry, 0, len(wrap.Items))
	for _, r := range wrap.Items {
		raw, _ := json.Marshal(r)
		rawType := r.Type
		if rawType == "" {
			rawType = r.Reason
		}
		reason := normalizeCeoLedgerType(rawType)

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
			entryID = ceoLedgerFingerprint(rawType, orderID, r.CreatedAt, micro)
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

// ledgerResp · /api/me/ledger 分页信封（本家 orders 同源）
type ledgerResp struct {
	Items    []ceoLedgerRow `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Pages    int            `json:"pages"`
	Total    int            `json:"total"`
}

// ceoLedgerRow · item 字段**推断**（抓时 items 空）· 多个可能名都留 · 有真数据再收紧
type ceoLedgerRow struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Reason        string `json:"reason"` // 兜底别名
	Amount        int64  `json:"amount"`
	BalanceAfter  int64  `json:"balance_after"`
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	CreatedAt     string `json:"created_at"`
}

// normalizeCeoLedgerType · kiroceo type → 我方 6 类（推断 · 有真数据再补全）
func normalizeCeoLedgerType(t string) string {
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

func ceoLedgerFingerprint(typ, orderID, created string, micro int64) string {
	return fmt.Sprintf("fp-%s-%s-%d", typ, orderID, micro) + "-" + created
}
