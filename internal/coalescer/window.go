package coalescer

// window · 集单窗口（v2 · 2026-08-15）
//
// **同步版本**（最简可用）：不做异步意图池 + 前端大改。收到 Anon Intent 时：
//   1. 按 (bus_id, zone, vendor_id) 找/建一个 window
//   2. 加入这个 window · 若是**首个**加入者 · 启动 200ms 计时器
//   3. **阻塞**等 window 关闭（超时或达到 max_batch）
//   4. window 关闭后 · **首个加入者** 拿到 batch 触发 decider.Pull · 其他成员等结果
//   5. 结果分发给每个成员（各自看到自己那份 count 分摊）
//
// **为什么同步**：API 层不用改成"异步 intent_id" · 前端不改。代价是每个成员的 HTTP 请求
// 多阻塞最多 200ms · 但省 vendor 侧 N-1 次 API 调用 · 抢货速度也更快（一次比 N 次快）。
//
// **不做的**（留 v3+）：跨进程集单 · 意图池表 · 窗口调度独立 goroutine + 后台 fire。
// 单进程内存足够撑住 v1 期用户量。多副本部署时会退化成"每副本各自合流"（可接受 ·
// 各副本 window 独立 · 不会重复扣钱）。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// WindowConfig · 集单窗口参数
type WindowConfig struct {
	// Duration 窗口时长 · 默认 200ms（够多人短窗内加入 · 又不明显拖慢单人体验）
	Duration time.Duration
	// MaxBatch 单窗口最多合并意图数 · 默认 8（避免一次拉太多 vendor 侧拒）
	// 达到后立即关窗（不等 Duration）
	MaxBatch int
}

// windowKey · 集单粒度键 · 只合并 (bus 同 + zone 同 + vendor 同)
type windowKey struct {
	BusID    string
	Zone     string
	VendorID string
}

// windowEntry · 一个正在合流的窗口
type windowEntry struct {
	key       windowKey
	openedAt  time.Time
	intents   []Intent
	// closed 用 channel 广播 · 关窗后所有等待者被叫醒
	closed chan struct{}
	// 关窗后 · Executor 把结果塞进 result / err · 广播给所有等待者
	result *BatchResult
	err    error
}

// BatchResult · Executor 执行合流后的结果 · 每个参与者靠 PassengerID 拿自己那份
type BatchResult struct {
	// Batch 传给 Executor 的原始 BatchIntent（分摊时参考）
	Batch *BatchIntent
	// PullResult · decider.Pull 返回的结果 · 逐成员分账用
	// 这里用 any · 避免 coalescer 反向 import decider（否则包循环）·
	// 上层类型断言取
	PullResult any
}

// Executor · 集单执行器（避免 coalescer → decider 硬依赖）·
// 装配层实现：接 decider.Pull · 传 BatchIntent 拉一次 · 返 PullResult。
type Executor interface {
	Execute(ctx context.Context, batch *BatchIntent) (any, error)
}

// Window · 集单窗口管理器 · 单例（每进程一个）
type Window struct {
	cfg      WindowConfig
	executor Executor

	mu   sync.Mutex
	live map[windowKey]*windowEntry
}

// NewWindow · 装配。executor nil 时 Add 报错。
func NewWindow(cfg WindowConfig, executor Executor) *Window {
	if cfg.Duration <= 0 {
		cfg.Duration = 200 * time.Millisecond
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 8
	}
	return &Window{
		cfg:      cfg,
		executor: executor,
		live:     make(map[windowKey]*windowEntry),
	}
}

// ErrNoExecutor · 未装配 executor 就调 Add · 报错让上层降级到 Single
var ErrNoExecutor = errors.New("coalescer: 未装配 executor · 走 Single 兜底")

// Add · 主流程 · 阻塞至窗口关闭 · 返自己那份结果。
//
// **阻塞语义**：调用者线程在这函数里挂 · 直到 (1) 窗口自然到时 · (2) 窗口被凑满
// (3) ctx 超时 (4) executor 执行错。
//
// 返 (BatchResult · error)：BatchResult 是共享的（所有成员看到同一份）· 上层按
// PassengerID 从 Batch.Participants 找位置拿分摊。
func (w *Window) Add(ctx context.Context, in Intent) (*BatchResult, error) {
	if w == nil || w.executor == nil {
		return nil, ErrNoExecutor
	}
	if in.BusID == "" {
		// record group（无 bus）不合流 —— 单独拉号本来就一人一单 · 没合流对象
		return nil, ErrNoExecutor
	}

	key := windowKey{
		BusID:    in.BusID,
		Zone:     string(in.Zone),
		VendorID: string(in.VendorID),
	}

	w.mu.Lock()
	entry, exists := w.live[key]
	if !exists {
		entry = &windowEntry{
			key:      key,
			openedAt: time.Now(),
			intents:  []Intent{in},
			closed:   make(chan struct{}),
		}
		w.live[key] = entry
		w.mu.Unlock()

		// 首个加入者 · 起计时器 → 到时关窗
		// 或等 MaxBatch 满时立刻关（在 Add 后半段判断）
		go w.timerClose(ctx, entry)
	} else {
		// 已有窗口 · 加入
		entry.intents = append(entry.intents, in)
		full := len(entry.intents) >= w.cfg.MaxBatch
		w.mu.Unlock()
		if full {
			w.tryClose(entry) // 达上限提前关
		}
	}

	// 阻塞等关窗
	select {
	case <-entry.closed:
		return entry.result, entry.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// timerClose · 起计时器 · 到期关窗
func (w *Window) timerClose(ctx context.Context, entry *windowEntry) {
	timer := time.NewTimer(w.cfg.Duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-entry.closed:
		return // 已被 tryClose 提前关（MaxBatch 满）
	case <-ctx.Done():
	}
	w.tryClose(entry)
}

// tryClose · 关窗 + 执行 batch · 幂等（多个触发路径抢关一次）
func (w *Window) tryClose(entry *windowEntry) {
	w.mu.Lock()
	// 幂等：channel 已关 · 直接返
	select {
	case <-entry.closed:
		w.mu.Unlock()
		return
	default:
	}
	// 从 live 摘除 · 后来者进的是新窗口
	if cur, ok := w.live[entry.key]; ok && cur == entry {
		delete(w.live, entry.key)
	}
	intents := entry.intents // snapshot（此刻起没人再往里加）
	w.mu.Unlock()

	// 组装 batch
	batch := buildBatch(intents)
	// 执行 decider.Pull（同步 · 阻塞所有等待者直到完成）
	result, err := w.executor.Execute(context.Background(), batch)

	entry.result = &BatchResult{Batch: batch, PullResult: result}
	entry.err = err
	close(entry.closed) // 广播给所有等待者
}

// buildBatch · Intent 列表 → BatchIntent（合流后的批量意图）
func buildBatch(intents []Intent) *BatchIntent {
	if len(intents) == 0 {
		return nil
	}
	first := intents[0]
	total := 0
	pids := make([]string, 0, len(intents))
	idemIDs := make([]string, 0, len(intents))
	for _, in := range intents {
		total += in.Count
		pids = append(pids, in.PassengerID)
		idemIDs = append(idemIDs, in.IdempotencyRecordID)
	}
	return &BatchIntent{
		BusID:                first.BusID,
		Participants:         pids,
		CountTotal:           total,
		Zone:                 first.Zone,
		VendorID:             first.VendorID,
		IdempotencyRecordIDs: idemIDs,
	}
}

// Anon · 匿名撮合合流 · v2 真做（同步窗口）
//
// **不再返 ErrNotImplemented** —— 走真集单：
//   · 未装配 executor / record group（无 bus）→ 走 Single 兜底
//   · 有 bus · 有 executor → Add 到 window 阻塞等关窗
//
// **注意**：Window 变量在上层装配 · Anon 只是 free-function 入口 · 需要一个包级 Window。
// 但为了保持 API 兼容（老代码调 Anon 期望 (context, Intent) → BatchIntent · 无 Window 参数）·
// 我方引入包级默认 Window · 上层调 SetDefaultWindow 装配。
//
// 如果测试或其他场景需要多实例 · 直接用 (*Window).Add 就好。

var (
	defaultWindowMu sync.RWMutex
	defaultWindow   *Window
)

// SetDefaultWindow · 装配包级 default window · main.go 里调一次
func SetDefaultWindow(w *Window) {
	defaultWindowMu.Lock()
	defer defaultWindowMu.Unlock()
	defaultWindow = w
}

func getDefaultWindow() *Window {
	defaultWindowMu.RLock()
	defer defaultWindowMu.RUnlock()
	return defaultWindow
}

// AnonV2 · Anon 的真实现版本（v2）· 上层调用点在 api/bus.go
//
// 为什么不直接改 Anon 签名：Anon 是老接口 · 保留报 ErrNotImplemented（兼容不改动上层）·
// v2 上层直接调 AnonV2·或 Add 到 Window·或降级 Single。
//
// 但为了让 v2 的调用更顺 · AnonV2 封装了三条路径：
//   · 无 executor → Single 兜底（等价老行为 · 各自跑）
//   · 有 executor · record group → Single 兜底（无合流对象）
//   · 有 executor · 有 bus → Add 阻塞等
func AnonV2(ctx context.Context, in Intent) (*BatchResult, error) {
	w := getDefaultWindow()
	if w == nil || in.BusID == "" {
		// 单独拉号 · 走 Single 语义（返一个"单人 batch"结果 · PullResult 由上层直接跑 decider 填）
		batch, _ := Single(ctx, in)
		return &BatchResult{Batch: batch}, ErrNoExecutor
	}
	return w.Add(ctx, in)
}

// 兜底避免 fmt 未使用（future error 消息用）
var _ = fmt.Sprintf
