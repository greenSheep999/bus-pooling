package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 1f-B API 契约测试(15-scheduling §4.3.5) · 前端 TS 已把 auto/refill 三字段改成 `| null` ·
// 后端要能区分 "null(跟随全局)" 和 "有值(覆盖本车 · 含 0 / false)"。

// GET /api/me/strategy 应返回 3 新字段(默认零值)
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
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("返回体缺字段 %q(前端 TS 契约)", k)
		}
	}
	// 默认值:auto=false / watermark=0 / min_count=null
	if string(got["default_auto_refill_enabled"]) != "false" {
		t.Errorf("default_auto_refill_enabled = %s · want false", got["default_auto_refill_enabled"])
	}
	if string(got["default_refill_watermark"]) != "0" {
		t.Errorf("default_refill_watermark = %s · want 0", got["default_refill_watermark"])
	}
	if string(got["default_refill_min_count"]) != "null" {
		t.Errorf("default_refill_min_count = %s · want null", got["default_refill_min_count"])
	}
}

// PUT /api/me/strategy 3 新字段 · 圆环读回一致
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

// PUT /api/me/buses/{id}/strategy · null vs 值区分
//
// 前端 TS 契约(15-scheduling §4.3.5.3)：
//
//	{ "auto_refill_enabled": null }   → 车级 NULL(跟随全局)
//	{ "auto_refill_enabled": false }  → 车级 false(显式关闭 · 覆盖全局)
//	{ "auto_refill_enabled": true }   → 车级 true(显式开启 · 覆盖全局)
//
// 三个 payload 应产生三种不同的落库状态 · GET 回来能区分。
func TestPutBusStrategy_NullVsExplicitValue(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)

	// 建车
	_, cBody := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "n", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(cBody, &b)
	url := "/api/me/buses/" + b.ID + "/strategy"

	// 1) 显式 true
	if s, body := e.do(t, "PUT", url, map[string]any{
		"auto_refill_enabled": true, "refill_watermark": 5,
	}, withKey); s != http.StatusOK {
		t.Fatalf("PUT true status = %d body = %s", s, body)
	}
	got := getBusStrategy(t, e, b.ID, withKey)
	if got.AutoRefillEnabled == nil || !*got.AutoRefillEnabled {
		t.Errorf("显式 true 后 auto = %v · want true", got.AutoRefillEnabled)
	}
	if got.RefillWatermark == nil || *got.RefillWatermark != 5 {
		t.Errorf("显式 5 后 watermark = %v · want 5", got.RefillWatermark)
	}

	// 2) 显式 false / 0 (覆盖本车 · 关闭)
	if s, body := e.do(t, "PUT", url, map[string]any{
		"auto_refill_enabled": false, "refill_watermark": 0,
	}, withKey); s != http.StatusOK {
		t.Fatalf("PUT false status = %d body = %s", s, body)
	}
	got = getBusStrategy(t, e, b.ID, withKey)
	if got.AutoRefillEnabled == nil {
		t.Fatal("显式 false 后 auto 应 non-nil(覆盖) · got nil(跟随)")
	}
	if *got.AutoRefillEnabled {
		t.Errorf("显式 false 后 auto = true · want false")
	}
	if got.RefillWatermark == nil || *got.RefillWatermark != 0 {
		t.Errorf("显式 0 后 watermark = %v · want 0", got.RefillWatermark)
	}

	// 3) 显式 null(跟随全局)
	if s, body := e.do(t, "PUT", url, map[string]any{
		"auto_refill_enabled": nil, "refill_watermark": nil,
	}, withKey); s != http.StatusOK {
		t.Fatalf("PUT null status = %d body = %s", s, body)
	}
	got = getBusStrategy(t, e, b.ID, withKey)
	if got.AutoRefillEnabled != nil {
		t.Errorf("PUT null 后 auto 应 nil(跟随) · got %v", *got.AutoRefillEnabled)
	}
	if got.RefillWatermark != nil {
		t.Errorf("PUT null 后 watermark 应 nil(跟随) · got %v", *got.RefillWatermark)
	}
}

// 建车不带 strategy · GET 回来 auto/watermark 应为 null(跟随全局)
func TestCreateBus_NullableFieldsDefaultToNull(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)
	_, cBody := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "n", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(cBody, &b)
	if b.Strategy.AutoRefillEnabled != nil {
		t.Errorf("建车默认 auto 应 nil · got %v", *b.Strategy.AutoRefillEnabled)
	}
	if b.Strategy.RefillWatermark != nil {
		t.Errorf("建车默认 watermark 应 nil · got %v", *b.Strategy.RefillWatermark)
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
