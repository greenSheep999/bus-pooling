package kiro91

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ListLedger · GET /api/my/ledger · vendor 侧积分流水（交叉对账 · docs/20 §1）。
//
// **⚠️ 响应 schema 是文档推断的**（vendor 档 §2.8 只列了 7 种 reason · 没给 JSON 形状）。
// 吸取 kiroappcc webhook 100% 丢的教训（照猜字段名写 parser）· 这里用**容错解析**：
//   - 外层包装名试多个（ledger / entries / items / data）· 都不中当裸数组
//   - 每笔字段名试多个（amount / credits / amount_fen …）
//   - **永远存 Raw** —— 上线后对着 vendor_ledger.raw 核字段 · 再收紧
//
// reason 归一（vendor 7 种 → 我方 6 类）· 方向由 reason 定（不信 amount 的符号）：
//
//	purchase/clawback → 扣费（负）· recharge/income/warranty/adjust/commit → 入账（正）
func (a *Adapter) ListLedger(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorLedgerEntry], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/ledger", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiro91: ledger: http %d", resp.StatusCode)
	}

	rows, err := parseLedgerRows(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kiro91: ledger 解析: %w", err)
	}

	out := make([]providers.VendorLedgerEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, ledgerRowToEntry(r))
	}
	return &providers.HistoryPage[providers.VendorLedgerEntry]{Items: out}, nil
}

// ledgerRow · 容错字段集 · 每个语义试多个 vendor 可能用的名字。
type ledgerRow struct {
	ID       string          `json:"id"`
	EntryID  string          `json:"entry_id"`
	Reason   string          `json:"reason"`
	OrderID  string          `json:"order_id"`
	ClientID string          `json:"client_order_id"`
	Amount   *float64        `json:"amount"`
	Credits  *float64        `json:"credits"`
	AmountF  *float64        `json:"amount_fen"`
	Balance  *float64        `json:"balance"`
	BalAfter *float64        `json:"balance_after"`
	Created  string          `json:"created_at"`
	Time     string          `json:"time"`
	raw      json.RawMessage `json:"-"`
}

// parseLedgerRows · 外层可能是 {ledger:[]} / {entries:[]} / {items:[]} / {data:[]} / 裸数组。
func parseLedgerRows(body []byte) ([]ledgerRow, error) {
	var wrap struct {
		Ledger  []json.RawMessage `json:"ledger"`
		Entries []json.RawMessage `json:"entries"`
		Items   []json.RawMessage `json:"items"`
		Data    []json.RawMessage `json:"data"`
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &wrap); err == nil {
		switch {
		case len(wrap.Ledger) > 0:
			raws = wrap.Ledger
		case len(wrap.Entries) > 0:
			raws = wrap.Entries
		case len(wrap.Items) > 0:
			raws = wrap.Items
		case len(wrap.Data) > 0:
			raws = wrap.Data
		}
	}
	if raws == nil {
		// 试裸数组
		if err := json.Unmarshal(body, &raws); err != nil {
			// 三种包装 + 裸数组都不中 · 返空不报错（上线看 raw 再修 · 别 crash backfill）
			return nil, nil
		}
	}
	out := make([]ledgerRow, 0, len(raws))
	for _, rm := range raws {
		var r ledgerRow
		if err := json.Unmarshal(rm, &r); err != nil {
			continue
		}
		r.raw = rm
		out = append(out, r)
	}
	return out, nil
}

func ledgerRowToEntry(r ledgerRow) providers.VendorLedgerEntry {
	reason := normalizeReason(r.Reason)

	// 金额：credits / amount 优先（都是积分单位）· amount_fen 是分 → 换算
	var credits float64
	switch {
	case r.Amount != nil:
		credits = *r.Amount
	case r.Credits != nil:
		credits = *r.Credits
	case r.AmountF != nil:
		credits = *r.AmountF / 100 // 分 → 元/积分
	}
	// 方向由 reason 定 · 取绝对值再定符号（不信 vendor 的符号约定）
	mag := credits
	if mag < 0 {
		mag = -mag
	}
	micro := int64(mag * 1_000_000)
	if reason == providers.LedgerPurchase {
		micro = -micro // 扣费为负
	}

	var balAfter int64
	switch {
	case r.BalAfter != nil:
		balAfter = int64(*r.BalAfter * 1_000_000)
	case r.Balance != nil:
		balAfter = int64(*r.Balance * 1_000_000)
	}

	orderID := r.OrderID
	if orderID == "" {
		orderID = r.ClientID
	}
	created := parseHistTime(r.Created)
	if created.IsZero() {
		created = parseHistTime(r.Time)
	}

	entryID := r.EntryID
	if entryID == "" {
		entryID = r.ID
	}
	if entryID == "" {
		// vendor 不给稳定 id · 合成指纹（同笔重拉不重复）
		entryID = ledgerFingerprint(r.Reason, orderID, r.Created, micro)
	}

	return providers.VendorLedgerEntry{
		EntryID:      entryID,
		OrderID:      orderID,
		Reason:       reason,
		RawReason:    r.Reason,
		Amount:       providers.Money{Amount: micro, Currency: providers.CurrencyCredit},
		BalanceAfter: providers.Money{Amount: balAfter, Currency: providers.CurrencyCredit},
		CreatedAt:    created,
		Raw:          r.raw,
	}
}

// normalizeReason · vendor 7 种 → 我方 6 类（docs/vendors/91kiro.md §2.8）。
func normalizeReason(raw string) string {
	switch raw {
	case "purchase":
		return providers.LedgerPurchase
	case "warranty":
		return providers.LedgerRefund
	case "clawback":
		return providers.LedgerPurchase // 冲回当初分我的收入 = 净扣
	case "recharge":
		return providers.LedgerRecharge
	case "income":
		return providers.LedgerIncome
	case "adjust", "commit":
		return providers.LedgerAdjust
	default:
		return providers.LedgerOther
	}
}

func ledgerFingerprint(reason, orderID, created string, micro int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", reason, orderID, created, micro)))
	return "fp-" + hex.EncodeToString(h[:])[:16]
}
