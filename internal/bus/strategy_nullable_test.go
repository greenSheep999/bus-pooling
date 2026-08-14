package bus

import (
	"context"
	"testing"
)

// 1f-B(15-scheduling §4.3.2b 方案 A) · Bus 存储层 nullable 语义。
//
// **关键差别**：
//   nil pointer          = SQL NULL       = "跟随全局"(Effective() 层负责 fallback)
//   非 nil 指针的 0/false = SQL 显式 0/0   = "覆盖本车"(Effective() 层用车级值)
//
// 存储层测试重点：Create / Get / UpdateStrategy 三对入口能来回读写 NULL 和显式值 ·
// 不混淆 nil 和零值。

// 建车不传 Strategy · 三字段应全 NULL(nil pointer)· 表示"跟随全局"
func TestCreateBus_NoStrategy_NullableFieldsNil(t *testing.T) {
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
	if got.Strategy.AutoRefillEnabled != nil {
		t.Errorf("AutoRefillEnabled 应 nil(跟随全局) · got %v", *got.Strategy.AutoRefillEnabled)
	}
	if got.Strategy.RefillWatermark != nil {
		t.Errorf("RefillWatermark 应 nil(跟随全局) · got %v", *got.Strategy.RefillWatermark)
	}
	if got.Strategy.RefillMinCount != nil {
		t.Errorf("RefillMinCount 应 nil · got %v", *got.Strategy.RefillMinCount)
	}
}

// 建车显式覆盖 auto=false / watermark=0 · 落库应保持"非 nil 的零值" · 不能被存成 NULL
func TestCreateBus_ExplicitFalseAndZero_PersistedAsExplicit(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	falseVal := false
	zeroWatermark := 0
	b, err := s.Create(ctx, CreateInput{
		Name: "n", Kind: KindSingle, CreatorID: pid,
		Strategy: &Strategy{
			AutoRefillEnabled: &falseVal,
			RefillWatermark:   &zeroWatermark,
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// 显式 false 应读回 *bool = false(不是 nil)
	if got.Strategy.AutoRefillEnabled == nil {
		t.Fatal("AutoRefillEnabled 应 non-nil(显式 false) · 存成 NULL 就分不出'跟随'vs'关闭'了")
	}
	if *got.Strategy.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled 值应 false · got true")
	}
	if got.Strategy.RefillWatermark == nil {
		t.Fatal("RefillWatermark 应 non-nil(显式 0)")
	}
	if *got.Strategy.RefillWatermark != 0 {
		t.Errorf("RefillWatermark 值应 0 · got %d", *got.Strategy.RefillWatermark)
	}
}

// 建车显式 auto=true / watermark=5 · 落库 · 读回一致
func TestCreateBus_ExplicitTrueAndValue(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	trueVal := true
	watermark := 5
	b, err := s.Create(ctx, CreateInput{
		Name: "n", Kind: KindSingle, CreatorID: pid,
		Strategy: &Strategy{
			AutoRefillEnabled: &trueVal,
			RefillWatermark:   &watermark,
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := s.Get(ctx, b.ID)
	if got.Strategy.AutoRefillEnabled == nil || !*got.Strategy.AutoRefillEnabled {
		t.Errorf("AutoRefillEnabled = %v · want true", got.Strategy.AutoRefillEnabled)
	}
	if got.Strategy.RefillWatermark == nil || *got.Strategy.RefillWatermark != 5 {
		t.Errorf("RefillWatermark = %v · want 5", got.Strategy.RefillWatermark)
	}
}

// UpdateStrategy nil → SQL NULL · 显式值 → SQL 值 · 来回切换
func TestUpdateStrategy_NilAndExplicitRoundTrip(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()
	trueVal := true
	fiveW := 5
	b, err := s.Create(ctx, CreateInput{
		Name: "n", Kind: KindSingle, CreatorID: pid,
		Strategy: &Strategy{AutoRefillEnabled: &trueVal, RefillWatermark: &fiveW},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 1) 显式 → NULL(用户 UI 切成"跟随全局")
	if err := s.UpdateStrategy(ctx, b.ID, pid, Strategy{
		AutoRefillEnabled: nil,
		RefillWatermark:   nil,
	}); err != nil {
		t.Fatalf("UpdateStrategy → NULL: %v", err)
	}
	got, _ := s.Get(ctx, b.ID)
	if got.Strategy.AutoRefillEnabled != nil {
		t.Errorf("清成 NULL 后 AutoRefillEnabled 应 nil · got %v", *got.Strategy.AutoRefillEnabled)
	}
	if got.Strategy.RefillWatermark != nil {
		t.Errorf("清成 NULL 后 RefillWatermark 应 nil · got %v", *got.Strategy.RefillWatermark)
	}

	// 2) NULL → 显式 0 / false(用户切成"覆盖 · 关闭")
	falseVal := false
	zeroW := 0
	if err := s.UpdateStrategy(ctx, b.ID, pid, Strategy{
		AutoRefillEnabled: &falseVal,
		RefillWatermark:   &zeroW,
	}); err != nil {
		t.Fatalf("UpdateStrategy → 0/false: %v", err)
	}
	got, _ = s.Get(ctx, b.ID)
	if got.Strategy.AutoRefillEnabled == nil {
		t.Fatal("覆盖 false 后 AutoRefillEnabled 应 non-nil")
	}
	if *got.Strategy.AutoRefillEnabled {
		t.Errorf("覆盖 false 后值应 false · got true")
	}
	if got.Strategy.RefillWatermark == nil {
		t.Fatal("覆盖 0 后 RefillWatermark 应 non-nil")
	}
	if *got.Strategy.RefillWatermark != 0 {
		t.Errorf("覆盖 0 后值应 0 · got %d", *got.Strategy.RefillWatermark)
	}
}
