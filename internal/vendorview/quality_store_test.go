package vendorview

// **回归哨兵 · Task 65 · 2026-08-14**
//
// AutoPick 打分公式里 aliveRate 老代码恒 50 · 相当于纯价格排序。
// 修：从 vendor_key 表按 30d 窗口聚合真存活率 + 平均寿命。
// 数据不足（死号 < 3）自动降级返 (nil, false) · 让 AutoPick 用 50 兜底。

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setupQualityDB(t *testing.T) *sql.DB {
	t.Helper()
	d := db.NewTestDB(t)
	return d.DB
}

func insertKey(t *testing.T, sqldb *sql.DB, vendorID, keyID, status string, createdAt, deadAt time.Time) {
	t.Helper()
	var deadStr any
	if !deadAt.IsZero() {
		deadStr = deadAt.UTC().Format(time.RFC3339)
	}
	_, err := sqldb.Exec(`
		INSERT INTO vendor_key
		  (vendor_id, vendor_key_id, status, created_at, dead_at, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		vendorID, keyID, status,
		createdAt.UTC().Format(time.RFC3339), deadStr,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertKey: %v", err)
	}
}

// 数据不足（死号 < 3）· 返 (nil, false)
func TestQualityStore_InsufficientData(t *testing.T) {
	sqldb := setupQualityDB(t)
	q := NewQualityStore(sqldb)

	// 只 1 个死号 + 5 个 alive
	now := time.Now()
	insertKey(t, sqldb, "kiro91", "k1", "dead", now.Add(-2*time.Hour), now.Add(-1*time.Hour))
	for i := 0; i < 5; i++ {
		insertKey(t, sqldb, "kiro91", "k"+string(rune('a'+i)), "active", now.Add(-1*time.Hour), time.Time{})
	}

	stats, ok, err := q.Get(context.Background(), "kiro91")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("死号只 1 个 · 应返 false · 得 %+v", stats)
	}
}

// 死号 >= 3 · 返真实聚合
func TestQualityStore_Aggregates(t *testing.T) {
	sqldb := setupQualityDB(t)
	q := NewQualityStore(sqldb)

	now := time.Now()
	// 3 个死号 · 寿命分别 60/120/180 秒 · 平均 120
	insertKey(t, sqldb, "kiroceo", "d1", "dead", now.Add(-2*time.Hour), now.Add(-2*time.Hour).Add(60*time.Second))
	insertKey(t, sqldb, "kiroceo", "d2", "dead", now.Add(-2*time.Hour), now.Add(-2*time.Hour).Add(120*time.Second))
	insertKey(t, sqldb, "kiroceo", "d3", "dead", now.Add(-2*time.Hour), now.Add(-2*time.Hour).Add(180*time.Second))
	// 7 个 alive
	for i := 0; i < 7; i++ {
		insertKey(t, sqldb, "kiroceo", "a"+string(rune('a'+i)), "active", now.Add(-1*time.Hour), time.Time{})
	}
	// 存活率 = 7 / 10 = 70

	stats, ok, err := q.Get(context.Background(), "kiroceo")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("死号 3 个 · 应返 true")
	}
	if stats.SampleSize != 3 {
		t.Errorf("SampleSize = %d · want 3", stats.SampleSize)
	}
	// 平均寿命 120 秒（60+120+180)/3 · SQLite julianday 是 float · 转 int 会 ±1 抖动
	if stats.AvgLifespanSeconds < 119 || stats.AvgLifespanSeconds > 121 {
		t.Errorf("AvgLifespanSeconds = %d · want ~120（±1）", stats.AvgLifespanSeconds)
	}
	if stats.AliveRate30d != 70 {
		t.Errorf("AliveRate30d = %d · want 70", stats.AliveRate30d)
	}
}

// 30d 之外的号不计入
func TestQualityStore_OnlyLast30d(t *testing.T) {
	sqldb := setupQualityDB(t)
	q := NewQualityStore(sqldb)

	old := time.Now().Add(-40 * 24 * time.Hour) // 40 天前 · 不算
	recent := time.Now().Add(-2 * time.Hour)

	// 3 个老死号 · 都在窗口外
	insertKey(t, sqldb, "kirooo", "old1", "dead", old, old.Add(60*time.Second))
	insertKey(t, sqldb, "kirooo", "old2", "dead", old, old.Add(60*time.Second))
	insertKey(t, sqldb, "kirooo", "old3", "dead", old, old.Add(60*time.Second))

	// 只有 1 个近死号 —— 不满 3 · 应返 false
	insertKey(t, sqldb, "kirooo", "new1", "dead", recent, recent.Add(60*time.Second))

	_, ok, err := q.Get(context.Background(), "kirooo")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("窗口外的死号不该计入 · 应返 false")
	}
}

// nil store 安全
func TestQualityStore_Nil(t *testing.T) {
	var q *QualityStore
	if _, ok, _ := q.Get(context.Background(), "kiro91"); ok {
		t.Error("nil store 应返 false")
	}
}
