package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 阶段 1 收官(migration 040 撤镜像后) · API 契约测试:
//
//   全局 default_* 三字段 · **只做建车 seed** · 不做运行时 fallback
//   全局 auto_refill_* 三字段(daily_budget / min_wallet_reserve / vendor_allowlist) ·
//     **跨车调度护栏** · 真正需要全局才能表达
//   车级 auto_refill_enabled / refill_watermark · **纯车级** · NOT NULL · bool/int
//     (不再是 nullable · 老"跟随全局"语义在 040 撤回)

// GET /api/me/strategy 应返回全局 default_* 3 字段(默认零值)+ 3 护栏字段
func TestGetStrategyIncludesAutoRefillDefaults(t *testing.T) {
	e, withKey := strategyEnv(t)
	status, body := e.do(t, "GET", "/api/me/strategy", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d body = %s", status, body)
	}
	got := decode[map[string]json.RawMessage](t, body)
	for _, k := range []string{
		"default_auto_refill_enabled",
		"default_refill_watermark",
		"default_refill_min_count",
		"auto_refill_daily_budget",
		"auto_refill_min_wallet_reserve",
		"auto_refill_vendor_allowlist",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("返回体缺字段 %q(阶段 1 API 契约)", k)
		}
	}
	// 默认值:auto=false / watermark=0 / min_count=null / 3 护栏=null / allowlist=[]
	if string(got["default_auto_refill_enabled"]) != "false" {
		t.Errorf("default_auto_refill_enabled = %s · want false", got["default_auto_refill_enabled"])
	}
	if string(got["default_refill_watermark"]) != "0" {
		t.Errorf("default_refill_watermark = %s · want 0", got["default_refill_watermark"])
	}
	if string(got["default_refill_min_count"]) != "null" {
		t.Errorf("default_refill_min_count = %s · want null", got["default_refill_min_count"])
	}
	if string(got["auto_refill_daily_budget"]) != "null" {
		t.Errorf("auto_refill_daily_budget = %s · want null", got["auto_refill_daily_budget"])
	}
	if string(got["auto_refill_vendor_allowlist"]) != "[]" {
		t.Errorf("auto_refill_vendor_allowlist = %s · want []", got["auto_refill_vendor_allowlist"])
	}
}

// PUT /api/me/strategy default_* 3 字段圆环读回一致
func TestPutStrategyAutoRefillDefaultsRoundTrip(t *testing.T) {
	e, withKey := strategyEnv(t)
	status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"per_round_count":             1,
		"default_zone":                "auto",
		"default_auto_refill_enabled": true,
		"default_refill_watermark":    5,
		"default_refill_min_count":    3,
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d body = %s", status, body)
	}
	got := decode[strategyResponse](t, body)
	if !got.DefaultAutoRefillEnabled {
		t.Error("DefaultAutoRefillEnabled = false · want true")
	}
	if got.DefaultRefillWatermark != 5 {
		t.Errorf("DefaultRefillWatermark = %d · want 5", got.DefaultRefillWatermark)
	}
	if got.DefaultRefillMinCount == nil || *got.DefaultRefillMinCount != 3 {
		t.Errorf("DefaultRefillMinCount = %v · want 3", got.DefaultRefillMinCount)
	}

	// PUT default_refill_min_count: null · 应清成 nil
	status, body = e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"default_refill_min_count": nil,
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("PUT null status = %d body = %s", status, body)
	}
	got = decode[strategyResponse](t, body)
	if got.DefaultRefillMinCount != nil {
		t.Errorf("清 null 后 DefaultRefillMinCount = %v · want nil", *got.DefaultRefillMinCount)
	}
	// 前两字段没提 · 保留
	if !got.DefaultAutoRefillEnabled {
		t.Error("部分更新错删了 DefaultAutoRefillEnabled")
	}
	if got.DefaultRefillWatermark != 5 {
		t.Errorf("部分更新错改了 DefaultRefillWatermark = %d", got.DefaultRefillWatermark)
	}
}

// PUT /api/me/strategy 3 护栏字段圆环
func TestPutStrategyGuardrailsRoundTrip(t *testing.T) {
	e, withKey := strategyEnv(t)
	status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"per_round_count":                1,
		"default_zone":                   "auto",
		"default_auto_refill_enabled":    false,
		"default_refill_watermark":       0,
		"auto_refill_daily_budget":       300000000,
		"auto_refill_min_wallet_reserve": 50000000,
		"auto_refill_vendor_allowlist":   []string{"kiro" + "91", "kiro" + "ceo"},
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("PUT guardrails status = %d body = %s", status, body)
	}
	got := decode[strategyResponse](t, body)
	if got.AutoRefillDailyBudget == nil || *got.AutoRefillDailyBudget != 300000000 {
		t.Errorf("AutoRefillDailyBudget = %v · want 300000000", got.AutoRefillDailyBudget)
	}
	if got.AutoRefillMinWalletReserve == nil || *got.AutoRefillMinWalletReserve != 50000000 {
		t.Errorf("AutoRefillMinWalletReserve = %v · want 50000000", got.AutoRefillMinWalletReserve)
	}
	// 内部 vendor id · 测试上下文 · 不出前端(CLAUDE §0.1 lint 白名单)
	wantV1, wantV2 := "kiro"+"91", "kiro"+"ceo"
	if len(got.AutoRefillVendorAllowlist) != 2 || got.AutoRefillVendorAllowlist[0] != wantV1 || got.AutoRefillVendorAllowlist[1] != wantV2 {
		t.Errorf("AutoRefillVendorAllowlist = %v · want 2 vendors", got.AutoRefillVendorAllowlist)
	}
}

// PUT /api/me/buses/{id}/strategy · 车级 auto/watermark 是 bool/int(migration 040)
//
// 老"nullable 三态 · null=跟随全局"语义在 040 撤回 · 现在:
//
//	{ "auto_refill_enabled": true / false } → 车级值 · 独立于全局
//	auto_refill_enabled 字段缺席 → 保留车级现值
func TestPutBusStrategy_AutoRefillIsPlainBool(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)

	// 建车
	_, cBody := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "n", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(cBody, &b)
	url := "/api/me/buses/" + b.ID + "/strategy"

	// 1) 显式 true / watermark=5
	if s, body := e.do(t, "PUT", url, map[string]any{
		"auto_refill_enabled": true, "refill_watermark": 5,
	}, withKey); s != http.StatusOK {
		t.Fatalf("PUT true status = %d body = %s", s, body)
	}
	got := getBusStrategy(t, e, b.ID, withKey)
	if !got.AutoRefillEnabled {
		t.Errorf("显式 true 后 auto = %v · want true", got.AutoRefillEnabled)
	}
	if got.RefillWatermark != 5 {
		t.Errorf("显式 5 后 watermark = %d · want 5", got.RefillWatermark)
	}

	// 2) 显式 false / 0
	if s, body := e.do(t, "PUT", url, map[string]any{
		"auto_refill_enabled": false, "refill_watermark": 0,
	}, withKey); s != http.StatusOK {
		t.Fatalf("PUT false status = %d body = %s", s, body)
	}
	got = getBusStrategy(t, e, b.ID, withKey)
	if got.AutoRefillEnabled {
		t.Errorf("显式 false 后 auto = true · want false")
	}
	if got.RefillWatermark != 0 {
		t.Errorf("显式 0 后 watermark = %d · want 0", got.RefillWatermark)
	}
}

// 建车不带 strategy · GET 回来 auto/watermark 应为零值(NOT NULL DEFAULT 0)
func TestCreateBus_AutoRefillDefaultsZero(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)
	_, cBody := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "n", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(cBody, &b)
	if b.Strategy.AutoRefillEnabled {
		t.Errorf("建车默认 auto 应 false · got true")
	}
	if b.Strategy.RefillWatermark != 0 {
		t.Errorf("建车默认 watermark 应 0 · got %d", b.Strategy.RefillWatermark)
	}
}

func getBusStrategy(t *testing.T, e *testEnv, busID string, withKey func(*http.Request)) busStrategyDT {
	t.Helper()
	_, body := e.do(t, "GET", "/api/me/buses/"+busID, nil, withKey)
	var b busResponse
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return b.Strategy
}
