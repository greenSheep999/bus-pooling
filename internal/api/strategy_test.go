package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

const microUnit = 1_000_000

// strategyEnv 起环境 + 一个带 API key 的乘客。
func strategyEnv(t *testing.T) (*testEnv, func(*http.Request)) {
	t.Helper()
	e := newEnv(t)
	key := seedWithAPIKey(t, e, "s@example.com", "sammy", "password123")
	return e, func(r *http.Request) { r.Header.Set("X-API-Key", key) }
}

// 没配过策略时 GET 要返默认值 + used_today，而不是 404 ——
// 乘客注册后就该能打开「拉号偏好」页。
func TestGetStrategyReturnsDefaults(t *testing.T) {
	e, withKey := strategyEnv(t)

	status, body := e.do(t, "GET", "/api/me/strategy", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	// 用 map 而不是 struct —— 要断言 null 字段**存在**（前端 TS 是 `Money | null`，
	// 字段消失会让前端拿到 undefined 而不是 null）
	got := decode[map[string]json.RawMessage](t, body)
	for _, k := range []string{
		"max_unit_price", "daily_round_limit", "daily_spend_limit",
		"per_round_count", "preferred_vendor", "default_zone", "used_today",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("返回体缺字段 %q", k)
		}
	}
	for _, k := range []string{"max_unit_price", "daily_round_limit", "daily_spend_limit"} {
		if string(got[k]) != "null" {
			t.Errorf("%s = %s，默认应为 null（不限）", k, got[k])
		}
	}
	if string(got["default_zone"]) != `"auto"` {
		t.Errorf("default_zone = %s", got["default_zone"])
	}

	used := decode[usedTodayResponse](t, got["used_today"])
	if used.Rounds != 0 || used.Spend != 0 {
		t.Errorf("used_today = %+v，新乘客应为 0", used)
	}
}

func TestPutStrategyRoundTrips(t *testing.T) {
	e, withKey := strategyEnv(t)

	status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"max_unit_price":    30 * microUnit,
		"daily_round_limit": 20,
		"daily_spend_limit": 500 * microUnit,
		"per_round_count":   3,
		"preferred_vendor":  "kiro91",
		"default_zone":      "us",
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", status, body)
	}

	// PUT 直接返回更新后的完整对象 —— 前端不用再 GET 一次
	put := decode[strategyResponse](t, body)
	if put.MaxUnitPrice == nil || *put.MaxUnitPrice != 30*microUnit {
		t.Errorf("PUT 返回 max_unit_price = %v", put.MaxUnitPrice)
	}

	status, body = e.do(t, "GET", "/api/me/strategy", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d", status)
	}
	got := decode[strategyResponse](t, body)
	if got.MaxUnitPrice == nil || *got.MaxUnitPrice != 30*microUnit {
		t.Errorf("max_unit_price = %v", got.MaxUnitPrice)
	}
	if got.DailyRoundLimit == nil || *got.DailyRoundLimit != 20 {
		t.Errorf("daily_round_limit = %v", got.DailyRoundLimit)
	}
	if got.PerRoundCount != 3 {
		t.Errorf("per_round_count = %d", got.PerRoundCount)
	}
	if got.PreferredVendor == nil || *got.PreferredVendor != "kiro91" {
		t.Errorf("preferred_vendor = %v", got.PreferredVendor)
	}
	if got.DefaultZone != "us" {
		t.Errorf("default_zone = %q", got.DefaultZone)
	}
}

// 部分更新：body 里没提的字段不能被清掉。
func TestPutStrategyIsPartial(t *testing.T) {
	e, withKey := strategyEnv(t)

	if status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"max_unit_price":    30 * microUnit,
		"daily_round_limit": 20,
	}, withKey); status != http.StatusOK {
		t.Fatalf("首次 PUT status = %d, body = %s", status, body)
	}
	// 只改 per_round_count
	if status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"per_round_count": 5,
	}, withKey); status != http.StatusOK {
		t.Fatalf("二次 PUT status = %d, body = %s", status, body)
	}

	_, body := e.do(t, "GET", "/api/me/strategy", nil, withKey)
	got := decode[strategyResponse](t, body)
	if got.PerRoundCount != 5 {
		t.Errorf("per_round_count = %d，want 5", got.PerRoundCount)
	}
	if got.MaxUnitPrice == nil || *got.MaxUnitPrice != 30*microUnit {
		t.Error("没提到的 max_unit_price 被清掉了")
	}
	if got.DailyRoundLimit == nil || *got.DailyRoundLimit != 20 {
		t.Error("没提到的 daily_round_limit 被清掉了")
	}
}

// 显式传 null = 清成"不限"。跟"没提这个字段"必须是两种行为，
// 否则乘客设了上限就再也去不掉。
func TestPutStrategyExplicitNullClearsLimit(t *testing.T) {
	e, withKey := strategyEnv(t)

	if status, _ := e.do(t, "PUT", "/api/me/strategy",
		map[string]any{"max_unit_price": 5 * microUnit}, withKey); status != http.StatusOK {
		t.Fatal("设上限失败")
	}

	// 用原始 JSON 传 null（map[string]any{"k": nil} 序列化出来也是 null）
	status, body := e.do(t, "PUT", "/api/me/strategy",
		map[string]any{"max_unit_price": nil}, withKey)
	if status != http.StatusOK {
		t.Fatalf("清上限 status = %d, body = %s", status, body)
	}

	got := decode[strategyResponse](t, body)
	if got.MaxUnitPrice != nil {
		t.Errorf("max_unit_price = %v，应已清成不限", *got.MaxUnitPrice)
	}
}

// used_today 是只读的：前端常把整个对象 PUT 回来，不该因此报错，
// 也不该被写进策略。
func TestPutStrategyIgnoresUsedToday(t *testing.T) {
	e, withKey := strategyEnv(t)

	status, body := e.do(t, "PUT", "/api/me/strategy", map[string]any{
		"per_round_count": 2,
		"used_today":      map[string]any{"rounds": 999, "spend": 999 * microUnit},
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[strategyResponse](t, body)
	if got.UsedToday.Rounds != 0 {
		t.Errorf("used_today.rounds = %d，只读字段不该被写入", got.UsedToday.Rounds)
	}
}

func TestPutStrategyValidation(t *testing.T) {
	e, withKey := strategyEnv(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"非法 zone", map[string]any{"default_zone": "mars"}},
		{"per_round 为 0", map[string]any{"per_round_count": 0}},
		{"per_round 超 200", map[string]any{"per_round_count": 201}},
		{"负单价上限", map[string]any{"max_unit_price": -1}},
		{"负轮数上限", map[string]any{"daily_round_limit": -5}},
		// 非空字段传 null 是请求错误 —— 静默忽略会让客户端以为改成功了
		{"per_round_count 传 null", map[string]any{"per_round_count": nil}},
		{"default_zone 传 null", map[string]any{"default_zone": nil}},
		{"类型不对", map[string]any{"daily_round_limit": "twenty"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := e.do(t, "PUT", "/api/me/strategy", tc.body, withKey)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d，应为 400，body = %s", status, body)
			}
			// message 必须是人话且不含内部术语（CLAUDE.md §12.6）
			got := decode[Error](t, body)
			if got.Message == "" {
				t.Error("错误 message 不该为空")
			}
			for _, banned := range []string{"housepool", "record group", "provider", "strategy:", "microunit"} {
				if contains(got.Message, banned) {
					t.Errorf("message 含内部术语 %q: %s", banned, got.Message)
				}
			}
		})
	}
}

// 未鉴权不能读写策略。
func TestStrategyRequiresAuth(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"GET", "PUT"} {
		status, _ := e.do(t, m, "/api/me/strategy", map[string]any{"per_round_count": 2})
		if status != http.StatusUnauthorized {
			t.Errorf("%s 未鉴权 status = %d，应为 401", m, status)
		}
	}
}

// 策略是按乘客隔离的 —— 一个人的上限不能影响另一个人。
func TestStrategyIsPerPassenger(t *testing.T) {
	e := newEnv(t)
	k1 := seedWithAPIKey(t, e, "a@example.com", "aaa", "password123")
	k2 := seedWithAPIKey(t, e, "b@example.com", "bbb", "password123")
	with := func(k string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("X-API-Key", k) }
	}

	if status, _ := e.do(t, "PUT", "/api/me/strategy",
		map[string]any{"max_unit_price": 5 * microUnit}, with(k1)); status != http.StatusOK {
		t.Fatal("给 a 设上限失败")
	}

	_, body := e.do(t, "GET", "/api/me/strategy", nil, with(k2))
	got := decode[strategyResponse](t, body)
	if got.MaxUnitPrice != nil {
		t.Errorf("b 的 max_unit_price = %v，不该受 a 影响", *got.MaxUnitPrice)
	}
}

// 上限类错误必须带上细节，前端要提示"超了多少"（契约 §7）。
// 0 是有意义的上限值（全拦），不能被 omitempty 吞掉。
func TestLimitErrorsCarryDetails(t *testing.T) {
	t.Run("price_over_cap 带 cap/current", func(t *testing.T) {
		f := ErrPriceOverCap(5*microUnit, 26*microUnit)
		if f.Err.Code != CodePriceOverCap {
			t.Errorf("code = %q", f.Err.Code)
		}
		if f.Status != http.StatusConflict {
			t.Errorf("status = %d，契约说 409", f.Status)
		}
		if f.Err.Cap == nil || *f.Err.Cap != 5*microUnit {
			t.Errorf("cap = %v", f.Err.Cap)
		}
		if f.Err.Current == nil || *f.Err.Current != 26*microUnit {
			t.Errorf("current = %v", f.Err.Current)
		}
	})

	t.Run("daily_limit_reached 带 limit/used", func(t *testing.T) {
		f := ErrDailyLimitReached("", 20, 20)
		if f.Err.Code != CodeDailyLimitReached {
			t.Errorf("code = %q", f.Err.Code)
		}
		if f.Err.Limit == nil || *f.Err.Limit != 20 {
			t.Errorf("limit = %v", f.Err.Limit)
		}
		if f.Err.Used == nil || *f.Err.Used != 20 {
			t.Errorf("used = %v", f.Err.Used)
		}
	})

	// limit=0（"一分钱都不许花"）必须能序列化出来
	t.Run("0 值不被 omitempty 吞掉", func(t *testing.T) {
		f := ErrDailyLimitReached("", 0, 0)
		raw, err := json.Marshal(f.Err)
		if err != nil {
			t.Fatal(err)
		}
		got := decode[map[string]json.RawMessage](t, raw)
		if _, ok := got["limit"]; !ok {
			t.Errorf("limit=0 被吞了: %s", raw)
		}
		if _, ok := got["used"]; !ok {
			t.Errorf("used=0 被吞了: %s", raw)
		}
	})
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
