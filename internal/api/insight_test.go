package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
)

// ── mock insightReader / busChecker ─────────────────────

type fakeInsight struct {
	overview   *insight.Overview
	overviewOK bool
	trend      []insight.TrendPoint
	activities []insight.Activity
	total      int

	gotMetric insight.TrendMetric
	gotDays   int
	gotScope  insight.TrendScope
	gotPage   int
	gotSize   int
	pid       string
}

func (f *fakeInsight) Overview(ctx context.Context, pid string) (*insight.Overview, error) {
	f.pid = pid
	if !f.overviewOK {
		return &insight.Overview{
			KPI:     insight.KPI{Balance: 500_000_000},
			Buses:   insight.BusesSummary{Items: []insight.BusSummaryRow{}},
			Extract: insight.ExtractSummary{ByDestination: []insight.DestinationRow{{Destination: "pending", Count: 0}, {Destination: "into_bus", Count: 0}, {Destination: "push_pool", Count: 0}}},
		}, nil
	}
	return f.overview, nil
}

func (f *fakeInsight) Trend(ctx context.Context, pid string, m insight.TrendMetric, days int, sc insight.TrendScope) ([]insight.TrendPoint, error) {
	f.pid, f.gotMetric, f.gotDays, f.gotScope = pid, m, days, sc
	return f.trend, nil
}

func (f *fakeInsight) Activities(ctx context.Context, pid string, page, size int) ([]insight.Activity, int, error) {
	f.pid, f.gotPage, f.gotSize = pid, page, size
	return f.activities, f.total, nil
}

type fakeBusChecker struct {
	allowedPassenger, allowedBus string
}

func (f *fakeBusChecker) GetForPassenger(ctx context.Context, busID, pid string) (*bus.Bus, error) {
	if busID != f.allowedBus || pid != f.allowedPassenger {
		return nil, bus.ErrNotMember
	}
	return &bus.Bus{ID: busID}, nil
}

// with 塞一个 passenger 到 ctx 后再调 handler —— 复现 RequireAuth 装的效果
func withPassenger(pid string) func(*http.Request) {
	p := &passenger.Passenger{ID: pid}
	return func(r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKeyPassenger, p)
		*r = *r.WithContext(ctx)
	}
}

// call 走一次 handler，返 status + body
func call(t *testing.T, h handler, method, url string, decor ...func(*http.Request)) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	for _, d := range decor {
		d(req)
	}
	rr := httptest.NewRecorder()
	if err := h(rr, req); err != nil {
		writeFail(rr, req, err)
	}
	return rr.Code, rr.Body.Bytes()
}

// ── 用例 ────────────────────────────────────────────

func TestOverview_HappyPath(t *testing.T) {
	f := &fakeInsight{}
	h := handleOverviewWith(f)
	status, body := call(t, h, "GET", "/api/me/overview", withPassenger("p1"))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	// 响应体里**必须**含 kpi / buses / extract 三个键（前端 assumes）
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kpi", "buses", "extract"} {
		if _, ok := got[k]; !ok {
			t.Errorf("overview 响应缺 %q 键", k)
		}
	}
	// 响应体里**不许**含加价链分层字段
	blocklist := []string{"key_cost", "vendor_fee", "region_fee",
		"single_pull_fee", "capability_fee", "housepool", "record_group"}
	low := strings.ToLower(string(body))
	for _, b := range blocklist {
		if strings.Contains(low, b) {
			t.Errorf("overview 响应含内部术语 %q: %s", b, low)
		}
	}
	if f.pid != "p1" {
		t.Errorf("pid=%q，应传给 store", f.pid)
	}
}

func TestOverview_Unauth(t *testing.T) {
	f := &fakeInsight{}
	h := handleOverviewWith(f)
	status, body := call(t, h, "GET", "/api/me/overview")
	if status != http.StatusUnauthorized {
		t.Fatalf("无鉴权 status=%d，应=401，body=%s", status, body)
	}
}

func TestTrend_RangeMapping(t *testing.T) {
	cases := []struct {
		q    string
		want int
	}{
		{"", 30},
		{"today", 1},
		{"7d", 7},
		{"30d", 30},
		{"90d", 90},
		{"garbage", 30},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			f := &fakeInsight{}
			h := handleTrendWith(f, nil)
			u := "/api/me/trend"
			if tc.q != "" {
				u += "?range=" + tc.q
			}
			status, _ := call(t, h, "GET", u, withPassenger("p1"))
			if status != 200 {
				t.Fatal(status)
			}
			if f.gotDays != tc.want {
				t.Errorf("range=%q days=%d，应=%d", tc.q, f.gotDays, tc.want)
			}
		})
	}
}

func TestTrend_MetricValidation(t *testing.T) {
	f := &fakeInsight{}
	h := handleTrendWith(f, nil)
	status, body := call(t, h, "GET", "/api/me/trend?metric=nonsense", withPassenger("p1"))
	if status != http.StatusBadRequest {
		t.Fatalf("非法 metric status=%d body=%s", status, body)
	}
}

func TestTrend_ScopeMutex(t *testing.T) {
	f := &fakeInsight{}
	h := handleTrendWith(f, nil)
	status, _ := call(t, h, "GET", "/api/me/trend?bus_id=b1&vendor=91kiro", withPassenger("p1"))
	if status != http.StatusBadRequest {
		t.Errorf("同传 bus_id + vendor status=%d，应=400", status)
	}
}

func TestTrend_BusOwnershipChecked(t *testing.T) {
	f := &fakeInsight{}
	ck := &fakeBusChecker{allowedPassenger: "p1", allowedBus: "b1"}
	h := handleTrendWith(f, ck)

	// 别人的车 → 404
	status, _ := call(t, h, "GET", "/api/me/trend?bus_id=someone_else", withPassenger("p1"))
	if status != http.StatusNotFound {
		t.Errorf("别人的车 status=%d，应=404", status)
	}

	// 自己的车 → 200
	status, _ = call(t, h, "GET", "/api/me/trend?bus_id=b1", withPassenger("p1"))
	if status != http.StatusOK {
		t.Errorf("自己车 status=%d，应=200", status)
	}
	if f.gotScope.BusID != "b1" {
		t.Errorf("scope.BusID=%q，应传给 store", f.gotScope.BusID)
	}
}

func TestActivities_Envelope(t *testing.T) {
	amt := int64(50_000_000)
	f := &fakeInsight{
		activities: []insight.Activity{
			{ID: "a1", Kind: "topup", Summary: "充值到账", Amount: &amt, CreatedAt: "2026-08-09T12:00:00Z"},
		},
		total: 1,
	}
	h := handleActivitiesWith(f)
	status, body := call(t, h, "GET", "/api/me/activities?page=1&page_size=20", withPassenger("p1"))
	if status != 200 {
		t.Fatalf("status=%d body=%s", status, body)
	}
	var got struct {
		Items    []insight.Activity `json:"items"`
		Total    int                `json:"total"`
		Page     int                `json:"page"`
		PageSize int                `json:"page_size"`
		Pages    int                `json:"pages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Page != 1 || got.PageSize != 20 {
		t.Errorf("信封字段错：%+v", got)
	}
}

func TestActivities_EmptyItemsAlwaysArray(t *testing.T) {
	// activities nil 时前端会挂 · 必须回空数组
	f := &fakeInsight{activities: nil, total: 0}
	h := handleActivitiesWith(f)
	_, body := call(t, h, "GET", "/api/me/activities", withPassenger("p1"))
	var got struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Items) != "[]" {
		t.Errorf("空列表得 %s，应=[]", string(got.Items))
	}
}

func TestVendorPrices_EmptyTrends(t *testing.T) {
	status, body := call(t, handler(handleVendorPrices), "GET", "/api/vendors/prices", withPassenger("p1"))
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	var got struct {
		Trends json.RawMessage `json:"trends"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Trends) != "[]" {
		t.Errorf("1a 阶段应返空数组，得 %s", string(got.Trends))
	}
}
