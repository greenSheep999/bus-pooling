package pricing

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func testSurchargeStore(t *testing.T) *SurchargeStore {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/sr.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewSurchargeStore(d.DB)
}

func TestEngine_UnconditionalRulesApply(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindService, Name: "service_fee", RateBp: 1000, Active: true, Priority: 100},
		{ID: "r2", Kind: KindRetail, Name: "retail", RateBp: 2000, Active: true, Priority: 90},
	}
	e := NewEngine(rules)
	got := e.Eval(EvalContext{})
	if got.Service != 1000 {
		t.Errorf("Service = %d · want 1000", got.Service)
	}
	if got.Retail != 2000 {
		t.Errorf("Retail = %d · want 2000", got.Retail)
	}
}

func TestEngine_InactiveRulesSkipped(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindService, RateBp: 1000, Active: false},
	}
	got := NewEngine(rules).Eval(EvalContext{})
	if got.Service != 0 {
		t.Errorf("inactive 规则不该应用 · Service=%d", got.Service)
	}
}

// applies_when · vendor_id 匹配
func TestEngine_AppliesWhenVendorID(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindVendor, RateBp: 500, Active: true,
			AppliesWhen: Predicate{"vendor_id": "vtest"}},
	}
	e := NewEngine(rules)
	// 命中
	got := e.Eval(EvalContext{VendorID: "vtest"})
	if got.Vendor != 500 {
		t.Errorf("命中 vendor_id=vtest · Vendor=%d · want 500", got.Vendor)
	}
	// 不命中
	got = e.Eval(EvalContext{VendorID: "vother"})
	if got.Vendor != 0 {
		t.Errorf("vendor_id 不同 Vendor=%d · want 0", got.Vendor)
	}
}

// waived_when 优先级高于 applies_when · invited 用户减免零售分项
func TestEngine_WaivedWhenTrumpsApplies(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindRetail, RateBp: 3000, Active: true,
			AppliesWhen: Predicate{}, // 无条件
			WaivedWhen:  Predicate{"passenger.invited": true}},
	}
	e := NewEngine(rules)
	got := e.Eval(EvalContext{PassengerInvited: false})
	if got.Retail != 3000 {
		t.Errorf("非 invited 应加零售 · Retail=%d · want 3000", got.Retail)
	}
	got = e.Eval(EvalContext{PassengerInvited: true})
	if got.Retail != 0 {
		t.Errorf("invited 应减免 · Retail=%d · want 0", got.Retail)
	}
}

// count == 1 时 single_pull 生效
func TestEngine_SinglePullOnCountEq1(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindSinglePull, RateBp: 2000, Active: true,
			AppliesWhen: Predicate{"count": float64(1)}},
	}
	e := NewEngine(rules)
	if got := e.Eval(EvalContext{Count: 1}); got.SinglePull != 2000 {
		t.Errorf("count=1 SinglePull=%d · want 2000", got.SinglePull)
	}
	if got := e.Eval(EvalContext{Count: 2}); got.SinglePull != 0 {
		t.Errorf("count=2 SinglePull=%d · want 0", got.SinglePull)
	}
}

// > 数值比较
func TestEngine_NumericGreaterThan(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindAdhoc, RateBp: 1500, Active: true,
			AppliesWhen: Predicate{"bus.avg_lifespan_h": map[string]any{">": float64(48)}}},
	}
	e := NewEngine(rules)
	if got := e.Eval(EvalContext{BusAvgLifespanH: 72}); got.Adhoc != 1500 {
		t.Errorf("72h > 48h · Adhoc=%d · want 1500", got.Adhoc)
	}
	if got := e.Eval(EvalContext{BusAvgLifespanH: 24}); got.Adhoc != 0 {
		t.Errorf("24h < 48h · Adhoc=%d · want 0", got.Adhoc)
	}
}

// 多规则同 kind 累加
func TestEngine_SameKindStacks(t *testing.T) {
	rules := []Rule{
		{ID: "r1", Kind: KindService, RateBp: 500, Active: true, Priority: 10},
		{ID: "r2", Kind: KindService, RateBp: 300, Active: true, Priority: 20},
	}
	e := NewEngine(rules)
	got := e.Eval(EvalContext{})
	if got.Service != 800 {
		t.Errorf("同 kind 应累加 · Service=%d · want 800", got.Service)
	}
	if len(got.Hits) != 2 {
		t.Errorf("Hits=%d · want 2", len(got.Hits))
	}
}

// Predicate JSON 序列化 · Upsert 后 ListActive 能拿回来
func TestSurchargeStore_UpsertRoundTrip(t *testing.T) {
	s := testSurchargeStore(t)
	orig := Rule{
		ID: "r1", Kind: KindRetail, Name: "retail_markup",
		RateBp: 3000, Base: "key_cost", Active: true,
		AppliesWhen: Predicate{"vendor_id": "vtest"},
		WaivedWhen:  Predicate{"passenger.invited": true},
		Priority:    100,
	}
	if err := s.Upsert(context.Background(), orig); err != nil {
		t.Fatal(err)
	}
	rules, err := s.ListActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("ListActive 返 %d · want 1", len(rules))
	}
	r := rules[0]
	if r.Kind != KindRetail || r.RateBp != 3000 {
		t.Errorf("kind/rate 不对 · %+v", r)
	}
	if r.AppliesWhen["vendor_id"] != "vtest" {
		t.Errorf("applies_when vendor_id = %v · want vtest", r.AppliesWhen["vendor_id"])
	}
	if r.WaivedWhen["passenger.invited"] != true {
		t.Errorf("waived_when invited = %v · want true", r.WaivedWhen["passenger.invited"])
	}
}

// active=0 不出现在 ListActive
func TestSurchargeStore_ListActiveExcludesInactive(t *testing.T) {
	s := testSurchargeStore(t)
	if err := s.Upsert(context.Background(), Rule{
		ID: "on", Kind: KindService, Name: "on", RateBp: 100, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), Rule{
		ID: "off", Kind: KindService, Name: "off", RateBp: 200, Active: false,
	}); err != nil {
		t.Fatal(err)
	}
	rules, _ := s.ListActive(context.Background())
	if len(rules) != 1 || rules[0].Name != "on" {
		t.Errorf("ListActive should only return active · got=%+v", rules)
	}
}
