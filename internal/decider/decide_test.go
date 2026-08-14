package decider

// 决策器单测 · 覆盖 docs/15-scheduling.md §5 六步串行判据
//
// 场景覆盖:
//   - Step 1 kill_pulls
//   - Step 2 auto off / unknown source
//   - Step 3 no_watermark / at_watermark / has_backup / Case A 整车挂
//   - Step 4 mode × source 决策表
//   - 备胎判据 · 价格过滤 · 数据 stale

import (
	"context"
	"testing"
	"time"
)

func baseIn(source Source) DecideInput {
	return DecideInput{
		Source:            source,
		BusID:             "b1",
		AutoRefillEnabled: true,
		RefillWatermark:   5,
		RefillMinCount:    3,
		Mode:              "balance",
		AliveByVendor:     map[string]int{},
		PricesByVendor:    map[string]VendorPriceSnapshot{},
	}
}

func priceSnap(price int64) VendorPriceSnapshot {
	return VendorPriceSnapshot{
		UnitPriceMicro: price,
		ObservedAt:     time.Now(),
		Stale:          false,
	}
}

func TestDecide_Step1_KillPulls(t *testing.T) {
	in := baseIn(SourceScheduler)
	in.KillPulls = true
	got, _ := Decide(context.Background(), in)
	if got.Verdict != VerdictReject {
		t.Errorf("KillPulls 应拒 · 得 %v", got.Verdict)
	}
	if got.RejectReason != "kill_pulls · 全停" {
		t.Errorf("reason = %q", got.RejectReason)
	}
}

func TestDecide_Step2_AutoOff(t *testing.T) {
	for _, source := range []Source{SourceDeathRefill, SourceScheduler, SourceWebhook, SourceProbe} {
		t.Run(string(source), func(t *testing.T) {
			in := baseIn(source)
			in.AutoRefillEnabled = false
			got, _ := Decide(context.Background(), in)
			if got.Verdict != VerdictReject {
				t.Errorf("auto off 应拒 · 得 %v", got.Verdict)
			}
			if got.RejectReason != "auto_off · 用户关了自动补车" {
				t.Errorf("reason = %q", got.RejectReason)
			}
		})
	}
}

func TestDecide_Step2_UnknownSource(t *testing.T) {
	in := baseIn(Source("weird"))
	got, err := Decide(context.Background(), in)
	if got.Verdict != VerdictReject {
		t.Errorf("未知 source 应拒 · 得 %v", got.Verdict)
	}
	if err != ErrSourceUnimplemented {
		t.Errorf("应返 ErrSourceUnimplemented · 得 %v", err)
	}
}

func TestDecide_Step3_NoWatermark(t *testing.T) {
	in := baseIn(SourceScheduler)
	in.RefillWatermark = 0
	got, _ := Decide(context.Background(), in)
	if got.Verdict != VerdictReject {
		t.Errorf("Watermark=0 应拒 · 得 %v", got.Verdict)
	}
}

func TestDecide_Step3_AtWatermark(t *testing.T) {
	in := baseIn(SourceScheduler)
	in.RefillWatermark = 5
	in.AliveByVendor = map[string]int{"v1": 3, "v2": 3} // 6 ≥ 5
	got, _ := Decide(context.Background(), in)
	if got.Verdict != VerdictReject {
		t.Errorf("已达水位应拒 · 得 %v", got.Verdict)
	}
}

func TestDecide_Step3_HasBackup(t *testing.T) {
	// vendor01 死了(0) · vendor02 活 6 · min=3 · vendor02 撑得住 → 拒
	in := baseIn(SourceScheduler)
	in.RefillWatermark = 10 // target 高 · 保证不"已达水位"
	in.RefillMinCount = 3
	in.AliveByVendor = map[string]int{"v1": 0, "v2": 6}
	in.BusMaxUnitPrice = 100_000_000
	in.PricesByVendor = map[string]VendorPriceSnapshot{
		"v2": priceSnap(80_000_000), // v2 价格没超
	}
	got, _ := Decide(context.Background(), in)
	if got.Verdict != VerdictReject {
		t.Errorf("有备胎应拒 · 得 %v · reason=%s", got.Verdict, got.RejectReason)
	}
}

func TestDecide_Step3_HasBackup_ButPriceExceeded(t *testing.T) {
	// vendor02 活 6 但当前单价超 max · 不算备胎
	in := baseIn(SourceScheduler)
	in.RefillWatermark = 10
	in.RefillMinCount = 3
	in.AliveByVendor = map[string]int{"v1": 0, "v2": 6}
	in.BusMaxUnitPrice = 100_000_000
	in.PricesByVendor = map[string]VendorPriceSnapshot{
		"v2": priceSnap(150_000_000), // 超价
	}
	got, _ := Decide(context.Background(), in)
	if got.Verdict == VerdictReject && got.RejectReason == "has_backup · 备胎 vendor 撑得住 · 等它也见底" {
		t.Errorf("超价 vendor 不应算备胎 · 应过 Step 3 走 Step 4")
	}
}

func TestDecide_Step3_HasBackup_ButPriceStale(t *testing.T) {
	// 数据 stale · 视为撑不住(codex P0-A 修正)
	in := baseIn(SourceScheduler)
	in.RefillWatermark = 10
	in.RefillMinCount = 3
	in.AliveByVendor = map[string]int{"v1": 0, "v2": 6}
	in.BusMaxUnitPrice = 100_000_000
	in.PricesByVendor = map[string]VendorPriceSnapshot{
		"v2": VendorPriceSnapshot{UnitPriceMicro: 80_000_000, Stale: true},
	}
	got, _ := Decide(context.Background(), in)
	if got.Verdict == VerdictReject && got.RejectReason == "has_backup · 备胎 vendor 撑得住 · 等它也见底" {
		t.Errorf("stale 数据不应算备胎 · 得 %s", got.RejectReason)
	}
}

func TestDecide_Step4_CaseA_AllDead(t *testing.T) {
	// 整车挂 · Cool → Pull · Balance/Tight → Enqueue
	cases := []struct {
		mode string
		want Verdict
	}{
		{"cool", VerdictPull},
		{"balance", VerdictEnqueue},
		{"tight", VerdictEnqueue},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			in := baseIn(SourceScheduler)
			in.Mode = c.mode
			in.AliveByVendor = map[string]int{} // alive=0
			got, _ := Decide(context.Background(), in)
			if got.Verdict != c.want {
				t.Errorf("mode=%s want %v got %v · reason=%s", c.mode, c.want, got.Verdict, got.RejectReason)
			}
		})
	}
}

func TestDecide_Step4_Scheduler_ModeMatrix(t *testing.T) {
	// scheduler · Cool/Balance → Pull · Tight → Enqueue
	cases := []struct {
		mode string
		want Verdict
	}{
		{"cool", VerdictPull},
		{"balance", VerdictPull},
		{"tight", VerdictEnqueue},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			in := baseIn(SourceScheduler)
			in.Mode = c.mode
			// 撑不住:v1 活 1 < min=3
			in.AliveByVendor = map[string]int{"v1": 1}
			got, _ := Decide(context.Background(), in)
			if got.Verdict != c.want {
				t.Errorf("mode=%s want %v got %v · reason=%s", c.mode, c.want, got.Verdict, got.RejectReason)
			}
		})
	}
}

func TestDecide_Step4_Webhook_ModeMatrix(t *testing.T) {
	cases := []struct {
		mode string
		want Verdict
	}{
		{"cool", VerdictReject},
		{"balance", VerdictPull},
		{"tight", VerdictPull},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			in := baseIn(SourceWebhook)
			in.Mode = c.mode
			in.AliveByVendor = map[string]int{"v1": 1}
			got, _ := Decide(context.Background(), in)
			if got.Verdict != c.want {
				t.Errorf("mode=%s want %v got %v · reason=%s", c.mode, c.want, got.Verdict, got.RejectReason)
			}
		})
	}
}

func TestDecide_Step4_Probe_ModeMatrix(t *testing.T) {
	cases := []struct {
		mode string
		want Verdict
	}{
		{"cool", VerdictReject},
		{"balance", VerdictReject},
		{"tight", VerdictPull},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			in := baseIn(SourceProbe)
			in.Mode = c.mode
			in.AliveByVendor = map[string]int{"v1": 1}
			got, _ := Decide(context.Background(), in)
			if got.Verdict != c.want {
				t.Errorf("mode=%s want %v got %v · reason=%s", c.mode, c.want, got.Verdict, got.RejectReason)
			}
		})
	}
}

func TestDecide_Turbo_ForcesTightBehavior(t *testing.T) {
	// TURBO_ON · Cool 也走 Tight
	in := baseIn(SourceScheduler)
	in.Mode = "cool"
	in.Turbo = true
	in.AliveByVendor = map[string]int{} // alive=0 · Case A
	got, _ := Decide(context.Background(), in)
	if got.Verdict != VerdictEnqueue {
		t.Errorf("TURBO 应把 Cool 视为 Tight · Case A 挂单 · 得 %v", got.Verdict)
	}
}

func TestDecide_EffectiveMaxPrice(t *testing.T) {
	cases := []struct {
		name          string
		busMax        int64
		passengerMax  int64
		want          int64
	}{
		{"both zero", 0, 0, 0},
		{"only bus", 100, 0, 100},
		{"only passenger", 0, 100, 100},
		{"bus stricter", 50, 100, 50},
		{"passenger stricter", 100, 50, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveMaxPrice(c.busMax, c.passengerMax)
			if got != c.want {
				t.Errorf("want %d got %d", c.want, got)
			}
		})
	}
}
