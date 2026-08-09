package decider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// 并发拉号限流（decisions §8.35 #18）。
//
// **目标**：避免并发打爆上游 + 尽量拉到号。
//
// **为什么按库里的在飞行数量算·不用内存 semaphore**：
//   - 内存计数重启就清零 —— 崩溃恢复时 janitor 还在处理旧单·内存却以为没人在飞
//   - 多进程 / 未来多副本时内存计数各算各的·等于没限
//   - `pending_purchase` 本来就记着"谁在飞"（status ∈ reserved/purchasing/purchased/imported）·
//     数它就是唯一事实来源
//
// 代价是每次拉号多一次 COUNT 查询（有 (status, updated_at) 索引 · 单机可忽略）。

// Limits 拉号并发上限 · 0 = 不限。
type Limits struct {
	// MaxConcurrentPerVendor 同一 vendor 同时最多几个在飞
	MaxConcurrentPerVendor int
	// MaxConcurrentPerPassenger 同一乘客同时最多几个在飞
	MaxConcurrentPerPassenger int
	// MinCount / MaxCount 单次拉号数量允许区间 · 0 = 不校验
	MinCount int
	MaxCount int
}

var (
	// ErrVendorBusy 这个 vendor 在飞的拉号已达上限 · 让客户端稍后重试
	// （不是失败·是"现在别挤"· api 层映射成 429）
	ErrVendorBusy = errors.New("decider: 该上游正忙")
	// ErrPassengerBusy 这个乘客自己的并发已达上限
	ErrPassengerBusy = errors.New("decider: 你有拉号正在进行中")
	// ErrCountOutOfRange 请求数量超出系统允许区间
	ErrCountOutOfRange = errors.New("decider: 拉号数量超出允许范围")
)

// inFlightStatuses 算"在飞"的状态。
//
// 这几个状态意味着**这一轮还没落地**：钱冻着 / vendor 请求可能已发出 / 号可能已买到但没入账。
// completed / cancelled_* / need_* 都是终态或人工态·不算在飞。
var inFlightStatuses = []Status{
	StatusReserved, StatusPurchasing, StatusPurchased, StatusImported,
}

// countRangeErr 带上区间数字的 ErrCountOutOfRange · 让 api 层能取出来拼人话
// （不直接透 err.Error() —— 那串带内部包名前缀 · CLAUDE.md §0.1）。
type countRangeErr struct {
	min, max int
}

func (e countRangeErr) Error() string {
	return fmt.Sprintf("decider: 拉号数量超出允许范围 [%d, %d]", e.min, e.max)
}
func (e countRangeErr) Unwrap() error { return ErrCountOutOfRange }

// CountRangeOf 从 ErrCountOutOfRange 里取出允许区间 · api 层拼对外 message 用。
// 第三个返回值 false = 这不是区间错误（或没带数字）。
func CountRangeOf(err error) (min, max int, ok bool) {
	var e countRangeErr
	if errors.As(err, &e) {
		return e.min, e.max, true
	}
	return 0, 0, false
}

// checkCountRange 校验请求数量落在系统区间内。
//
// **超区间直接拒**·不静默截断 —— 用户要 100 个给他 50 个还不告诉他，
// 他会以为系统坏了（§8.35 #18）。
func (l Limits) checkCountRange(count int) error {
	if l.MinCount > 0 && count < l.MinCount {
		return countRangeErr{min: l.MinCount, max: l.MaxCount}
	}
	if l.MaxCount > 0 && count > l.MaxCount {
		return countRangeErr{min: l.MinCount, max: l.MaxCount}
	}
	return nil
}

// checkConcurrency 查 vendor 和乘客维度的在飞数量。
//
// 在 **创建 pending_purchase 之前**调 —— 那时候这一轮还没进库·数出来的是"别人的"。
// 有并发窗口（两个请求同时 COUNT 都通过）·但：
//   - 这是限流不是资金安全·多放进去一两个不造成损失
//   - 真要严格得加库级锁·代价（写放大 + 锁竞争）不值得
func (l Limits) checkConcurrency(
	ctx context.Context, db *sql.DB, passengerID, vendorID string,
) error {
	if l.MaxConcurrentPerPassenger > 0 {
		n, err := countInFlight(ctx, db, "passenger_id", passengerID)
		if err != nil {
			return err
		}
		if n >= l.MaxConcurrentPerPassenger {
			return fmt.Errorf("%w（同时最多 %d 个）",
				ErrPassengerBusy, l.MaxConcurrentPerPassenger)
		}
	}
	if l.MaxConcurrentPerVendor > 0 {
		n, err := countInFlight(ctx, db, "vendor_id", vendorID)
		if err != nil {
			return err
		}
		if n >= l.MaxConcurrentPerVendor {
			return fmt.Errorf("%w（同时最多 %d 个）",
				ErrVendorBusy, l.MaxConcurrentPerVendor)
		}
	}
	return nil
}

// countInFlight 数某个维度上在飞的拉号数。
//
// col 只允许 "passenger_id" / "vendor_id" —— 调用方写死·不接受外部输入
// （拼进 SQL 的列名不能来自用户）。
func countInFlight(ctx context.Context, db *sql.DB, col, val string) (int, error) {
	if col != "passenger_id" && col != "vendor_id" {
		return 0, fmt.Errorf("decider: 非法的限流维度 %q", col)
	}
	// 手拼 IN 占位符（statuses 是包内常量·长度固定）
	q := `SELECT COUNT(1) FROM pending_purchase WHERE ` + col + ` = ? AND status IN (?, ?, ?, ?)`
	args := []any{val}
	for _, s := range inFlightStatuses {
		args = append(args, string(s))
	}
	var n int
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("decider: 数在飞拉号: %w", err)
	}
	return n, nil
}
