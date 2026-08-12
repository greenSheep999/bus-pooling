package stockwatch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// FileFlag · 文件哨兵开关 · 文件存在 = 开 · 不存在 = 关。
//
// **为什么用文件而不是 env**：env 改了要重启服务 · 人工干预需要**即时生效**。
// 运维一条 SSH 命令就能切 · 5 秒内服务感知 · 不重启不动代码。
//
//	touch /app/data/TURBO_ON     # 开 turbo 强制抢
//	rm    /app/data/TURBO_ON     # 关
//	touch /app/data/KILL_PULLS   # 急停 · 一切 Purchase 停
//	rm    /app/data/KILL_PULLS   # 解除
//
// 开销：每 5s 一次 os.Stat · 纳秒级 · 读走 atomic.Load 不加锁。
type FileFlag struct {
	path   string
	on     atomic.Bool
	name   string // log 用的短名
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// NewFileFlag · path 空时返 nil（该开关禁用 · Engaged 恒 false）
func NewFileFlag(path, name string, logger *slog.Logger) *FileFlag {
	if path == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	f := &FileFlag{path: path, name: name, logger: logger}
	f.refresh() // 启动立即读一次
	return f
}

// Engaged · 当前是否开 · 无锁原子读
func (f *FileFlag) Engaged() bool {
	if f == nil {
		return false
	}
	return f.on.Load()
}

// Path · 开关文件路径 · 启动 log 里报给运维
func (f *FileFlag) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

// Start · 起后台 goroutine · 每 interval 检查一次文件存在性
// interval <= 0 用默认 5s
func (f *FileFlag) Start(ctx context.Context, interval time.Duration) {
	if f == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	runCtx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.done = make(chan struct{})

	go func() {
		defer close(f.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				f.refresh()
			}
		}
	}()
}

func (f *FileFlag) Stop(timeout time.Duration) {
	if f == nil || f.cancel == nil {
		return
	}
	f.cancel()
	select {
	case <-f.done:
	case <-time.After(timeout):
	}
}

// refresh · stat 文件 · 状态变了就 log（运维能在日志里看到人工干预时刻）
func (f *FileFlag) refresh() {
	_, err := os.Stat(f.path)
	exists := err == nil
	old := f.on.Swap(exists)
	if old != exists {
		f.logger.Warn("stockwatch: 开关状态变更（人工干预）",
			"flag", f.name,
			"path", f.path,
			"now", map[bool]string{true: "ON", false: "OFF"}[exists])
	}
}

// Engage · 手动开（写文件）· 给未来 admin API 用 · CLI 也可以直接 touch
func (f *FileFlag) Engage() error {
	if f == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_ = file.Close()
	f.refresh()
	return nil
}

// Release · 手动关（删文件）
func (f *FileFlag) Release() error {
	if f == nil {
		return nil
	}
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	f.refresh()
	return nil
}
