package bus

import (
	"context"
	"testing"
)

// migration 040 后 · Bus 存储层策略字段语义:
//
//   AutoRefillEnabled  · bool · NOT NULL DEFAULT 0(纯车级 · 无跟随全局)
//   RefillWatermark    · int  · NOT NULL DEFAULT 0(纯车级)
//   RefillMinCount     · *int · 可空 · nil = 按 gap 补齐差额
//   PerRoundCount / MaxUnitPrice / PreferredVendor · 仍 nullable(nil = 跟随全局)
//
// 老的 nullable 三态测试(1f-B 方案 A · 已在 6d446e9 refactor 撤回)在 040 之后语义作废 ·
// 这里重写为**保行为**语义测试:建车不传 = 零值 · 传值 = 落库回读。

// 建车不传 Strategy · auto/watermark 应 = 零值(0/false)· min_count = nil
func TestCreateBus_NoStrategy_AutoRefillZero(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	b, err := s.Create(ctx, CreateInput{Name: "n", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Strategy.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled 应默认 false · 得 %v", got.Strategy.AutoRefillEnabled)
	}
	if got.Strategy.RefillWatermark != 0 {
		t.Errorf("RefillWatermark 应默认 0 · 得 %d", got.Strategy.RefillWatermark)
	}
	if got.Strategy.RefillMinCount != nil {
		t.Errorf("RefillMinCount 应默认 nil · 得 %v", got.Strategy.RefillMinCount)
	}
}

// UpdateStrategy 显式设 auto=true / watermark=5 · 回读一致
func TestUpdateStrategy_AutoRefillRoundtrip(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	b, err := s.Create(ctx, CreateInput{Name: "n", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newMinCount := 3
	if err := s.UpdateStrategy(ctx, b.ID, pid, Strategy{
		AutoRefillEnabled: true,
		RefillWatermark:   5,
		RefillMinCount:    &newMinCount,
	}); err != nil {
		t.Fatalf("UpdateStrategy: %v", err)
	}

	got, err := s.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Strategy.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled 应 true · 得 %v", got.Strategy.AutoRefillEnabled)
	}
	if got.Strategy.RefillWatermark != 5 {
		t.Errorf("RefillWatermark 应 5 · 得 %d", got.Strategy.RefillWatermark)
	}
	if got.Strategy.RefillMinCount == nil || *got.Strategy.RefillMinCount != 3 {
		t.Errorf("RefillMinCount 应 &3 · 得 %v", got.Strategy.RefillMinCount)
	}
}

// UpdateStrategy 显式关 · 老车 auto=true 改回 false 应能回读到
func TestUpdateStrategy_AutoRefillDisable(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	b, err := s.Create(ctx, CreateInput{Name: "n", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.UpdateStrategy(ctx, b.ID, pid, Strategy{
		AutoRefillEnabled: true,
		RefillWatermark:   3,
	}); err != nil {
		t.Fatalf("UpdateStrategy open: %v", err)
	}
	// 再关掉
	if err := s.UpdateStrategy(ctx, b.ID, pid, Strategy{
		AutoRefillEnabled: false,
		RefillWatermark:   0,
	}); err != nil {
		t.Fatalf("UpdateStrategy close: %v", err)
	}
	got, err := s.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Strategy.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled 应 false(关了)· 得 true")
	}
	if got.Strategy.RefillWatermark != 0 {
		t.Errorf("RefillWatermark 应 0(关了)· 得 %d", got.Strategy.RefillWatermark)
	}
}
