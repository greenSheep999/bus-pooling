package insight_test

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/insight"
)

// TestTrend_FillsGaps 每天都有点，空缺补 0
func TestTrend_FillsGaps(t *testing.T) {
	e := setup(t)
	e.insertLedger("key_cost", -20_000_000, e.now, "今日")
	// -2 天 也造一笔
	twoDaysAgo := e.now.AddDate(0, 0, -2)
	e.insertLedger("service_fee", -5_000_000, twoDaysAgo, "两天前")

	pts, err := e.st.Trend(context.Background(), e.pid, insight.TrendCredits, 7, insight.TrendScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 7 {
		t.Fatalf("窗口=7 得 %d 点", len(pts))
	}
	// 最新点 = 今日，值 = 20
	last := pts[len(pts)-1]
	if last.Value != 20 {
		t.Errorf("今日值=%v，应=20", last.Value)
	}
	// 中间某些点应=0（补 0 的证据）
	seenZero := false
	for _, p := range pts[:len(pts)-1] {
		if p.Value == 0 {
			seenZero = true
		}
	}
	if !seenZero {
		t.Error("窗口内应有 0 点（说明填了 gap）")
	}
}

// TestTrend_Pulls 每日拉号轮次数
func TestTrend_Pulls(t *testing.T) {
	e := setup(t)
	// 今日 2 轮，昨日 1 轮
	e.insertPullRound("91kiro", e.bus, 3, 60_000_000, e.now)
	e.insertPullRound("91kiro", e.bus, 1, 20_000_000, e.now)
	e.insertPullRound("91kiro", e.bus, 2, 40_000_000, e.now.AddDate(0, 0, -1))

	pts, err := e.st.Trend(context.Background(), e.pid, insight.TrendPulls, 3, insight.TrendScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 3 {
		t.Fatal(len(pts))
	}
	// 最新 = 今日 = 2
	if pts[len(pts)-1].Value != 2 {
		t.Errorf("今日轮数=%v，应=2", pts[len(pts)-1].Value)
	}
	// 昨日 = 1
	if pts[len(pts)-2].Value != 1 {
		t.Errorf("昨日轮数=%v，应=1", pts[len(pts)-2].Value)
	}
}

// TestTrend_ScopeMutex bus_id + vendor 同时传 → 报错
func TestTrend_ScopeMutex(t *testing.T) {
	e := setup(t)
	_, err := e.st.Trend(context.Background(), e.pid, insight.TrendCredits, 7,
		insight.TrendScope{BusID: e.bus, VendorID: "91kiro"})
	if err == nil {
		t.Fatal("bus_id + vendor 同传应报错")
	}
}
