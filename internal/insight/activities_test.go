package insight_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestActivities_MixesStreams 三源合流，按时间倒序
func TestActivities_MixesStreams(t *testing.T) {
	e := setup(t)
	// 依时间倒序造：最近的是充值，中间是拉号，最早是号死
	round := e.insertPullRound("91kiro", e.bus, 3, 60_000_000, e.now.Add(-2*time.Hour))
	pulled := e.now.Add(-3 * time.Hour)
	dead := e.now.Add(-30 * time.Minute)
	e.insertCredential("91kiro", "bus-"+e.bus, "dead", pulled, &dead, nil, e.bus, "", round)
	e.insertLedger("recharge", 100_000_000, e.now, "充值到账")

	items, total, err := e.st.Activities(context.Background(), e.pid, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	// 至少 3 条（充值/拉号/号死）· push 事件也可能因 nil pushed_at 不产出
	if total < 3 {
		t.Fatalf("total=%d 应≥3", total)
	}
	if items[0].Kind != "topup" {
		t.Errorf("首条应=topup（最新充值），得 %q", items[0].Kind)
	}
	if items[0].Amount == nil || *items[0].Amount != 100_000_000 {
		t.Errorf("首条 amount=%v，应=100_000_000", items[0].Amount)
	}
	// 时间倒序
	for i := 1; i < len(items); i++ {
		if items[i-1].CreatedAt < items[i].CreatedAt {
			t.Errorf("顺序错乱：%d.at=%s < %d.at=%s", i-1, items[i-1].CreatedAt, i, items[i].CreatedAt)
		}
	}
}

// TestActivities_Pagination 分页语义
func TestActivities_Pagination(t *testing.T) {
	e := setup(t)
	// 造 5 笔充值
	for i := 0; i < 5; i++ {
		at := e.now.Add(-time.Duration(i) * time.Hour)
		e.insertLedger("recharge", int64(10_000_000*(i+1)), at, "充值")
	}
	items, total, err := e.st.Activities(context.Background(), e.pid, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total=%d 应=5", total)
	}
	if len(items) != 2 {
		t.Errorf("page 1 应 2 条，得 %d", len(items))
	}
	page2, _, _ := e.st.Activities(context.Background(), e.pid, 2, 2)
	if len(page2) != 2 {
		t.Errorf("page 2 应 2 条，得 %d", len(page2))
	}
	// page 1 / page 2 不重叠
	if items[0].ID == page2[0].ID {
		t.Error("page1[0] 和 page2[0] 撞了")
	}
}

// TestActivities_NoInternalTermsInResponse activity.summary 不含内部术语
func TestActivities_NoInternalTermsInResponse(t *testing.T) {
	e := setup(t)
	round := e.insertPullRound("91kiro", e.bus, 2, 40_000_000, e.now)
	pulled := e.now.Add(-2 * time.Hour)
	dead := e.now.Add(-1 * time.Hour)
	e.insertCredential("91kiro", "bus-"+e.bus, "dead", pulled, &dead, nil, e.bus, "", round)

	items, _, err := e.st.Activities(context.Background(), e.pid, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{
		"housepool", "record group", "provider", "adapter",
		"credential_ledger", "pending_purchase", "handed_off",
		"vendor_fee", "region_fee", "key_cost", "capability_fee",
	}
	for _, a := range items {
		s := strings.ToLower(a.Summary)
		for _, b := range banned {
			if strings.Contains(s, strings.ToLower(b)) {
				t.Errorf("activity summary 含内部术语 %q: %q", b, a.Summary)
			}
		}
	}
}
