package strategy

import (
	"context"
	"errors"
	"testing"
)

// mockDeps · 测试用 · 手动填三层数据 · 不查库。
type mockDeps struct {
	global  Strategy
	globErr error
	bus     *BusStrategy
	busErr  error
	sys     SystemDefaults
}

func (m *mockDeps) GlobalGet(ctx context.Context, pid string) (Strategy, error) {
	if m.globErr != nil {
		return Strategy{}, m.globErr
	}
	return m.global, nil
}

func (m *mockDeps) BusGet(ctx context.Context, busID string) (*BusStrategy, error) {
	if m.busErr != nil {
		return nil, m.busErr
	}
	return m.bus, nil
}

func (m *mockDeps) SystemDefaults() SystemDefaults { return m.sys }

// defaultSys · 测试用系统默认 · 跟 config.pull 默认对齐(DefaultCount=3)
func defaultSys() SystemDefaults {
	return SystemDefaults{PerRoundCount: 3, DefaultZone: ZoneAuto}
}

// ─────────────────────────────────────────────────────────────
// 场景 1 · 只有系统默认(全局无值 · 车级无值 · 无 request)
// ─────────────────────────────────────────────────────────────

func TestEffective_OnlySystemDefaults(t *testing.T) {
	deps := &mockDeps{
		global: Defaults("p1"), // 空全局 · 只有 zone=auto / per_round=1
		bus:    nil,
		sys:    defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	// PerRoundCount · Defaults 返 1 · 覆盖 sys 的 3(全局有值 · 就用它)
	if got.PerRoundCount != 1 {
		t.Errorf("PerRoundCount=%d · want 1(全局 Defaults)", got.PerRoundCount)
	}
	if got.Zone != ZoneAuto {
		t.Errorf("Zone=%q · want auto", got.Zone)
	}
	if got.MaxUnitPrice != 0 || got.DailyRoundLimit != 0 || got.DailySpendLimit != 0 {
		t.Errorf("硬上限应全 0(不限): %+v", got)
	}
	if got.PreferredVendor != "" {
		t.Errorf("PreferredVendor=%q · want 空", got.PreferredVendor)
	}
	if got.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled=true · want false")
	}
	if got.RefillWatermark != 0 {
		t.Errorf("RefillWatermark=%d · want 0", got.RefillWatermark)
	}
	if got.RefillMinCount != nil {
		t.Errorf("RefillMinCount=%v · want nil(gap 语义)", got.RefillMinCount)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 2 · 只有全局默认(车级无 · 无 request)
// ─────────────────────────────────────────────────────────────

func TestEffective_OnlyGlobal(t *testing.T) {
	max := int64(20 * 1_000_000)
	rounds := 50
	spend := int64(500 * 1_000_000)
	vendor := "kiro91"
	deps := &mockDeps{
		global: Strategy{
			PassengerID:              "p1",
			MaxUnitPrice:             &max,
			DailyRoundLimit:          &rounds,
			DailySpendLimit:          &spend,
			PerRoundCount:            5,
			PreferredVendor:          &vendor,
			DefaultZone:              ZoneUS,
			DefaultAutoRefillEnabled: true,
			DefaultRefillWatermark:   3,
		},
		bus: nil,
		sys: defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != max {
		t.Errorf("MaxUnitPrice=%d · want %d(全局)", got.MaxUnitPrice, max)
	}
	if got.DailyRoundLimit != rounds {
		t.Errorf("DailyRoundLimit=%d · want %d", got.DailyRoundLimit, rounds)
	}
	if got.DailySpendLimit != spend {
		t.Errorf("DailySpendLimit=%d · want %d", got.DailySpendLimit, spend)
	}
	if got.PerRoundCount != 5 {
		t.Errorf("PerRoundCount=%d · want 5(全局)", got.PerRoundCount)
	}
	if got.PreferredVendor != vendor {
		t.Errorf("PreferredVendor=%q · want %q", got.PreferredVendor, vendor)
	}
	if got.Zone != ZoneUS {
		t.Errorf("Zone=%q · want %q", got.Zone, ZoneUS)
	}
	// 1f-refactor(migration 040) · auto/refill 不再走全局 fallback ·
	// 无 bus 时车级零值(等价关闭)· 全局 default_* 只做建车 seed 不做运行时。
	if got.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled=true · 无 bus 应零值 false(migration 040 撤全局 fallback)")
	}
	if got.RefillWatermark != 0 {
		t.Errorf("RefillWatermark=%d · 无 bus 应零值 0", got.RefillWatermark)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 3 · 全局 + 车级覆盖(无 request)
// ─────────────────────────────────────────────────────────────

func TestEffective_GlobalPlusBusOverride(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	busMax := int64(15 * 1_000_000) // 车级更严 · 硬上限取 min = 15
	busPer := 10
	busVendor := "kirodrop"
	busAuto := false // 车级显式关(值字段 false · 非 nil = 覆盖)
	busWm := 5
	busMin := 2

	deps := &mockDeps{
		global: Strategy{
			PassengerID:              "p1",
			MaxUnitPrice:             &globalMax,
			PerRoundCount:            3,
			DefaultZone:              ZoneUS,
			DefaultAutoRefillEnabled: true, // 全局开
			DefaultRefillWatermark:   1,
		},
		bus: &BusStrategy{
			MaxUnitPrice:      &busMax,
			PerRoundCount:     &busPer,
			PreferredVendor:   &busVendor,
			AutoRefillEnabled: busAuto,
			RefillWatermark:   busWm,
			RefillMinCount:    &busMin,
		},
		sys: defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "b1", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	// 硬上限取 min
	if got.MaxUnitPrice != busMax {
		t.Errorf("MaxUnitPrice=%d · want %d(min 车级)", got.MaxUnitPrice, busMax)
	}
	// 覆盖字段 · 车级盖全局
	if got.PerRoundCount != busPer {
		t.Errorf("PerRoundCount=%d · want %d(车级)", got.PerRoundCount, busPer)
	}
	if got.PreferredVendor != busVendor {
		t.Errorf("PreferredVendor=%q · want %q(车级)", got.PreferredVendor, busVendor)
	}
	if got.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled=true · want false(车级显式关)")
	}
	if got.RefillWatermark != busWm {
		t.Errorf("RefillWatermark=%d · want %d(车级)", got.RefillWatermark, busWm)
	}
	if got.RefillMinCount == nil || *got.RefillMinCount != busMin {
		t.Errorf("RefillMinCount=%v · want %d(车级)", got.RefillMinCount, busMin)
	}
	// zone · 车级 Zone=nil · 走全局 US
	if got.Zone != ZoneUS {
		t.Errorf("Zone=%q · want %q(全局)", got.Zone, ZoneUS)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 4 · 三层都有 + request override
// ─────────────────────────────────────────────────────────────

func TestEffective_ThreeLayersPlusRequest(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	busMax := int64(15 * 1_000_000)
	busVendor := "kirodrop"
	busPer := 10
	reqCount := 2
	reqVendor := "kiro91"
	reqZone := ZoneEU
	reqMax := int64(10 * 1_000_000)

	deps := &mockDeps{
		global: Strategy{
			MaxUnitPrice:  &globalMax,
			PerRoundCount: 3,
			DefaultZone:   ZoneUS,
		},
		bus: &BusStrategy{
			MaxUnitPrice:    &busMax,
			PerRoundCount:   &busPer,
			PreferredVendor: &busVendor,
		},
		sys: defaultSys(),
	}
	req := &RequestOverride{
		Count:        &reqCount,
		Vendor:       &reqVendor,
		Zone:         &reqZone,
		MaxUnitPrice: &reqMax,
	}
	got, err := Effective(context.Background(), deps, "p1", "b1", req)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != reqMax {
		t.Errorf("MaxUnitPrice=%d · want %d(request 最严)", got.MaxUnitPrice, reqMax)
	}
	if got.PerRoundCount != reqCount {
		t.Errorf("PerRoundCount=%d · want %d(request)", got.PerRoundCount, reqCount)
	}
	if got.PreferredVendor != reqVendor {
		t.Errorf("PreferredVendor=%q · want %q(request)", got.PreferredVendor, reqVendor)
	}
	if got.Zone != reqZone {
		t.Errorf("Zone=%q · want %q(request)", got.Zone, reqZone)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 5 · 硬上限取 min(全局 20 + 车级 30 → 20)
// ─────────────────────────────────────────────────────────────

func TestEffective_HardCapMinAcrossLayers(t *testing.T) {
	globalMax := int64(20 * 1_000_000) // 全局更严
	busMax := int64(30 * 1_000_000)    // 车级放宽 · 硬上限不能放宽 · 取 min = 20

	deps := &mockDeps{
		global: Strategy{MaxUnitPrice: &globalMax},
		bus:    &BusStrategy{MaxUnitPrice: &busMax},
		sys:    defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "b1", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != globalMax {
		t.Errorf("MaxUnitPrice=%d · want %d(全局更严)", got.MaxUnitPrice, globalMax)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 6 · 硬上限 · request 收紧生效(request 10 + 全局 20 → 10)
// ─────────────────────────────────────────────────────────────

func TestEffective_HardCapRequestTightens(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	reqMax := int64(10 * 1_000_000) // 收紧

	deps := &mockDeps{
		global: Strategy{MaxUnitPrice: &globalMax},
		sys:    defaultSys(),
	}
	req := &RequestOverride{MaxUnitPrice: &reqMax}
	got, err := Effective(context.Background(), deps, "p1", "", req)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != reqMax {
		t.Errorf("MaxUnitPrice=%d · want %d(request 收紧)", got.MaxUnitPrice, reqMax)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 7 · 硬上限 · request 放宽不生效(request 30 + 全局 20 → 20)
// ─────────────────────────────────────────────────────────────

func TestEffective_HardCapRequestCannotLoosen(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	reqMax := int64(30 * 1_000_000) // 想放宽 · 无效

	deps := &mockDeps{
		global: Strategy{MaxUnitPrice: &globalMax},
		sys:    defaultSys(),
	}
	req := &RequestOverride{MaxUnitPrice: &reqMax}
	got, err := Effective(context.Background(), deps, "p1", "", req)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != globalMax {
		t.Errorf("MaxUnitPrice=%d · want %d(全局仍是硬护栏 · request 放宽无效)",
			got.MaxUnitPrice, globalMax)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 8 · 覆盖字段按优先级(request > 车级 > 全局 > 系统)
// ─────────────────────────────────────────────────────────────

func TestEffective_OverridePriority(t *testing.T) {
	// 分别测 vendor / zone / count / auto/watermark 的覆盖链
	tests := []struct {
		name  string
		build func() (*mockDeps, *RequestOverride)
		check func(*testing.T, EffectiveStrategy)
	}{
		{
			name: "vendor · request 盖车级盖全局",
			build: func() (*mockDeps, *RequestOverride) {
				gv, bv, rv := "gv", "bv", "rv"
				return &mockDeps{
					global: Strategy{PreferredVendor: &gv},
					bus:    &BusStrategy{PreferredVendor: &bv},
					sys:    defaultSys(),
				}, &RequestOverride{Vendor: &rv}
			},
			check: func(t *testing.T, e EffectiveStrategy) {
				if e.PreferredVendor != "rv" {
					t.Errorf("want rv · got %q", e.PreferredVendor)
				}
			},
		},
		{
			name: "vendor · 车级盖全局(无 request)",
			build: func() (*mockDeps, *RequestOverride) {
				gv, bv := "gv", "bv"
				return &mockDeps{
					global: Strategy{PreferredVendor: &gv},
					bus:    &BusStrategy{PreferredVendor: &bv},
					sys:    defaultSys(),
				}, nil
			},
			check: func(t *testing.T, e EffectiveStrategy) {
				if e.PreferredVendor != "bv" {
					t.Errorf("want bv · got %q", e.PreferredVendor)
				}
			},
		},
		{
			name: "count · 三层全跳到系统默认(全无)",
			build: func() (*mockDeps, *RequestOverride) {
				return &mockDeps{
					global: Strategy{PerRoundCount: 0}, // 无值(0)
					bus:    nil,
					sys:    SystemDefaults{PerRoundCount: 7, DefaultZone: ZoneAuto},
				}, nil
			},
			check: func(t *testing.T, e EffectiveStrategy) {
				if e.PerRoundCount != 7 {
					t.Errorf("want 7(系统) · got %d", e.PerRoundCount)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps, req := tc.build()
			got, err := Effective(context.Background(), deps, "p1", "b1", req)
			if err != nil {
				t.Fatalf("Effective: %v", err)
			}
			tc.check(t, got)
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 9 · 覆盖字段 · 0/false 是合法覆盖值(nullable 语义 · §4.3.2)
// ─────────────────────────────────────────────────────────────

func TestEffective_ZeroFalseIsValidOverride(t *testing.T) {
	// 全局 auto=true watermark=10 · 车级显式关 auto=false watermark=0
	// 车级 false 应盖全局 true · 不是"零值=跟随"
	busAuto := false
	busWm := 0

	deps := &mockDeps{
		global: Strategy{
			DefaultAutoRefillEnabled: true,
			DefaultRefillWatermark:   10,
		},
		bus: &BusStrategy{
			AutoRefillEnabled: busAuto,
			RefillWatermark:   busWm,
		},
		sys: defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "b1", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled=true · 车级显式 false 应生效")
	}
	if got.RefillWatermark != 0 {
		t.Errorf("RefillWatermark=%d · 车级显式 0 应生效(不是跟随全局 10)",
			got.RefillWatermark)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 10 · 自动触发无 request(req=nil · webhook/scheduler/deathwatch 场景)
// ─────────────────────────────────────────────────────────────

func TestEffective_AutoTriggerNoRequest(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	busMax := int64(15 * 1_000_000)
	busVendor := "kirodrop"

	deps := &mockDeps{
		global: Strategy{
			MaxUnitPrice:             &globalMax,
			PerRoundCount:            3,
			DefaultZone:              ZoneUS,
			DefaultAutoRefillEnabled: true,
			DefaultRefillWatermark:   5,
		},
		bus: &BusStrategy{
			MaxUnitPrice:    &busMax,
			PreferredVendor: &busVendor,
			// 1f-refactor(migration 040) · auto/refill 撤回 NOT NULL · 值字段直接就是车级值
			// 全局 default_* 只做建车 seed · 不做运行时 fallback
			AutoRefillEnabled: false, // 车级零值 · 就是 false · 全局 true 不影响
			RefillWatermark:   0,     // 同上
		},
		sys: defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "b1", nil)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != busMax {
		t.Errorf("MaxUnitPrice=%d · want %d", got.MaxUnitPrice, busMax)
	}
	if got.PreferredVendor != busVendor {
		t.Errorf("PreferredVendor=%q · want %q", got.PreferredVendor, busVendor)
	}
	// migration 040 · 车级就是车级 · 全局不 fallback
	if got.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled=true · 车级 false 直接生效(migration 040 撤 fallback)")
	}
	if got.RefillWatermark != 0 {
		t.Errorf("RefillWatermark=%d · 车级 0 直接生效", got.RefillWatermark)
	}
	if got.PerRoundCount != 3 {
		t.Errorf("PerRoundCount=%d · want 3(全局)", got.PerRoundCount)
	}
}

// ─────────────────────────────────────────────────────────────
// 场景 11 · busID 为空 · record 路径(不查车级 · §4.3.3 底部)
// ─────────────────────────────────────────────────────────────

func TestEffective_RecordPathNoBusID(t *testing.T) {
	globalMax := int64(20 * 1_000_000)
	globalVendor := "gv"
	reqCount := 5

	// 装一个 mock · 只要 BusGet 被调就 panic · 验证根本没被查
	deps := &busGetPanicDeps{
		global: Strategy{
			MaxUnitPrice:    &globalMax,
			PerRoundCount:   3,
			PreferredVendor: &globalVendor,
			DefaultZone:     ZoneUS,
		},
		sys: defaultSys(),
	}
	got, err := Effective(context.Background(), deps, "p1", "", &RequestOverride{Count: &reqCount})
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.MaxUnitPrice != globalMax {
		t.Errorf("MaxUnitPrice=%d · want %d", got.MaxUnitPrice, globalMax)
	}
	if got.PerRoundCount != reqCount {
		t.Errorf("PerRoundCount=%d · want %d(request)", got.PerRoundCount, reqCount)
	}
	if got.PreferredVendor != globalVendor {
		t.Errorf("PreferredVendor=%q · want %q(全局)", got.PreferredVendor, globalVendor)
	}
}

// busGetPanicDeps · BusGet 被调就 panic · 用来验证 busID 为空时不查车级
type busGetPanicDeps struct {
	global Strategy
	sys    SystemDefaults
}

func (b *busGetPanicDeps) GlobalGet(ctx context.Context, pid string) (Strategy, error) {
	return b.global, nil
}

func (b *busGetPanicDeps) BusGet(ctx context.Context, busID string) (*BusStrategy, error) {
	panic("busID=\"\" 时不该查车级")
}

func (b *busGetPanicDeps) SystemDefaults() SystemDefaults { return b.sys }

// ─────────────────────────────────────────────────────────────
// 场景 12 · 错误传播
// ─────────────────────────────────────────────────────────────

func TestEffective_GlobalError(t *testing.T) {
	sentinel := errors.New("db down")
	deps := &mockDeps{globErr: sentinel, sys: defaultSys()}
	_, err := Effective(context.Background(), deps, "p1", "", nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v · 应包含 sentinel", err)
	}
}

func TestEffective_BusError(t *testing.T) {
	sentinel := errors.New("bus lookup fail")
	deps := &mockDeps{
		global: Defaults("p1"),
		busErr: sentinel,
		sys:    defaultSys(),
	}
	_, err := Effective(context.Background(), deps, "p1", "b1", nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v · 应包含 sentinel", err)
	}
}

func TestEffective_MissingPassenger(t *testing.T) {
	_, err := Effective(context.Background(), &mockDeps{sys: defaultSys()}, "", "", nil)
	if err == nil {
		t.Fatalf("passengerID 空 · 应返错")
	}
}

func TestEffective_NilDeps(t *testing.T) {
	_, err := Effective(context.Background(), nil, "p1", "", nil)
	if err == nil {
		t.Fatalf("deps=nil · 应返错")
	}
}

// ─────────────────────────────────────────────────────────────
// stricter3 · 直接单测
// ─────────────────────────────────────────────────────────────

func TestStricter3(t *testing.T) {
	tests := []struct {
		a, b, c, want int64
		name          string
	}{
		{0, 0, 0, 0, "全 0 · 不限"},
		{20, 0, 0, 20, "只有一个"},
		{0, 30, 0, 30, "只有一个 · 位置无关"},
		{0, 0, 10, 10, "只有一个 · 位置无关"},
		{20, 30, 0, 20, "两个非零 · 取小"},
		{20, 30, 10, 10, "三个 · 取最小(request 收紧)"},
		{20, 30, 40, 20, "request 放宽 · 无效 · 取 min(全局, 车级)"},
		{-5, 30, 0, 30, "负数视为 0(不限) · 忽略"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stricter3(tc.a, tc.b, tc.c); got != tc.want {
				t.Errorf("stricter3(%d,%d,%d)=%d · want %d",
					tc.a, tc.b, tc.c, got, tc.want)
			}
		})
	}
}
