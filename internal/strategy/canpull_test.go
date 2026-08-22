package strategy

import (
	"context"
	"errors"
	"testing"
)

// decide 是纯函数，直接测各种上限组合，不用搭库。
func TestDecideAllowsWhenNoLimits(t *testing.T) {
	st := Defaults("p1")
	got, err := decide(st, "p1", CheckInput{Count: 3, UnitPriceHint: 10 * micro, Balance: 100 * micro})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if got.WantCount != 3 {
		t.Errorf("WantCount = %d", got.WantCount)
	}
	if got.Zone != ZoneAuto {
		t.Errorf("Zone = %q", got.Zone)
	}
	if got.MaxUnitPrice != nil {
		t.Errorf("没设上限时 Intent.MaxUnitPrice 应为 nil，得到 %v", *got.MaxUnitPrice)
	}
}

// 1 轮 = 1 次拉号动作，**不管这轮拉几个号**（CLAUDE.md §2 术语）。
// 写成"按号数算轮"会让 count=5 一次吃掉 5 轮额度。
func TestDailyRoundLimitCountsActionsNotKeys(t *testing.T) {
	st := Defaults("p1")
	st.DailyRoundLimit = ip(3)

	// 已用 2 轮，这是第 3 轮 —— 即使一次拉 50 个号也只算 1 轮
	got, err := decide(st, "p1", CheckInput{Count: 50, Used: Usage{Rounds: 2}})
	if err != nil {
		t.Fatalf("第 3 轮应放行: %v", err)
	}
	if got.WantCount != 50 {
		t.Errorf("WantCount = %d", got.WantCount)
	}

	// 已用 3 轮 = 拉满
	_, err = decide(st, "p1", CheckInput{Count: 1, Used: Usage{Rounds: 3}})
	var le *LimitError
	if !errors.As(err, &le) {
		t.Fatalf("应返回 LimitError，得到 %v", err)
	}
	if le.Kind != LimitDailyRound {
		t.Errorf("Kind = %q，want %q", le.Kind, LimitDailyRound)
	}
	// 契约要求带 limit / used，前端要画"超了多少"
	if le.Limit != 3 || le.Used != 3 {
		t.Errorf("Limit/Used = %d/%d，want 3/3", le.Limit, le.Used)
	}
	if !errors.Is(err, ErrLimitReached) {
		t.Error("应能用 errors.Is(ErrLimitReached) 粗判")
	}
}

func TestUnitPriceCapBlocks(t *testing.T) {
	st := Defaults("p1")
	st.MaxUnitPrice = i64(5 * micro)

	// 等于上限 = 放行（上限是"不超过"，不是"小于"）
	if _, err := decide(st, "p1", CheckInput{Count: 1, UnitPriceHint: 5 * micro, Balance: 999 * micro}); err != nil {
		t.Errorf("等于上限应放行: %v", err)
	}

	_, err := decide(st, "p1", CheckInput{Count: 1, UnitPriceHint: 26 * micro, Balance: 999 * micro})
	var le *LimitError
	if !errors.As(err, &le) {
		t.Fatalf("应返回 LimitError，得到 %v", err)
	}
	if le.Kind != LimitUnitPrice {
		t.Errorf("Kind = %q", le.Kind)
	}
	// 契约：price_over_cap 带 cap / current
	if le.Cap != 5*micro || le.Current != 26*micro {
		t.Errorf("Cap/Current = %d/%d", le.Cap, le.Current)
	}
}

// 比价前还不知道价（hint=0）不该被单价上限拦 —— 否则第一步就走不下去。
func TestUnitPriceCapSkippedWhenPriceUnknown(t *testing.T) {
	st := Defaults("p1")
	st.MaxUnitPrice = i64(1) // 极严
	if _, err := decide(st, "p1", CheckInput{Count: 1, UnitPriceHint: 0}); err != nil {
		t.Errorf("hint=0（比价前）不该被单价上限拦: %v", err)
	}
}

// 全局跟车级取**更严**的（AND · decisions §8.27）。
func TestUnitPriceCapIsANDOfGlobalAndBus(t *testing.T) {
	cases := []struct {
		name    string
		global  *int64
		bus     *int64
		wantCap *int64
		hint    int64
		blocked bool
	}{
		{"只有全局", i64(10 * micro), nil, i64(10 * micro), 11 * micro, true},
		{"只有车级", nil, i64(8 * micro), i64(8 * micro), 9 * micro, true},
		{"车级更严", i64(10 * micro), i64(6 * micro), i64(6 * micro), 7 * micro, true},
		{"全局更严", i64(4 * micro), i64(9 * micro), i64(4 * micro), 5 * micro, true},
		{"都没设", nil, nil, nil, 999 * micro, false},
		{"取严后仍在线内", i64(10 * micro), i64(6 * micro), i64(6 * micro), 6 * micro, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := Defaults("p1")
			st.MaxUnitPrice = tc.global
			// BusID 非空 —— 车级上限只在进车时参与 AND
			// （提取只受全局管，见 TestExtractIsGovernedByGlobalLimitsOnly）
			got, err := decide(st, "p1", CheckInput{
				BusID: "bus-1", Count: 1, UnitPriceHint: tc.hint, Balance: 1 << 62,
				BusMaxUnitPrice: tc.bus,
			})
			if tc.blocked {
				var le *LimitError
				if !errors.As(err, &le) || le.Kind != LimitUnitPrice {
					t.Fatalf("应被单价上限拦，得到 %v", err)
				}
				if tc.wantCap != nil && le.Cap != *tc.wantCap {
					t.Errorf("生效上限 = %d，want %d", le.Cap, *tc.wantCap)
				}
				return
			}
			if err != nil {
				t.Fatalf("应放行: %v", err)
			}
			switch {
			case tc.wantCap == nil && got.MaxUnitPrice != nil:
				t.Errorf("Intent.MaxUnitPrice = %d，want nil", *got.MaxUnitPrice)
			case tc.wantCap != nil && (got.MaxUnitPrice == nil || *got.MaxUnitPrice != *tc.wantCap):
				t.Errorf("Intent.MaxUnitPrice = %v，want %d", got.MaxUnitPrice, *tc.wantCap)
			}
		})
	}
}

func TestDailySpendLimitUsesEstimatedTotal(t *testing.T) {
	st := Defaults("p1")
	st.DailySpendLimit = i64(100 * micro)

	// 已花 90，这轮预估 3×5=15 → 105 超了
	_, err := decide(st, "p1", CheckInput{
		Count: 3, UnitPriceHint: 5 * micro, Balance: 1 << 62,
		Used: Usage{Spend: 90 * micro},
	})
	var le *LimitError
	if !errors.As(err, &le) {
		t.Fatalf("应返回 LimitError，得到 %v", err)
	}
	if le.Kind != LimitDailySpend {
		t.Errorf("Kind = %q", le.Kind)
	}
	if le.Limit != 100*micro || le.Used != 90*micro {
		t.Errorf("Limit/Used = %d/%d", le.Limit, le.Used)
	}

	// 刚好等于上限 = 放行
	if _, err := decide(st, "p1", CheckInput{
		Count: 2, UnitPriceHint: 5 * micro, Balance: 1 << 62,
		Used: Usage{Spend: 90 * micro},
	}); err != nil {
		t.Errorf("刚好到上限应放行: %v", err)
	}
}

func TestInsufficientBalance(t *testing.T) {
	st := Defaults("p1")
	_, err := decide(st, "p1", CheckInput{Count: 3, UnitPriceHint: 10 * micro, Balance: 5 * micro})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("应返回 ErrInsufficientBalance，得到 %v", err)
	}
}

// 同时余额不足 + 超轮数时，报"拉满了"比报"钱不够"有用 ——
// 后者会让乘客去充钱，充完还是拉不动。
func TestLimitReportedBeforeBalance(t *testing.T) {
	st := Defaults("p1")
	st.DailyRoundLimit = ip(1)

	_, err := decide(st, "p1", CheckInput{
		Count: 1, UnitPriceHint: 10 * micro,
		Balance: 0, // 钱也不够
		Used:    Usage{Rounds: 1},
	})
	if !errors.Is(err, ErrLimitReached) {
		t.Fatalf("应优先报上限而不是余额，得到 %v", err)
	}
	if errors.Is(err, ErrInsufficientBalance) {
		t.Error("不该报余额不足")
	}
}

func TestBadCount(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()
	for _, n := range []int{0, -1} {
		if _, err := s.CanPull(ctx, pid, CheckInput{Count: n}); !errors.Is(err, ErrBadCount) {
			t.Errorf("count=%d 应返回 ErrBadCount，得到 %v", n, err)
		}
	}
}

// 提取 key（BusID 空）**只受全局限额管** —— record group 没有车级限额
// （decisions §8.27）。这条容易写错成"提取不受任何限额管"。
func TestExtractIsGovernedByGlobalLimitsOnly(t *testing.T) {
	st := Defaults("p1")
	st.DailyRoundLimit = ip(2)

	// BusID 空 = 提取。全局轮数上限照样拦
	_, err := decide(st, "p1", CheckInput{BusID: "", Count: 1, Used: Usage{Rounds: 2}})
	if !errors.Is(err, ErrLimitReached) {
		t.Fatalf("提取也要受全局轮数上限管，得到 %v", err)
	}

	// 车级上限对提取**无效** —— 即使调用方误传了也必须被忽略。
	// 主动忽略而不是信任调用方：多传一个字段就让提取被本不该管它的上限拦住，
	// 现象看起来像"上限算错了"，极难查。
	st2 := Defaults("p1")
	got, err := decide(st2, "p1", CheckInput{
		BusID: "", Count: 1, UnitPriceHint: 100 * micro, Balance: 1 << 62,
		BusMaxUnitPrice: i64(1), // 极严的车级上限
	})
	if err != nil {
		t.Fatalf("提取不该被车级上限拦: %v", err)
	}
	if got.MaxUnitPrice != nil {
		t.Errorf("提取的 Intent.MaxUnitPrice = %d，车级上限应被忽略", *got.MaxUnitPrice)
	}

	// 对照：同样的车级上限，进车（BusID 非空）时必须生效
	if _, err := decide(st2, "p1", CheckInput{
		BusID: "bus-1", Count: 1, UnitPriceHint: 100 * micro, Balance: 1 << 62,
		BusMaxUnitPrice: i64(1),
	}); !errors.Is(err, ErrLimitReached) {
		t.Errorf("进车时车级上限必须生效，得到 %v", err)
	}
}

// CanPull 走完整路径（读库 + 判定），确认存进去的上限真的生效。
func TestCanPullReadsLimitsFromDB(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	// 先确认不限时能过
	if _, err := s.CanPull(ctx, pid, CheckInput{Count: 1, UnitPriceHint: 50 * micro, Balance: 1 << 62}); err != nil {
		t.Fatalf("没设上限应放行: %v", err)
	}

	// 存一个严上限
	if _, err := s.Put(ctx, pid, Patch{MaxUnitPrice: ptr(i64(5 * micro))}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	_, err := s.CanPull(ctx, pid, CheckInput{Count: 1, UnitPriceHint: 50 * micro, Balance: 1 << 62})
	var le *LimitError
	if !errors.As(err, &le) || le.Kind != LimitUnitPrice {
		t.Fatalf("存进去的上限应真生效（不是存着等 1d），得到 %v", err)
	}
}

// Intent 要带上乘客的偏好 vendor 和 zone，交给 decider 用。
func TestIntentCarriesPreferences(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, pid, Patch{
		PreferredVendor: ptr(sp("kiro91")),
		DefaultZone:     sp(ZoneEU),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.CanPull(ctx, pid, CheckInput{BusID: "bus-1", Count: 2, Balance: 1 << 62})
	if err != nil {
		t.Fatalf("CanPull: %v", err)
	}
	if got.Vendor == nil || *got.Vendor != "kiro91" {
		t.Errorf("Vendor = %v", got.Vendor)
	}
	if got.Zone != ZoneEU {
		t.Errorf("Zone = %q", got.Zone)
	}
	if got.BusID != "bus-1" {
		t.Errorf("BusID = %q", got.BusID)
	}
}

func TestStricter(t *testing.T) {
	cases := []struct {
		a, b, want *int64
	}{
		{nil, nil, nil},
		{i64(5), nil, i64(5)},
		{nil, i64(7), i64(7)},
		{i64(5), i64(7), i64(5)},
		{i64(9), i64(7), i64(7)},
		// 0 是合法上限，不该被当成"没设"
		{i64(0), i64(7), i64(0)},
		{nil, i64(0), i64(0)},
	}
	for _, tc := range cases {
		got := stricter(tc.a, tc.b)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("stricter(%v,%v) = %d，want nil", tc.a, tc.b, *got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("stricter(%v,%v) = %v，want %d", tc.a, tc.b, got, *tc.want)
		}
	}
}

// §8.47 · 车级每日轮数上限 · 两层 AND 独立生效
//
// 语义:
//   - 全局管"所有车加起来" · 用 in.Used 判(跨车累加)
//   - 车级管"这辆车" · 用 in.UsedBus 判(本车累加)
//   - 两层任一层拦都拦
func TestBusDailyRoundLimit_ANDWithGlobal(t *testing.T) {
	st := Defaults("p1")
	st.DailyRoundLimit = ip(10) // 全局 10 轮 · 跨所有车

	// 场景 A · 全局远没到 · 车级 3 轮 · 本车已用 3 → 拦
	busLimit := 3
	_, err := decide(st, "p1", CheckInput{
		BusID:         "b1",
		Count:         1,
		BusDailyRound: &busLimit,
		Used:          Usage{Rounds: 5}, // 跨车 5(<10 全局 OK)
		UsedBus:       &Usage{Rounds: 3}, // 本车已 3 · 再拉 1 = 4 > 3 · 车级拦
	})
	if err == nil {
		t.Fatal("车级 3 轮已满 · 应拦")
	}
	var le *LimitError
	if !errors.As(err, &le) || le.Kind != LimitDailyRound {
		t.Errorf("应报车级 daily_round · 得 %v", err)
	}
	if le.Limit != 3 {
		t.Errorf("Limit 应为车级 3 · 得 %d", le.Limit)
	}

	// 场景 B · 车级没设 · 只受全局管 · 全局够(5<10)· 通过
	if _, err := decide(st, "p1", CheckInput{
		BusID:   "b1",
		Count:   1,
		Used:    Usage{Rounds: 5},
		UsedBus: &Usage{Rounds: 5}, // 车级 nil · 这个字段被忽略
	}); err != nil {
		t.Errorf("车级未设应过全局判 · 得 %v", err)
	}

	// 场景 C · 提取(BusID 空)· 车级参数被主动忽略
	if _, err := decide(st, "p1", CheckInput{
		BusID:         "",
		Count:         1,
		BusDailyRound: &busLimit,     // 传了也不生效
		UsedBus:       &Usage{Rounds: 100},
		Used:          Usage{Rounds: 5},
	}); err != nil {
		t.Errorf("提取应只受全局管 · 得 %v", err)
	}
}

// §8.47 · 车级每日花费上限 · 两层 AND 独立生效
func TestBusDailySpendLimit_ANDWithGlobal(t *testing.T) {
	st := Defaults("p1")
	st.DailySpendLimit = i64(1000 * micro) // 全局 1000 · 跨所有车

	// 场景 · 本车已用 900 · 车级 950 · 再拉 1 号单价 100 → 车级拦(900+100=1000 > 950)
	busSpend := int64(950 * micro)
	_, err := decide(st, "p1", CheckInput{
		BusID:         "b1",
		Count:         1,
		UnitPriceHint: 100 * micro,
		Balance:       10000 * micro,
		BusDailySpend: &busSpend,
		Used:          Usage{Spend: 500 * micro}, // 跨车 500(远 < 1000 全局)
		UsedBus:       &Usage{Spend: 900 * micro}, // 本车 900 · 再花 100 = 1000 > 950
	})
	if err == nil {
		t.Fatal("车级 spend 已快满 · 应拦")
	}
	var le *LimitError
	if !errors.As(err, &le) || le.Kind != LimitDailySpend {
		t.Errorf("应报车级 daily_spend · 得 %v", err)
	}
	if le.Limit != busSpend {
		t.Errorf("Limit 应为车级 %d · 得 %d", busSpend, le.Limit)
	}
}

// §8.47 · 全局跟车级放宽时 · 全局仍生效(不能放宽)
func TestBusDaily_CannotRelaxGlobal(t *testing.T) {
	st := Defaults("p1")
	st.DailyRoundLimit = ip(5) // 全局 5 轮

	// 车级 100 轮(远宽于全局)· 跨车已用 5 · 全局拦
	busLimit := 100
	_, err := decide(st, "p1", CheckInput{
		BusID:         "b1",
		Count:         1,
		BusDailyRound: &busLimit,
		Used:          Usage{Rounds: 5}, // 全局 5 已满
		UsedBus:       &Usage{Rounds: 0},
	})
	if err == nil {
		t.Fatal("全局已满 · 车级即使很宽 · 也该被全局拦")
	}
	var le *LimitError
	if !errors.As(err, &le) || le.Kind != LimitDailyRound {
		t.Errorf("应报全局 daily_round · 得 %v", err)
	}
	if le.Limit != 5 {
		t.Errorf("Limit 应为全局 5 · 得 %d", le.Limit)
	}
}
