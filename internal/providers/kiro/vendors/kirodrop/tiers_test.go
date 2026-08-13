package kirodrop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 实测响应形状（2026-08-14 · 浏览器 session）· us 区 · schedule 两档
// ⚠️ 数值字段用浮点（interval_minutes:30.0 / reduction_number:0.0）· 实测 vendor 就这么返 ·
// 别改成 int —— 那样测不到"int 直接解会炸"这个真 bug（2026-08-14 live 抓到）。
const resvUS = `{"currency":"CNY","exchange_rate":"6.8","goods_id":1,"region":"us-east-1","stock":0,
 "quantity":1,"unit_price_cny":"49.980000","unit_price_usd":"7.35",
 "timed_pricing":{"enabled":true,"active":false,"interval_minutes":30.0,"max_reductions":1.0,
   "reductions_applied":0.0,"start_time":"2026-08-12T15:20:27.584046",
   "schedule":[{"reduction_number":0.0,"effective_at":"2026-08-12T15:20:27.584046","unit_price_cny":"49.980000","unit_price_usd":"7.35"},
               {"reduction_number":1.0,"effective_at":"2026-08-12T15:50:27.584046","unit_price_cny":"39.984000","unit_price_usd":"5.88"}]}}`

const resvEU = `{"currency":"CNY","exchange_rate":"6.8","goods_id":2,"region":"eu-central-1","stock":0,
 "quantity":1,"unit_price_cny":"34.952000","unit_price_usd":"5.14",
 "timed_pricing":{"enabled":true,"active":false,"interval_minutes":30.0,"max_reductions":1.0,
   "reductions_applied":0.0,"start_time":"2026-08-12T15:20:27.586336",
   "schedule":[{"reduction_number":0.0,"effective_at":"2026-08-12T15:20:27.586336","unit_price_cny":"34.952000","unit_price_usd":"5.14"},
               {"reduction_number":1.0,"effective_at":"2026-08-12T15:50:27.586336","unit_price_cny":"24.956000","unit_price_usd":"3.67"}]}}`

func TestListTimeDecay_Parse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sess-xyz" {
			t.Errorf("Authorization 头应为 Bearer · 实际 %q", got)
		}
		if r.Header.Get("X-API-Key") != "" {
			t.Errorf("/api/v1/* 不该带 X-API-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("region") {
		case "us":
			_, _ = w.Write([]byte(resvUS))
		case "eu":
			_, _ = w.Write([]byte(resvEU))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "sess-xyz"})
	if err != nil {
		t.Fatal(err)
	}
	tiers, err := a.ListTimeDecay(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 2 {
		t.Fatalf("应 2 区 · 实际 %d", len(tiers))
	}

	us := tiers[0]
	if us.Region != "us" || !us.Enabled || us.IntervalMin != 30 || us.MaxReductions != 1 {
		t.Errorf("us 头字段错：%+v", us)
	}
	if len(us.Schedule) != 2 {
		t.Fatalf("us schedule 应 2 档 · 实际 %d", len(us.Schedule))
	}
	// 价格：49.98 CNY → 49_980_000 microunit 积分 · 7.35 USD → 7_350_000
	if us.Schedule[0].UnitPriceCredits != 49_980_000 {
		t.Errorf("us base 积分应 49_980_000 · 实际 %d", us.Schedule[0].UnitPriceCredits)
	}
	if us.Schedule[0].UnitPriceUSDRaw != 7_350_000 {
		t.Errorf("us base USD 应 7_350_000 · 实际 %d", us.Schedule[0].UnitPriceUSDRaw)
	}
	if us.Schedule[1].Index != 1 || us.Schedule[1].UnitPriceCredits != 39_984_000 {
		t.Errorf("us 第1档错：%+v", us.Schedule[1])
	}
	// 时区：15:20:27 北京 → 07:20:27 UTC
	if got := us.Schedule[0].EffectiveAt.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-08-12T07:20:27Z" {
		t.Errorf("us base effective_at 应转 UTC 07:20:27 · 实际 %s", got)
	}

	if tiers[1].Region != "eu" || tiers[1].Schedule[1].UnitPriceCredits != 24_956_000 {
		t.Errorf("eu 解析错：%+v", tiers[1])
	}
}

// 无 token · 静默跳过（返 nil · backfiller 不清旧值）
func TestListTimeDecay_NoToken(t *testing.T) {
	a, err := New(Config{BaseURL: "https://unused.example", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	tiers, err := a.ListTimeDecay(context.Background())
	if err != nil {
		t.Fatalf("无 token 不该报错 · 实际 %v", err)
	}
	if tiers != nil {
		t.Errorf("无 token 应返 nil · 实际 %+v", tiers)
	}
}

// token 过期（401）· 返 error 让 backfiller 记 WARN 提示重 seed
func TestListTimeDecay_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"AUTH_INVALID"}}`))
	}))
	defer srv.Close()

	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListTimeDecay(context.Background()); err == nil {
		t.Error("401 应返 error")
	}
}
