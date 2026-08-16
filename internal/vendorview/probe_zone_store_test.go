package vendorview

// v4.4 · InsertWebhook + LatestZoneCredits 优先级链回归

import (
	"context"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func setupProbeZoneDB(t *testing.T) *ProbeZoneStore {
	t.Helper()
	d := db.NewTestDB(t)
	return NewProbeZoneStore(d.DB)
}

func TestInsertWebhook_LandsRow(t *testing.T) {
	s := setupProbeZoneDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := s.InsertWebhook(ctx, "kirotest", "us", 100_000_000, 50, now)
	if err != nil {
		t.Fatal(err)
	}

	// 直读 · 验落库
	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM vendor_probe_zone WHERE source='webhook'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("应落 1 行 · 得 %d", count)
	}
}

func TestInsertWebhook_NoPriceNoAvail_Skipped(t *testing.T) {
	s := setupProbeZoneDB(t)
	err := s.InsertWebhook(context.Background(), "kirotest", "us", 0, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM vendor_probe_zone`).Scan(&count)
	if count != 0 {
		t.Errorf("空数据不该落 · 得 %d 行", count)
	}
}

// v4.4 · vendor_self 存在时 webhook 不该被拿到（优先级链尊重）
func TestLatestZoneCredits_VendorSelfWinsOverWebhook(t *testing.T) {
	s := setupProbeZoneDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 先落 webhook 100
	_ = s.InsertWebhook(ctx, "kirotest", "us", 100_000_000, 0, now.Add(-1*time.Minute))
	// 再落 vendor_self 90（更旧但优先级更高）
	_ = s.InsertBatch(ctx, []ProbeZoneSample{{
		VendorID:       "kirotest",
		ProbedAt:       now.Add(-5 * time.Minute),
		Zone:           "us",
		OurUnitCredits: 90_000_000,
		Source:         "vendor_self",
	}})

	c, _, ok := s.LatestZoneCredits(ctx, "kirotest", providers.Zone("us"))
	if !ok {
		t.Fatal("应有价")
	}
	if c != 90_000_000 {
		t.Errorf("vendor_self 90 应赢 webhook 100 · 得 %d", c)
	}
}

// v4.4 · vendor_self 不存在时 · webhook 应赢过 xi8
func TestLatestZoneCredits_WebhookBeatsXi8(t *testing.T) {
	s := setupProbeZoneDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 落 xi8 200
	_ = s.InsertBatch(ctx, []ProbeZoneSample{{
		VendorID: "kirotest", ProbedAt: now.Add(-2 * time.Minute), Zone: "us",
		OurUnitCredits: 200_000_000, Source: "xi8",
	}})
	// 落 webhook 100（更旧但优先）
	_ = s.InsertWebhook(ctx, "kirotest", "us", 100_000_000, 0, now.Add(-1*time.Minute))

	c, _, ok := s.LatestZoneCredits(ctx, "kirotest", providers.Zone("us"))
	if !ok {
		t.Fatal("应有价")
	}
	if c != 100_000_000 {
		t.Errorf("webhook 应赢 xi8 · 得 %d", c)
	}
}

// nil-safe · store == nil 也不 panic
func TestProbeZoneStore_Nil(t *testing.T) {
	var s *ProbeZoneStore
	err := s.InsertWebhook(context.Background(), "x", "us", 1, 1, time.Now())
	if err != nil {
		t.Errorf("nil store 应静默返 nil · 得 %v", err)
	}
	_, _, ok := s.LatestZoneCredits(context.Background(), "x", "us")
	if ok {
		t.Error("nil store 不该有价")
	}
}
