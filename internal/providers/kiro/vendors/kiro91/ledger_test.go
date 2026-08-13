package kiro91

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 容错解析 · 外层包装名试多个（文档没给形状 · 上线前先扛住几种可能）
func TestParseLedgerRows_WrapperVariants(t *testing.T) {
	cases := map[string]string{
		"ledger 包装":  `{"ledger":[{"id":"1","reason":"purchase","amount":50,"order_id":"o1","created_at":"2026-08-13 10:00:00"}]}`,
		"entries 包装": `{"entries":[{"id":"1","reason":"purchase","amount":50,"order_id":"o1","created_at":"2026-08-13 10:00:00"}]}`,
		"items 包装":   `{"items":[{"id":"1","reason":"purchase","amount":50,"order_id":"o1","created_at":"2026-08-13 10:00:00"}]}`,
		"裸数组":        `[{"id":"1","reason":"purchase","amount":50,"order_id":"o1","created_at":"2026-08-13 10:00:00"}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rows, err := parseLedgerRows([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("应解析出 1 条 · 得 %d", len(rows))
			}
		})
	}
}

// 无法识别的形状 · 返空不报错（别 crash backfill · 上线看 raw 再修）
func TestParseLedgerRows_UnknownShapeNoCrash(t *testing.T) {
	rows, err := parseLedgerRows([]byte(`{"weird":{"nested":1}}`))
	if err != nil {
		t.Fatalf("未知形状不该报错 · 得 %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("未知形状应返空 · 得 %d", len(rows))
	}
}

// reason 归一 + 方向：purchase 为负 · recharge/warranty 为正
func TestLedgerRowToEntry_ReasonAndSign(t *testing.T) {
	amt := 50.0
	cases := []struct {
		reason     string
		wantReason string
		wantNeg    bool
	}{
		{"purchase", providers.LedgerPurchase, true},
		{"clawback", providers.LedgerPurchase, true},
		{"recharge", providers.LedgerRecharge, false},
		{"warranty", providers.LedgerRefund, false},
		{"income", providers.LedgerIncome, false},
		{"adjust", providers.LedgerAdjust, false},
		{"commit", providers.LedgerAdjust, false},
		{"未来新增", providers.LedgerOther, false},
	}
	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			e := ledgerRowToEntry(ledgerRow{ID: "x", Reason: c.reason, Amount: &amt, OrderID: "o1", Created: "2026-08-13 10:00:00"})
			if e.Reason != c.wantReason {
				t.Errorf("reason 归一错：%s → %s · want %s", c.reason, e.Reason, c.wantReason)
			}
			if c.wantNeg && e.Amount.Amount >= 0 {
				t.Errorf("%s 应为负（扣费）· 得 %d", c.reason, e.Amount.Amount)
			}
			if !c.wantNeg && e.Amount.Amount < 0 {
				t.Errorf("%s 应为正（入账）· 得 %d", c.reason, e.Amount.Amount)
			}
		})
	}
}

// credits 换算：50 积分 → 50_000_000 microunit
func TestLedgerRowToEntry_CreditsToMicro(t *testing.T) {
	amt := 50.0
	e := ledgerRowToEntry(ledgerRow{ID: "x", Reason: "recharge", Amount: &amt, Created: "2026-08-13 10:00:00"})
	if e.Amount.Amount != 50_000_000 {
		t.Errorf("50 积分应 50_000_000 microunit · 得 %d", e.Amount.Amount)
	}
	if e.Amount.Currency != providers.CurrencyCredit {
		t.Errorf("币种应 credit · 得 %s", e.Amount.Currency)
	}
}

// amount_fen 字段：3435 分 → 34.35 积分 → 34_350_000 microunit
func TestLedgerRowToEntry_FenField(t *testing.T) {
	fen := 3435.0
	e := ledgerRowToEntry(ledgerRow{ID: "x", Reason: "recharge", AmountF: &fen, Created: "2026-08-13 10:00:00"})
	if e.Amount.Amount != 34_350_000 {
		t.Errorf("3435 分应 34_350_000 microunit · 得 %d", e.Amount.Amount)
	}
}

// 无稳定 id → 合成指纹 · 同笔两次指纹一致（幂等）
func TestLedgerRowToEntry_Fingerprint(t *testing.T) {
	amt := 50.0
	r := ledgerRow{Reason: "purchase", Amount: &amt, OrderID: "o1", Created: "2026-08-13 10:00:00"}
	e1 := ledgerRowToEntry(r)
	e2 := ledgerRowToEntry(r)
	if e1.EntryID == "" {
		t.Fatal("应合成指纹 · 得空")
	}
	if e1.EntryID != e2.EntryID {
		t.Errorf("同笔指纹应一致 · %s vs %s", e1.EntryID, e2.EntryID)
	}
}

// order_id 缺失回落 client_order_id
func TestLedgerRowToEntry_OrderIDFallback(t *testing.T) {
	amt := 50.0
	e := ledgerRowToEntry(ledgerRow{ID: "x", Reason: "purchase", Amount: &amt, ClientID: "cli-1", Created: "2026-08-13 10:00:00"})
	if e.OrderID != "cli-1" {
		t.Errorf("order_id 空应回落 client_order_id · 得 %q", e.OrderID)
	}
}
