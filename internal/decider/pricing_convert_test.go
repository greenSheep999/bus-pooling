package decider

import (
	"context"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// mockPricing · 测试用·可控每个 vendor 的换算规则
type mockPricing struct {
	quotes map[providers.VendorID]VendorQuote
}

func (m *mockPricing) QuoteFor(_ context.Context, id providers.VendorID) VendorQuote {
	if q, ok := m.quotes[id]; ok {
		return q
	}
	// 默认 fallback CNY 1:1
	return VendorQuote{QuoteCurrency: "CNY", CreditsPerUnit: 1_000_000}
}

// mockCredits · 测试用 · 可控 vendor_probe 里有没有积分数据
type mockCredits struct {
	byVendor map[string]int64
}

func (m *mockCredits) LatestCredits(_ context.Context, vendorID string, _ string) (int64, time.Time, bool) {
	if c, ok := m.byVendor[vendorID]; ok && c > 0 {
		return c, time.Now(), true
	}
	return 0, time.Time{}, false
}

// ── 估价基准优先读库（docs/18 §1.4）─────────────────────

// 库里有 our_unit_credits · 就用它 · **不看本轮快照单价**
func TestUnitCreditsFor_PrefersDB(t *testing.T) {
	o := &Orchestrator{
		pricing: &mockPricing{},
		credits: &mockCredits{byVendor: map[string]int64{
			string(providers.VendorKiroDrop): 49_980_000, // 探针入库时换算好的
		}},
	}
	// 快照给个完全不同的数 · 应该被忽略
	got, fromDB := o.unitCreditsFor(context.Background(), providers.VendorKiroDrop, providers.ZoneUS,
		providers.Money{Amount: 999_000_000, Currency: providers.CurrencyUSD})
	if !fromDB {
		t.Error("库里有数据时应标 fromDB=true")
	}
	if got != 49_980_000 {
		t.Errorf("应读库里的值 · got=%d want=49_980_000", got)
	}
}

// 库里没数据（冷启动 / 新接入）· 退回按本轮快照现算 · **不能硬失败**
func TestUnitCreditsFor_FallsBackToSnapshot(t *testing.T) {
	o := &Orchestrator{
		pricing: &mockPricing{quotes: map[providers.VendorID]VendorQuote{
			providers.VendorKiroDrop: {QuoteCurrency: "USD", CreditsPerUnit: 6_800_000},
		}},
		credits: &mockCredits{byVendor: map[string]int64{}}, // 空库
	}
	got, fromDB := o.unitCreditsFor(context.Background(), providers.VendorKiroDrop, providers.ZoneUS,
		providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD})
	if fromDB {
		t.Error("空库时应标 fromDB=false")
	}
	// 7.35 × 6.8 = 49.98 积分
	if got != 49_980_000 {
		t.Errorf("快照现算 · got=%d want=49_980_000", got)
	}
}

// credits = nil（老装配 / 测试）· 也走快照兜底
func TestUnitCreditsFor_NilLookupFallsBack(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{}, credits: nil}
	got, fromDB := o.unitCreditsFor(context.Background(), providers.Vendor91Kiro, providers.ZoneUS,
		providers.Money{Amount: 30_000_000, Currency: providers.CurrencyCredit})
	if fromDB {
		t.Error("nil lookup 应标 fromDB=false")
	}
	if got != 30_000_000 {
		t.Errorf("credit 家 1:1 · got=%d want=30_000_000", got)
	}
}

// ── 快照兜底的换算式 · 跟 Prober 落库时同一条（docs/18 §1.3）──

// USD 家 · 7.35 USD × 6.8 = 49.98 积分
func TestConvertSnapshotPrice_USDAppliesRate(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{
		quotes: map[providers.VendorID]VendorQuote{
			providers.VendorKiroDrop: {QuoteCurrency: "USD", CreditsPerUnit: 6_800_000},
		},
	}}
	got := o.convertSnapshotPrice(context.Background(), providers.VendorKiroDrop,
		providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD})
	if got != 49_980_000 {
		t.Errorf("7.35 USD × 6.8 → %d · want 49_980_000", got)
	}
}

// credit / CNY 家 · credits_per_unit = 1_000_000 · 换算式退化成恒等
func TestConvertSnapshotPrice_CreditPassthrough(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{}}
	for _, cur := range []string{providers.CurrencyCNY, providers.CurrencyCredit} {
		got := o.convertSnapshotPrice(context.Background(), providers.Vendor91Kiro,
			providers.Money{Amount: 30_000_000, Currency: cur})
		if got != 30_000_000 {
			t.Errorf("%s 家应 pass-through · got=%d want=30_000_000", cur, got)
		}
	}
}

// 0 单价（vendor 缺货）· 返 0 · 不做换算
func TestConvertSnapshotPrice_ZeroStaysZero(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{}}
	if got := o.convertSnapshotPrice(context.Background(), providers.Vendor91Kiro,
		providers.Money{Amount: 0, Currency: providers.CurrencyCredit}); got != 0 {
		t.Errorf("0 单价应返 0 · got=%d", got)
	}
}

// ── 币种校验 ────────────────────────────────────────
// vendorMaxTotal 本体的用例在 maxtotal_test.go（那份是这个换算的 owner）

// 币种对不上 → 调用方不设上限（配错的表会把上限放大几倍 · 涨价保护形同虚设）
func TestQuoteCurrencyMatches(t *testing.T) {
	usd := providers.Money{Currency: providers.CurrencyUSD}
	cred := providers.Money{Currency: providers.CurrencyCredit}
	cny := providers.Money{Currency: providers.CurrencyCNY}

	if !quoteCurrencyMatches("USD", usd) {
		t.Error("USD/USD 应匹配")
	}
	if quoteCurrencyMatches("CNY", usd) {
		t.Error("CNY 表配 USD 快照 · 应判不匹配")
	}
	// credit 跟 CNY 同族（我方积分口径 1:1）
	if !quoteCurrencyMatches("CNY", cred) {
		t.Error("CNY/credit 应视为同族")
	}
	if !quoteCurrencyMatches("credit", cny) {
		t.Error("credit/CNY 应视为同族")
	}
	// 任一侧缺币种 · 无从校验 · 放行
	if !quoteCurrencyMatches("", usd) {
		t.Error("表侧缺币种应放行")
	}
	if !quoteCurrencyMatches("USD", providers.Money{}) {
		t.Error("快照侧缺币种应放行")
	}
}
