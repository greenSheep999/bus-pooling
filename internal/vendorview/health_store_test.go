package vendorview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func healthDB(t *testing.T) *HealthStore {
	t.Helper()
	d := db.NewTestDB(t)
	return NewHealthStore(d.DB)
}

func rowFor(rows []PipelineHealthRow, vendor, pipeline string) (PipelineHealthRow, bool) {
	for _, r := range rows {
		if r.VendorID == vendor && r.Pipeline == pipeline {
			return r, true
		}
	}
	return PipelineHealthRow{}, false
}

func TestHealthStore_MarkOKAndReport(t *testing.T) {
	s := healthDB(t)
	ctx := context.Background()
	if err := s.Mark(ctx, "v-alpha", "probe", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Report(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := rowFor(rows, "v-alpha", "probe")
	if !ok {
		t.Fatal("应有 v-alpha/probe 行")
	}
	if r.LastOKAt.IsZero() {
		t.Error("MarkOK 后 last_ok_at 应有值")
	}
	if r.Stale(time.Now().UTC()) {
		t.Error("刚成功不该 stale")
	}
}

// COALESCE：成功不抹 last_err · 失败不抹 last_ok_at
func TestHealthStore_MarkKeepsBothSides(t *testing.T) {
	s := healthDB(t)
	ctx := context.Background()
	// 先失败：记 last_err · 无 last_ok
	_ = s.Mark(ctx, "v-beta", "time_decay", errors.New("401 token 过期"))
	rows, _ := s.Report(ctx)
	r, _ := rowFor(rows, "v-beta", "time_decay")
	if r.LastErr == "" || !r.LastOKAt.IsZero() {
		t.Fatalf("失败后应有 err 无 ok · %+v", r)
	}
	// 再成功：last_ok 有了 · last_err 仍保留（看得到"上次为啥挂过"）
	_ = s.Mark(ctx, "v-beta", "time_decay", nil)
	rows, _ = s.Report(ctx)
	r, _ = rowFor(rows, "v-beta", "time_decay")
	if r.LastOKAt.IsZero() {
		t.Error("成功后 last_ok_at 应有值")
	}
	if r.LastErr == "" {
		t.Error("成功不该抹掉 last_err（保留历史失败）")
	}
}

func TestHealthStore_Stale(t *testing.T) {
	now := time.Now().UTC()
	// 从没成功过 = stale
	never := PipelineHealthRow{Pipeline: "probe"}
	if !never.Stale(now) {
		t.Error("从没成功过应 stale")
	}
	// probe 阈值 3min · 5min 前成功 = stale
	old := PipelineHealthRow{Pipeline: "probe", LastOKAt: now.Add(-5 * time.Minute)}
	if !old.Stale(now) {
		t.Error("probe 5min 前应 stale")
	}
	// 1min 前成功 = 新鲜
	fresh := PipelineHealthRow{Pipeline: "probe", LastOKAt: now.Add(-1 * time.Minute)}
	if fresh.Stale(now) {
		t.Error("probe 1min 前应新鲜")
	}
	// backfill 家族阈值 15min · 10min 前成功 = 新鲜
	bf := PipelineHealthRow{Pipeline: "ledger", LastOKAt: now.Add(-10 * time.Minute)}
	if bf.Stale(now) {
		t.Error("ledger 10min 前应新鲜（阈值 15min）")
	}
}
