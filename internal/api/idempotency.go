package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// 幂等键格式：32 位十六进制（跟 vendor 侧 client_order_id 保持一致）
var hexKeyRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// idemStatus 是一次幂等查询的三种结果。
type idemStatus int

const (
	idemFresh    idemStatus = iota // 从未见过，新建了一行
	idemReplay                     // 见过，返回原响应
	idemConflict                   // key 相同但 body 不同 —— 客户端拼错了
)

type idemHit struct {
	status   idemStatus
	recordID string
	// 只在 idemReplay 时有值
	responseStatus int
	responseBody   []byte
}

// ensureIdempotencyRecord 幂等入口。写入并返回结果：
//   - fresh：占位行已建，业务继续跑
//   - replay：客户端在窗口内重放，直接返回原响应字节
//   - conflict：key 同但 body 不同，返回 idempotency_conflict
//
// **不加事务** —— UNIQUE(passenger_id, path, idempotency_key) 保证并发只有一个 INSERT 成功；
// 后到的走 SELECT 分支。
func ensureIdempotencyRecord(
	ctx context.Context,
	db *sql.DB,
	passengerID, method, path, key string,
	body []byte,
) (*idemHit, error) {
	if !hexKeyRe.MatchString(key) {
		return nil, ErrBadIdempotencyKey()
	}

	fingerprint := fingerprintOf(body)
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	res, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, passengerID, method, path, key, fingerprint, now)
	if err != nil {
		return nil, fmt.Errorf("api: 写 idempotency_record: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return &idemHit{status: idemFresh, recordID: id}, nil
	}

	// 已存在 —— 校验 body 是否一致
	var (
		existingID string
		existingFP string
		respStatus sql.NullInt64
		respBody   []byte
	)
	err = db.QueryRowContext(ctx, `
		SELECT id, request_fingerprint, response_status, response_body
		  FROM idempotency_record
		 WHERE passenger_id = ? AND path = ? AND idempotency_key = ?`,
		passengerID, path, key).Scan(&existingID, &existingFP, &respStatus, &respBody)
	if err != nil {
		return nil, fmt.Errorf("api: 读 idempotency_record: %w", err)
	}
	if existingFP != fingerprint {
		return &idemHit{status: idemConflict, recordID: existingID}, nil
	}

	// body 一致：如果已完成，返回原响应；未完成（同一请求还在跑），也返 replay
	// 让客户端等下一次 —— 这里保守用 202 提示。1a 简化：只处理已完成场景，
	// 未完成时返回 409 让客户端重试（极少发生 · 服务端处理速度快于两次调用间隔）
	if !respStatus.Valid {
		return &idemHit{status: idemConflict, recordID: existingID}, nil
	}
	return &idemHit{
		status:         idemReplay,
		recordID:       existingID,
		responseStatus: int(respStatus.Int64),
		responseBody:   respBody,
	}, nil
}

// ensureIdempotencyRecordTx tx 版·让 handler 把 idempotency INSERT 和后续业务 INSERT
// （比如 assign 的 pending_assignment initial）合到同一个 tx1 · 一起 commit。
//
// 修 P1（审计发现）：之前 ensureIdempotencyRecord 独立 commit·跟 pending_assignment
// initial 分两个 tx·中间崩溃会留个 orphan idempotency_record（response_status IS NULL）·
// 同 key 重试永远 hit in-flight conflict（89 行 `!respStatus.Valid` 分支）。
//
// 语义跟 ensureIdempotencyRecord 一致 · 只是走 *sql.Tx。fresh 场景由调用方在同 tx
// 内继续 INSERT 业务行 + Commit · 保证两个 INSERT 原子成对。
func ensureIdempotencyRecordTx(
	ctx context.Context,
	tx *sql.Tx,
	passengerID, method, path, key string,
	body []byte,
) (*idemHit, error) {
	if !hexKeyRe.MatchString(key) {
		return nil, ErrBadIdempotencyKey()
	}
	fingerprint := fingerprintOf(body)
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, passengerID, method, path, key, fingerprint, now)
	if err != nil {
		return nil, fmt.Errorf("api: 写 idempotency_record (tx): %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return &idemHit{status: idemFresh, recordID: id}, nil
	}
	var (
		existingID string
		existingFP string
		respStatus sql.NullInt64
		respBody   []byte
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, request_fingerprint, response_status, response_body
		  FROM idempotency_record
		 WHERE passenger_id = ? AND path = ? AND idempotency_key = ?`,
		passengerID, path, key).Scan(&existingID, &existingFP, &respStatus, &respBody)
	if err != nil {
		return nil, fmt.Errorf("api: 读 idempotency_record (tx): %w", err)
	}
	if existingFP != fingerprint {
		return &idemHit{status: idemConflict, recordID: existingID}, nil
	}
	if !respStatus.Valid {
		return &idemHit{status: idemConflict, recordID: existingID}, nil
	}
	return &idemHit{
		status:         idemReplay,
		recordID:       existingID,
		responseStatus: int(respStatus.Int64),
		responseBody:   respBody,
	}, nil
}

// checkIdempotencyReplay tx1 之前的**只读**幂等预检 · SELECT 现有已完成记录。
//
// 用途：handler 想在开写事务之前先判"这是不是重放"·避免占用 SQLite writer 锁做归属校验
// （tx1 拿 IMMEDIATE 写锁·其中再 SELECT 其他表可能死锁 · 归属校验放外面用只读连接更稳）。
//
// 返回：
//   - ok=true：命中已完成幂等记录·respBody + respStatus 可直接原样返回给客户端
//   - ok=false：未命中或未完成 · 调用方应继续走 tx1 走 ensureIdempotencyRecordTx
//
// **不做 conflict 判断** —— body 冲突留到 tx1 里的 ensureIdempotencyRecordTx 处理·
// 因为 conflict 会返 409 · 而 handler 想在 fresh 时也做归属校验·预检只关心 fast-path。
func checkIdempotencyReplay(ctx context.Context, db *sql.DB, passengerID, path, key string, body []byte) ([]byte, int, bool, error) {
	if !hexKeyRe.MatchString(key) {
		return nil, 0, false, ErrBadIdempotencyKey()
	}
	fp := fingerprintOf(body)
	var (
		existingFP string
		respStatus sql.NullInt64
		respBody   []byte
	)
	err := db.QueryRowContext(ctx, `
		SELECT request_fingerprint, response_status, response_body
		  FROM idempotency_record
		 WHERE passenger_id = ? AND path = ? AND idempotency_key = ?`,
		passengerID, path, key).Scan(&existingFP, &respStatus, &respBody)
	if err == sql.ErrNoRows {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("api: 幂等预检 SELECT: %w", err)
	}
	if !respStatus.Valid {
		// 记录存在但未完成 · fresh 流程还在跑（并发或崩溃 orphan）
		// 让 tx1 里的 ensureIdempotencyRecordTx 走 conflict 分支
		return nil, 0, false, nil
	}
	if existingFP != fp {
		// body 不同 · 走 tx1 里报 conflict
		return nil, 0, false, nil
	}
	return respBody, int(respStatus.Int64), true, nil
}

// saveIdempotentResponse 落一次完成响应的字节，供后续重放。
func saveIdempotentResponse(ctx context.Context, db *sql.DB, recordID string, status int, body []byte) error {
	_, err := db.ExecContext(ctx, `
		UPDATE idempotency_record
		   SET response_status = ?, response_body = ?, first_completed_at = ?
		 WHERE id = ? AND first_completed_at IS NULL`,
		status, body, time.Now().UTC().Format(time.RFC3339Nano), recordID)
	if err != nil {
		return fmt.Errorf("api: 保存幂等响应: %w", err)
	}
	return nil
}

// saveIdempotentResponseTx 事务版·让 handler 把幂等响应写跟业务写合成一个 tx。
// 语义跟 saveIdempotentResponse 一致·只是走 *sql.Tx。
func saveIdempotentResponseTx(ctx context.Context, tx *sql.Tx, recordID string, status int, body []byte) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE idempotency_record
		   SET response_status = ?, response_body = ?, first_completed_at = ?
		 WHERE id = ? AND first_completed_at IS NULL`,
		status, body, time.Now().UTC().Format(time.RFC3339Nano), recordID)
	if err != nil {
		return fmt.Errorf("api: 保存幂等响应 (tx): %w", err)
	}
	return nil
}

// fingerprintOf 对请求体做规范化指纹：解出来重新按 key 排序序列化，避免
// 客户端字段顺序不同或多余空格导致误判 conflict。
func fingerprintOf(body []byte) string {
	if len(body) == 0 {
		h := sha256.Sum256(nil)
		return hex.EncodeToString(h[:])
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		// 不是合法 JSON 就按原始字节算 —— 上游会先 decodeJSON 拦到 bad_json
		h := sha256.Sum256(body)
		return hex.EncodeToString(h[:])
	}
	canon, err := canonicalMarshal(v)
	if err != nil {
		h := sha256.Sum256(body)
		return hex.EncodeToString(h[:])
	}
	h := sha256.Sum256(canon)
	return hex.EncodeToString(h[:])
}

// canonicalMarshal 递归排序 map 后 marshal，让 {"a":1,"b":2} 和 {"b":2,"a":1} 同指纹。
func canonicalMarshal(v any) ([]byte, error) {
	return json.Marshal(canonicalize(v))
}

func canonicalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		// json.Marshal 对 map 已按 key 排序，直接递归 canonicalize 值即可
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = canonicalize(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = canonicalize(vv)
		}
		return out
	default:
		return v
	}
}

// ErrBadIdempotencyKey 幂等键格式错误（05-api-contract 错误码表）。
func ErrBadIdempotencyKey() *Fail {
	return newFail(http.StatusBadRequest, CodeBadIdempotencyKey,
		"幂等键格式不对（要 32 位十六进制）")
}

// ErrIdempotencyConflict 同 key 但请求体不一致。
func ErrIdempotencyConflict() *Fail {
	return newFail(http.StatusConflict, CodeIdempotencyConflict,
		"这个幂等键对应的请求内容跟上一次不一样")
}
