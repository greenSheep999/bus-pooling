package bus_test

// autorefill_fk_smoke_test · FK 787 修复的最小回归 · 见 sprint-1e/decisions §refill-fk
//
// **背景**: scheduler.doPull 传给 puller 的 IdempotencyRecordID 曾是 32-hex 但从未落
// idempotency_record 表 · 下游 pending_purchase.idempotency_record_id FK 悬空 ·
// 生产每 5min 撞 FK 787 崩。修法: autoRefillBridge 在调 orch.Pull 前先落 record。
//
// **本 test 验证契约**: bus.Scheduler.ScanOnce 触发一次补车后 · idempotency_record
// 表里必有对应 (path='/internal/auto-refill', idempotency_key=32-hex) 行。

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/db"
)

// fkFixPuller · 简单的 mock · 记 IdempotencyRecordID 传进来是啥 · 不真调 decider
type fkFixPuller struct {
	db          *sql.DB
	seenRecords []string // 每次 Refill 收到的 IdempotencyRecordID
}

func (p *fkFixPuller) Refill(_ context.Context, req bus.AutoRefillRequest) error {
	p.seenRecords = append(p.seenRecords, req.IdempotencyRecordID)
	return nil
}

// 本 test 直接跑 bus.Scheduler + fkFixPuller · 但**注意**: autoRefillBridge 只在
// cmd/bus-pooling/main.go 装配 · 走真链路测得跨 cmd 层。我们这里模拟"puller 收到
// req.IdempotencyRecordID 后 · 会先落 idempotency_record"的契约: 让 puller 自己落。
func (p *fkFixPuller) EnsureRecord(ctx context.Context, req bus.AutoRefillRequest) error {
	// 复刻 autoRefillBridge.ensureRefillIdemRecord 的核心逻辑
	// 若这里 idem 不能落表 · 就是 fix 语义有洞
	_, err := p.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, 'POST', '/internal/auto-refill', ?, ?, ?)`,
		"uuid-"+req.IdempotencyRecordID, req.InitiatorPassengerID,
		req.IdempotencyRecordID, req.IdempotencyRecordID,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// TestAutoRefillIdemRecordFK · 检查 scheduler 触发 ScanOnce 后 · idem 是 32-hex ·
// 交给桥后**能成功落 idempotency_record** · 不会因为 request_fingerprint NOT NULL 撞库。
func TestAutoRefillIdemRecordFK(t *testing.T) {
	// 建库 · 应用 migration
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "fk.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlite := database.DB

	// seed · passenger + bus + auto_refill_enabled=1
	_, err = sqlite.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'u1', 'a@b.c', 'x', ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed passenger: %v", err)
	}
	_, err = sqlite.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES ('b1', 'test', 'single', 'p1', 'active', ?, 1, 3)`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed bus: %v", err)
	}

	// puller 模拟桥 · 每次 Refill 先落 idempotency_record 再 no-op
	puller := &fkFixPuller{db: sqlite}
	wrapped := &fkPullerWithRecord{puller: puller}

	sched := bus.NewScheduler(sqlite, wrapped, 5*time.Minute, nil)
	sched.SetDecider(fkFixDecider{})

	// 一轮扫 · 无论 refill 是否命中候选 · 只要触发 · IdempotencyRecordID 必落
	// (即使 alive 为 0 也算命中 · watermark=3 · min_count 默认 · candidate exists)
	touched, _ := sched.ScanOnce(ctx)
	if touched == 0 {
		t.Fatal("scheduler 未扫到候选 · seed 数据错")
	}

	// 断言 · idempotency_record 有 auto-refill 行
	var count int
	err = sqlite.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM idempotency_record WHERE path='/internal/auto-refill'`).Scan(&count)
	if err != nil {
		t.Fatalf("query idem: %v", err)
	}
	if count == 0 {
		t.Fatal("scheduler ScanOnce 未触发落 idempotency_record · FK 修复失效")
	}
	if len(puller.seenRecords) == 0 {
		t.Fatal("puller 未收到 idem · 契约断")
	}
	// 且 idem 是 32-hex(hex 编码 16 字节)
	seen := puller.seenRecords[0]
	if len(seen) != 32 {
		t.Fatalf("idem 长度 want=32 got=%d val=%q", len(seen), seen)
	}
}

// fkPullerWithRecord · 包装 puller · 每次 Refill 前先落 record(模拟 autoRefillBridge)
type fkPullerWithRecord struct {
	puller *fkFixPuller
}

func (w *fkPullerWithRecord) Refill(ctx context.Context, req bus.AutoRefillRequest) error {
	if err := w.puller.EnsureRecord(ctx, req); err != nil {
		return errors.New("fkPullerWithRecord: 落 record: " + err.Error())
	}
	return w.puller.Refill(ctx, req)
}

// fkFixDecider · 极简 · 直接返 ActionPull · count=1
type fkFixDecider struct{}

func (fkFixDecider) Decide(_ context.Context, _ string, cand bus.SchedulerCandidate) bus.SchedulerVerdict {
	return bus.SchedulerVerdict{
		Action:    bus.ActionPull,
		PullCount: 1,
	}
}
