package strategy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

const micro = 1_000_000

func setup(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	const pid = "p1"
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`, pid); err != nil {
		t.Fatalf("建乘客: %v", err)
	}
	return NewStore(d.DB), pid
}

func i64(v int64) *int64  { return &v }
func ip(v int) *int       { return &v }
func sp(v string) *string { return &v }

// 没存过策略不该是错 —— 乘客注册后就该能拉号，不该因为没进过设置页而卡住。
func TestGetReturnsDefaultsWhenUnset(t *testing.T) {
	s, pid := setup(t)
	got, err := s.Get(context.Background(), pid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// 三个硬上限默认必须是"不限" —— 给个"合理"默认值等于替乘客做决定，
	// 而他根本没设过，拉不动时也不知道是被谁拦的
	if got.MaxUnitPrice != nil {
		t.Errorf("MaxUnitPrice = %v，默认应不限", *got.MaxUnitPrice)
	}
	if got.DailyRoundLimit != nil {
		t.Errorf("DailyRoundLimit = %v，默认应不限", *got.DailyRoundLimit)
	}
	if got.DailySpendLimit != nil {
		t.Errorf("DailySpendLimit = %v，默认应不限", *got.DailySpendLimit)
	}
	if got.DefaultZone != ZoneAuto {
		t.Errorf("DefaultZone = %q，want %q", got.DefaultZone, ZoneAuto)
	}
	if got.PerRoundCount != 1 {
		t.Errorf("PerRoundCount = %d，want 1", got.PerRoundCount)
	}
}

func TestPutThenGetRoundTrips(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	want := Patch{
		MaxUnitPrice:    ptr(i64(30 * micro)),
		DailyRoundLimit: ptr(ip(20)),
		DailySpendLimit: ptr(i64(500 * micro)),
		PerRoundCount:   ip(3),
		PreferredVendor: ptr(sp("kiro91")),
		DefaultZone:     sp(ZoneUS),
	}
	if _, err := s.Put(ctx, pid, want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.MaxUnitPrice == nil || *got.MaxUnitPrice != 30*micro {
		t.Errorf("MaxUnitPrice = %v", got.MaxUnitPrice)
	}
	if got.DailyRoundLimit == nil || *got.DailyRoundLimit != 20 {
		t.Errorf("DailyRoundLimit = %v", got.DailyRoundLimit)
	}
	if got.DailySpendLimit == nil || *got.DailySpendLimit != 500*micro {
		t.Errorf("DailySpendLimit = %v", got.DailySpendLimit)
	}
	if got.PerRoundCount != 3 {
		t.Errorf("PerRoundCount = %d", got.PerRoundCount)
	}
	if got.PreferredVendor == nil || *got.PreferredVendor != "kiro91" {
		t.Errorf("PreferredVendor = %v", got.PreferredVendor)
	}
	if got.DefaultZone != ZoneUS {
		t.Errorf("DefaultZone = %q", got.DefaultZone)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt 应有值")
	}
}

// 部分更新：没提到的字段不该被清掉。
func TestPutIsPartial(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, pid, Patch{
		MaxUnitPrice:    ptr(i64(30 * micro)),
		DailyRoundLimit: ptr(ip(20)),
	}); err != nil {
		t.Fatalf("首次 Put: %v", err)
	}
	// 只改 PerRoundCount
	if _, err := s.Put(ctx, pid, Patch{PerRoundCount: ip(5)}); err != nil {
		t.Fatalf("二次 Put: %v", err)
	}

	got, _ := s.Get(ctx, pid)
	if got.PerRoundCount != 5 {
		t.Errorf("PerRoundCount = %d，want 5", got.PerRoundCount)
	}
	if got.MaxUnitPrice == nil || *got.MaxUnitPrice != 30*micro {
		t.Error("没提到的 MaxUnitPrice 被清掉了")
	}
	if got.DailyRoundLimit == nil || *got.DailyRoundLimit != 20 {
		t.Error("没提到的 DailyRoundLimit 被清掉了")
	}
}

// 双层指针的意义：能表达「显式清空上限」，跟「没提这个字段」区分开。
// 少了这个能力，乘客设了上限就再也去不掉。
func TestPutCanClearLimitExplicitly(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, pid, Patch{MaxUnitPrice: ptr(i64(5 * micro))}); err != nil {
		t.Fatalf("设上限: %v", err)
	}
	// 外层非 nil、内层 nil = 显式设成"不限"
	var nilLimit *int64
	if _, err := s.Put(ctx, pid, Patch{MaxUnitPrice: &nilLimit}); err != nil {
		t.Fatalf("清上限: %v", err)
	}

	got, _ := s.Get(ctx, pid)
	if got.MaxUnitPrice != nil {
		t.Errorf("MaxUnitPrice = %v，应已清成不限", *got.MaxUnitPrice)
	}
}

func TestPutValidates(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		patch Patch
		want  error
	}{
		{"非法 zone", Patch{DefaultZone: sp("mars")}, ErrBadZone},
		{"per_round 为 0", Patch{PerRoundCount: ip(0)}, ErrBadPerRoundCount},
		{"per_round 超 200", Patch{PerRoundCount: ip(201)}, ErrBadPerRoundCount},
		// 负上限不是"不限"，是配错了 —— 会让每次拉号都被拦且很难查
		{"负单价上限", Patch{MaxUnitPrice: ptr(i64(-1))}, ErrNegativeLimit},
		{"负轮数上限", Patch{DailyRoundLimit: ptr(ip(-1))}, ErrNegativeLimit},
		{"负消费上限", Patch{DailySpendLimit: ptr(i64(-1))}, ErrNegativeLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Put(ctx, pid, tc.patch); !errors.Is(err, tc.want) {
				t.Errorf("err = %v，want %v", err, tc.want)
			}
		})
	}

	// 校验失败不该留下半截数据
	got, _ := s.Get(ctx, pid)
	if got.DefaultZone != ZoneAuto {
		t.Errorf("校验失败后 DefaultZone = %q，不该被改", got.DefaultZone)
	}
}

// 0 是合法上限（"一分钱都不许花"），跟 nil（不限）不同。
func TestZeroLimitIsValidAndMeansBlockAll(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, pid, Patch{DailyRoundLimit: ptr(ip(0))}); err != nil {
		t.Fatalf("Put 0 轮上限应合法: %v", err)
	}
	got, _ := s.Get(ctx, pid)
	if got.DailyRoundLimit == nil {
		t.Fatal("0 被存成了 nil —— 0（全拦）跟 nil（不限）是两回事")
	}
	if *got.DailyRoundLimit != 0 {
		t.Errorf("DailyRoundLimit = %d，want 0", *got.DailyRoundLimit)
	}

	// 且它必须真的拦住
	_, err := s.CanPull(ctx, pid, CheckInput{Count: 1})
	if !errors.Is(err, ErrLimitReached) {
		t.Errorf("0 轮上限应拦住拉号，得到 %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
func bp(v bool) *bool  { return &v }

// 1f-B(15-scheduling §4.3.2b) · 全局默认三字段读写。
//
// 三种场景：
//  1. 没配过 · Get 返默认值(auto=false / watermark=0 / min_count=nil)
//  2. Put 后 Get · 值应保留
//  3. min_count 可显式 null(§4.3.2c 选项 X · null = 按 gap 补差额)
func TestGlobalAutoRefillDefaultsRoundTrip(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	// 1) 没配过 · 默认值
	got, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DefaultAutoRefillEnabled {
		t.Error("默认 DefaultAutoRefillEnabled 应为 false")
	}
	if got.DefaultRefillWatermark != 0 {
		t.Errorf("默认 DefaultRefillWatermark = %d · 应为 0", got.DefaultRefillWatermark)
	}
	if got.DefaultRefillMinCount != nil {
		t.Errorf("默认 DefaultRefillMinCount = %v · 应为 nil", *got.DefaultRefillMinCount)
	}

	// 2) 写入后 Get · 值应保留
	if _, err := s.Put(ctx, pid, Patch{
		DefaultAutoRefillEnabled: bp(true),
		DefaultRefillWatermark:   ip(5),
		DefaultRefillMinCount:    ptr(ip(3)),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _ = s.Get(ctx, pid)
	if !got.DefaultAutoRefillEnabled {
		t.Error("DefaultAutoRefillEnabled 应为 true")
	}
	if got.DefaultRefillWatermark != 5 {
		t.Errorf("DefaultRefillWatermark = %d · want 5", got.DefaultRefillWatermark)
	}
	if got.DefaultRefillMinCount == nil || *got.DefaultRefillMinCount != 3 {
		t.Errorf("DefaultRefillMinCount = %v · want 3", got.DefaultRefillMinCount)
	}

	// 3) min_count 显式设成 null(按 gap 补差额)
	var nilMin *int
	if _, err := s.Put(ctx, pid, Patch{DefaultRefillMinCount: &nilMin}); err != nil {
		t.Fatalf("清 min_count: %v", err)
	}
	got, _ = s.Get(ctx, pid)
	if got.DefaultRefillMinCount != nil {
		t.Errorf("DefaultRefillMinCount 应清成 nil · got %v", *got.DefaultRefillMinCount)
	}
	// 其它两字段应保留
	if !got.DefaultAutoRefillEnabled || got.DefaultRefillWatermark != 5 {
		t.Errorf("清 min_count 时改动了其它字段 · auto=%v watermark=%d",
			got.DefaultAutoRefillEnabled, got.DefaultRefillWatermark)
	}
}

// 全局默认字段的 0 / false 是合法值(不是"跟随"):
//   - watermark=0 = 不触发自动补
//   - auto=false  = 关闭自动补
// 都能落库 · 读回来一致。
func TestGlobalAutoRefillZeroFalseIsExplicit(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	// 先设成 true / 10 · 再显式关成 false / 0 · 确认能真的关掉
	if _, err := s.Put(ctx, pid, Patch{
		DefaultAutoRefillEnabled: bp(true),
		DefaultRefillWatermark:   ip(10),
	}); err != nil {
		t.Fatalf("初次 Put: %v", err)
	}
	if _, err := s.Put(ctx, pid, Patch{
		DefaultAutoRefillEnabled: bp(false),
		DefaultRefillWatermark:   ip(0),
	}); err != nil {
		t.Fatalf("关闭 Put: %v", err)
	}
	got, _ := s.Get(ctx, pid)
	if got.DefaultAutoRefillEnabled {
		t.Error("DefaultAutoRefillEnabled 应关成 false")
	}
	if got.DefaultRefillWatermark != 0 {
		t.Errorf("DefaultRefillWatermark = %d · 应关成 0", got.DefaultRefillWatermark)
	}
}

// 负值不合法 —— watermark / min_count 都是"数量" · 负数没语义。
func TestGlobalAutoRefillRejectsNegative(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, pid, Patch{DefaultRefillWatermark: ip(-1)}); !errors.Is(err, ErrNegativeLimit) {
		t.Errorf("负 watermark 应拒 · got %v", err)
	}
	if _, err := s.Put(ctx, pid, Patch{DefaultRefillMinCount: ptr(ip(-1))}); !errors.Is(err, ErrNegativeLimit) {
		t.Errorf("负 min_count 应拒 · got %v", err)
	}
}
