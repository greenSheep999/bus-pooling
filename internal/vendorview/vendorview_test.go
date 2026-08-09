package vendorview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// mockVendor 是一家简单的假 vendor，可控库存 / 单价 / 是否慢查。
type mockVendor struct {
	id        providers.VendorID
	name      string
	unitPrice int64
	available int
	// zones 覆盖默认的 us zone —— 空则用默认
	zones []providers.ZoneStock
	// slow 让 Stock() 阻塞（模拟慢家 / 超时）
	slow time.Duration
	// calls 记录被调用次数
	calls atomic.Int32
	// err Stock 返回的错误（模拟上游挂）
	err error
}

func (m *mockVendor) ID() providers.VendorID           { return m.id }
func (m *mockVendor) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (m *mockVendor) DisplayName() string              { return m.name }
func (m *mockVendor) Capability() providers.Capability {
	return providers.Capability{MinPerOrder: 1, MaxPerOrder: 200}
}

func (m *mockVendor) Stock(ctx context.Context, _ providers.StockOptions) (*providers.StockSnapshot, error) {
	m.calls.Add(1)
	if m.slow > 0 {
		select {
		case <-time.After(m.slow):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	zs := m.zones
	if zs == nil {
		zs = []providers.ZoneStock{{
			Zone: providers.ZoneUS, Region: "us-east-1", Available: m.available,
			UnitPrice: providers.Money{Amount: m.unitPrice, Currency: providers.CurrencyCredit},
		}}
	}
	total := 0
	for _, z := range zs {
		total += z.Available
	}
	return &providers.StockSnapshot{
		VendorID: m.id, ObservedAt: time.Now().UTC(),
		Available: total, MinPerOrder: 1, MaxPerOrder: 200,
		WarrantyMinutes: 10, Zones: zs,
	}, nil
}

// 其余方法不用，全部返回 ErrNotSupported。
func (m *mockVendor) Purchase(context.Context, providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) OrderKeys(context.Context, string) (*providers.PurchaseResult, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) Balance(context.Context) (*providers.Balance, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) KeyHealth(context.Context, string) (*providers.KeyHealth, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) KeyStats(context.Context, providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) Redeem(context.Context, string) (*providers.RedeemResult, error) {
	return nil, providers.ErrNotSupported
}
func (m *mockVendor) Usage(context.Context, []string) (*providers.UsageBatch, error) {
	return nil, providers.ErrNotSupported
}

// ── 工具 ────────────────────────────────────────────

// buildService 起一个装着两家 vendor 的 Service。
// 一家便宜多货、另一家贵少货 —— autoPick 应该挑便宜多货那家。
func buildService(t *testing.T, opts ...func(*mockVendor, *mockVendor)) (*Service, *mockVendor, *mockVendor) {
	t.Helper()
	reg := providers.NewRegistry()

	v91 := &mockVendor{
		id: providers.Vendor91Kiro, name: "Kiro Market",
		unitPrice: 30_000_000, available: 42,
	}
	vceo := &mockVendor{
		id: providers.VendorKiroCEO, name: "Kiro CEO",
		unitPrice: 50_000_000, available: 12,
	}
	for _, o := range opts {
		o(v91, vceo)
	}
	if err := reg.Register(v91, true); err != nil {
		t.Fatalf("register 91: %v", err)
	}
	if err := reg.Register(vceo, true); err != nil {
		t.Fatalf("register ceo: %v", err)
	}

	// 服务费 5% —— 简单看得出分项链有没生效
	svc, err := New(Config{
		Registry: reg,
		Rates:    decider.Rates{Service: 500}, // 5%
		// 短超时便于测慢家兜底
		StockTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return svc, v91, vceo
}

// ── 用例 ────────────────────────────────────────────

func TestAggregateStock_SumsAvailableAcrossVendors(t *testing.T) {
	svc, _, _ := buildService(t)
	got := svc.AggregateStock(context.Background(), Viewer{Invited: true})

	if got.TotalAvailable != 42+12 {
		t.Errorf("total = %d, want 54", got.TotalAvailable)
	}
	if len(got.ByVendor) != 2 {
		t.Fatalf("byVendor len = %d, want 2", len(got.ByVendor))
	}
	// 稳定顺序（Registry.Enabled 按 VendorID 排）
	if got.ByVendor[0].VendorID != string(providers.Vendor91Kiro) {
		t.Errorf("byVendor[0] = %q, want v01", got.ByVendor[0].VendorID)
	}
}

func TestAggregateStock_InvitedShowsRealNames(t *testing.T) {
	svc, _, _ := buildService(t)

	invited := svc.AggregateStock(context.Background(), Viewer{Invited: true})
	if invited.ByVendor[0].VendorLabel != "Kiro Market" {
		t.Errorf("invited label = %q, want 'Kiro Market'", invited.ByVendor[0].VendorLabel)
	}

	anon := svc.AggregateStock(context.Background(), Viewer{Invited: false})
	// 匿名用户不该看到 "Kiro Market" —— 应该是 "AWS-Q Kiro Vendor 01"
	if anon.ByVendor[0].VendorLabel == "Kiro Market" {
		t.Errorf("anon label 不该是真名: %q", anon.ByVendor[0].VendorLabel)
	}
	if !strings.HasPrefix(anon.ByVendor[0].VendorLabel, "AWS-Q Kiro Vendor") {
		t.Errorf("anon label = %q, want 'AWS-Q Kiro Vendor ...'", anon.ByVendor[0].VendorLabel)
	}
	// AnonID 稳定短哈希
	if len(anon.ByVendor[0].AnonID) != 6 {
		t.Errorf("anon id 长度 = %d, want 6", len(anon.ByVendor[0].AnonID))
	}
}

func TestAggregateStock_SlowVendorDoesntBlockOthers(t *testing.T) {
	svc, v91, _ := buildService(t, func(a, _ *mockVendor) {
		a.slow = 1 * time.Second // 远超 100ms 超时
	})

	start := time.Now()
	got := svc.AggregateStock(context.Background(), Viewer{Invited: true})
	elapsed := time.Since(start)

	// 单家 100ms 超时 · 并发跑 · 整体应远小于慢家的 1s
	if elapsed > 500*time.Millisecond {
		t.Errorf("聚合耗时 %v，慢家没走超时兜底", elapsed)
	}
	// 慢家 available 应为 0（超时了拿不到快照），另一家正常
	var slowRow, fastRow VendorStockRow
	for _, r := range got.ByVendor {
		if r.VendorID == string(v91.id) {
			slowRow = r
		} else {
			fastRow = r
		}
	}
	if slowRow.Available != 0 {
		t.Errorf("慢家 available = %d, want 0（超时兜底）", slowRow.Available)
	}
	if fastRow.Available <= 0 {
		t.Errorf("快家 available = %d, want > 0", fastRow.Available)
	}
}

func TestVendorStock_NotFound(t *testing.T) {
	svc, _, _ := buildService(t)
	_, err := svc.VendorStock(context.Background(), "nonexistent", Viewer{})
	if !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("err = %v, want ErrVendorNotFound", err)
	}
}

func TestVendorStock_MarkupAppliedForNonInvited(t *testing.T) {
	// 加两层分项：Region 20% + Service 5%
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "Kiro Market",
		unitPrice: 100_000_000, available: 10,
	}
	_ = reg.Register(v, true)
	svc, err := New(Config{
		Registry: reg,
		Rates:    decider.Rates{RegionMarkup: 2000, Service: 500}, // 20% + 5%
	})
	if err != nil {
		t.Fatal(err)
	}

	// 未邀请 → 全链生效 · 100 → 120 → 126
	nonInvited, err := svc.VendorStock(context.Background(),
		string(providers.Vendor91Kiro), Viewer{Invited: false})
	if err != nil {
		t.Fatal(err)
	}
	if p := nonInvited.Zones[0].UnitPrice; p != 126_000_000 {
		t.Errorf("非邀请单价 = %d, want 126_000_000 (区 20 + 服务 5)", p)
	}

	// 邀请 → 跳过区域分项 · 只保留服务费 · 100 → 105
	invited, err := svc.VendorStock(context.Background(),
		string(providers.Vendor91Kiro), Viewer{Invited: true})
	if err != nil {
		t.Fatal(err)
	}
	if p := invited.Zones[0].UnitPrice; p != 105_000_000 {
		t.Errorf("邀请用户单价 = %d, want 105_000_000 (跳过区域 · 只服务 5)", p)
	}
}

func TestVendorStock_NoInternalTermsInJSON(t *testing.T) {
	svc, _, _ := buildService(t)
	out, err := svc.VendorStock(context.Background(),
		string(providers.Vendor91Kiro), Viewer{Invited: false})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	body := strings.ToLower(string(raw))
	// CLAUDE.md §0.1 禁词
	banned := []string{"housepool", "kiro.rs", "kiro_rs", "record group", "record-",
		"current_group", "death_source", "provider_id", "decider", "adapter",
		"key_cost", "vendor_fee", "region_fee", "single_pull_fee", "capability_fee"}
	for _, b := range banned {
		if strings.Contains(body, b) {
			t.Errorf("响应含禁词 %q: %s", b, raw)
		}
	}
}

func TestAutoPick_PicksCheapestWhenNoHistory(t *testing.T) {
	svc, _, _ := buildService(t) // 一家便宜（30）· 另一家贵（50）
	pick := svc.AutoPick(context.Background(), "auto", Viewer{Invited: true})
	if pick.VendorID != string(providers.Vendor91Kiro) {
		t.Errorf("autoPick = %q, want v01", pick.VendorID)
	}
	if pick.Reason == "" {
		t.Error("autoPick 缺 reason")
	}
	// 前端要人话理由，不要 decider 字样
	if strings.Contains(pick.Reason, "decider") {
		t.Errorf("reason 含内部术语 decider: %q", pick.Reason)
	}
}

func TestAutoPick_OutOfStock_ReturnsEmptyShell(t *testing.T) {
	svc, v91, vceo := buildService(t, func(a, b *mockVendor) {
		a.available = 0
		a.zones = []providers.ZoneStock{{
			Zone: providers.ZoneUS, Available: 0,
			UnitPrice: providers.Money{Amount: a.unitPrice},
		}}
		b.available = 0
		b.zones = []providers.ZoneStock{{
			Zone: providers.ZoneUS, Available: 0,
			UnitPrice: providers.Money{Amount: b.unitPrice},
		}}
	})
	_ = v91
	_ = vceo

	pick := svc.AutoPick(context.Background(), "auto", Viewer{Invited: true})
	if pick.Available != 0 {
		t.Errorf("缺货时 available = %d, want 0", pick.Available)
	}
	if !strings.Contains(pick.Reason, "缺货") {
		t.Errorf("缺货 reason = %q，应含 '缺货'", pick.Reason)
	}
}

func TestAutoPick_ZoneFilter(t *testing.T) {
	// v91 只有 eu 有货；vceo 只有 us 有货 · 传 zone=us 应该挑 vceo
	svc, _, _ := buildService(t, func(a, b *mockVendor) {
		a.zones = []providers.ZoneStock{
			{Zone: providers.ZoneUS, Available: 0, UnitPrice: providers.Money{Amount: a.unitPrice}},
			{Zone: providers.ZoneEU, Available: 5, UnitPrice: providers.Money{Amount: a.unitPrice}},
		}
		b.zones = []providers.ZoneStock{
			{Zone: providers.ZoneUS, Available: 8, UnitPrice: providers.Money{Amount: b.unitPrice}},
		}
	})

	pick := svc.AutoPick(context.Background(), "us", Viewer{Invited: true})
	if pick.VendorID != string(providers.VendorKiroCEO) {
		t.Errorf("zone=us pick = %q, want v02（v01 在 us 缺货）", pick.VendorID)
	}
}

func TestPrices_ReturnsPlaceholderDaysWithNotice(t *testing.T) {
	svc, _, _ := buildService(t)
	out := svc.Prices(context.Background(), "auto", 7, Viewer{Invited: false})
	if out.Notice == "" {
		t.Error("1a 阶段 prices 应带 notice 提示数据未采集")
	}
	if len(out.Trends) != 2 {
		t.Fatalf("trends 数 = %d, want 2", len(out.Trends))
	}
	if len(out.Trends[0].Days) != 7 {
		t.Errorf("days 数 = %d, want 7", len(out.Trends[0].Days))
	}
	// 每天 rounds 空 · 前端已适配空态
	if len(out.Trends[0].Days[0].Rounds) != 0 {
		t.Errorf("1a 阶段 rounds 应为空数组")
	}
}

func TestHistory_ReturnsPlaceholderForKnownVendor(t *testing.T) {
	svc, _, _ := buildService(t)
	out, err := svc.History(context.Background(), string(providers.Vendor91Kiro))
	if err != nil {
		t.Fatal(err)
	}
	if out.Notice == "" {
		t.Error("1a 阶段 history 应带 notice")
	}
	if out.AvgLifespanSeconds != 0 || out.AliveRate30d != 0 || out.TotalPulled30d != 0 {
		t.Error("1a 阶段 history 数值字段应为 0")
	}
}

func TestHistory_NotFound(t *testing.T) {
	svc, _, _ := buildService(t)
	_, err := svc.History(context.Background(), "nonexistent")
	if !errors.Is(err, ErrVendorNotFound) {
		t.Errorf("err = %v, want ErrVendorNotFound", err)
	}
}

func TestStats_ListsAllEnabledVendors(t *testing.T) {
	svc, _, _ := buildService(t)
	out := svc.Stats(context.Background(), Viewer{Invited: true})
	if len(out.Stats) != 2 {
		t.Fatalf("stats 数 = %d, want 2", len(out.Stats))
	}
	if len(out.Share) != 2 {
		t.Fatalf("share 数 = %d, want 2", len(out.Share))
	}
	// rank 从 1 起递增
	if out.Stats[0].Rank != 1 || out.Stats[1].Rank != 2 {
		t.Errorf("rank = %d,%d", out.Stats[0].Rank, out.Stats[1].Rank)
	}
}

func TestAnonIDStable(t *testing.T) {
	// AnonID 是内容哈希 · 同 vendor_id 每次调用应得到同一个短串
	a := anonIDOf(providers.Vendor91Kiro)
	b := anonIDOf(providers.Vendor91Kiro)
	if a != b {
		t.Errorf("anonID 不稳定: %q vs %q", a, b)
	}
	if len(a) != 6 {
		t.Errorf("anonID 长度 = %d, want 6", len(a))
	}
}
