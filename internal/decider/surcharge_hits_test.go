package decider

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// P1 · insertSurchargeHits · retail/capability/adhoc 三 kind 共用 capabilityFee 桶时·
// 每条 hit 分摊桶总额的 rate_bp 占比 · SUM(amount) 应 == capabilityFee(不是 3×)。
//
// 审计发现:老代码 kindBp["retail"] 只统计 retail 的 rate_bp · 摊时按 retail_bp/retail_bp
// 分摊 → 每条摊了整个 capabilityFee · 3 kind 都命中时 SUM = 3×capabilityFee。

func setupPullRoundSurchargeTable(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE pull_round_surcharge (
			pull_round_id TEXT NOT NULL,
			rule_id       TEXT NOT NULL,
			rule_name     TEXT NOT NULL,
			rule_kind     TEXT NOT NULL,
			rate_bp       INTEGER NOT NULL,
			amount        INTEGER NOT NULL,
			created_at    TEXT NOT NULL,
			PRIMARY KEY (pull_round_id, rule_id)
		)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInsertSurchargeHits_CapabilityBucketShared(t *testing.T) {
	db := setupPullRoundSurchargeTable(t)
	defer db.Close()

	// 场景:capabilityFee=300(3 kind 合并的实收) · 三条 hit 分别 retail 100 / capability 200 / adhoc 100
	// bucket 总 rate_bp = 400 · 每条摊 amount = 300 * (rate/400)
	// retail:     300 * 100/400 = 75
	// capability: 300 * 200/400 = 150
	// adhoc:      300 * 100/400 = 75
	// SUM = 300 ✅(不是 3×300=900)
	bd := Breakdown{
		capabilityFee: 300_000_000,
	}
	hits := []SurchargeHit{
		{RuleID: "r1", RuleName: "retail", Kind: "retail", RateBp: 100},
		{RuleID: "r2", RuleName: "capability", Kind: "capability", RateBp: 200},
		{RuleID: "r3", RuleName: "adhoc", Kind: "adhoc", RateBp: 100},
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = insertSurchargeHits(context.Background(), tx, "round-1", hits, bd, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// SUM 应 == capabilityFee(300M) · 不是 3×(900M)
	var sum int64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM pull_round_surcharge WHERE pull_round_id = 'round-1'`,
	).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 300_000_000 {
		t.Errorf("SUM(amount) = %d · want 300_000_000(桶总额 · 不是 3× 也不是漏计)", sum)
	}

	// 分项断言:retail=75M · capability=150M · adhoc=75M
	rows, err := db.Query(`SELECT rule_kind, amount FROM pull_round_surcharge WHERE pull_round_id = 'round-1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var k string
		var a int64
		if err := rows.Scan(&k, &a); err != nil {
			t.Fatal(err)
		}
		got[k] = a
	}
	want := map[string]int64{"retail": 75_000_000, "capability": 150_000_000, "adhoc": 75_000_000}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("kind=%s amount=%d · want %d", k, got[k], w)
		}
	}
}

// TestInsertSurchargeHits_SoloVendorBucket · 独占桶行为不受影响
func TestInsertSurchargeHits_SoloVendorBucket(t *testing.T) {
	db := setupPullRoundSurchargeTable(t)
	defer db.Close()

	bd := Breakdown{vendorFee: 100_000_000}
	hits := []SurchargeHit{
		{RuleID: "v1", RuleName: "vendor-fee", Kind: "vendor", RateBp: 500},
	}

	tx, _ := db.Begin()
	_ = insertSurchargeHits(context.Background(), tx, "round-2", hits, bd, time.Now())
	tx.Commit()

	var amount int64
	db.QueryRow(`SELECT amount FROM pull_round_surcharge WHERE pull_round_id = 'round-2'`).Scan(&amount)
	// 独占桶 · 只一条 hit · 摊整个桶 · amount = 100M
	if amount != 100_000_000 {
		t.Errorf("单条 hit 独占桶 · amount = %d · want 100_000_000", amount)
	}
}
