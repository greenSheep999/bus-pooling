package providers

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Provider 是一个协议族（当前只有 kiro）。
type Provider interface {
	ID() ProviderID
	Vendors(ctx context.Context) ([]VendorEntry, error)
	Vendor(vendorID VendorID) (Vendor, error)
	WebhookParser() WebhookParser
}

type VendorEntry struct {
	VendorID    VendorID
	DisplayName string
	Vendor      Vendor
	// Enabled 关掉的 vendor 仍在 Registry 里（配置可见），但 Enabled() 不返回它。
	// 出事时能一键摘掉一家而不用改代码。
	Enabled bool
}

// Registry 是 vendor 的查找表。
//
// **存在的理由**：decider / pullrecord 只 import 这个包，靠 VendorID 拿到实现
// （契约 §10：decider 不 import 任何 vendor 具体包）。没有它，上层就得 import
// 6 个 adapter 包，加一家 vendor 要改所有调用点。
//
// 并发安全 —— 注册通常在启动时一次做完，但 admin 改 enabled 是运行时的。
type Registry struct {
	mu      sync.RWMutex
	entries map[VendorID]VendorEntry
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[VendorID]VendorEntry)}
}

// Register 登记一家 vendor。重复注册同一个 VendorID 是错误 ——
// 静默覆盖会让"到底用了哪份配置"变成猜谜。
func (r *Registry) Register(v Vendor, enabled bool) error {
	if v == nil {
		return fmt.Errorf("providers: 注册了 nil vendor")
	}
	id := v.ID()
	if id == "" {
		return fmt.Errorf("providers: vendor 的 ID 为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.entries[id]; dup {
		return fmt.Errorf("providers: vendor %q 重复注册", id)
	}
	r.entries[id] = VendorEntry{
		VendorID:    id,
		DisplayName: v.DisplayName(),
		Vendor:      v,
		Enabled:     enabled,
	}
	return nil
}

// Get 按 id 取 vendor。**不管 Enabled** —— 停用的 vendor 也要能取到，
// 否则手上还有它拉来的号时，deathwatch / 对账就没法回头查那家。
func (r *Registry) Get(id VendorID) (Vendor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, fmt.Errorf("providers: 没有注册过 vendor %q: %w", id, ErrNotFound)
	}
	return e.Vendor, nil
}

// All 返回全部登记项（含停用的），按 VendorID 排序 ——
// 稳定顺序让日志和测试可复现。
func (r *Registry) All() []VendorEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sortedLocked(func(VendorEntry) bool { return true })
}

// Enabled 只返回启用的 —— decider 比价时用这个。
func (r *Registry) Enabled() []VendorEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sortedLocked(func(e VendorEntry) bool { return e.Enabled })
}

func (r *Registry) sortedLocked(keep func(VendorEntry) bool) []VendorEntry {
	out := make([]VendorEntry, 0, len(r.entries))
	for _, e := range r.entries {
		if keep(e) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VendorID < out[j].VendorID })
	return out
}

// SetEnabled 运行时开关一家 vendor（admin 用）。
func (r *Registry) SetEnabled(id VendorID, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return fmt.Errorf("providers: 没有注册过 vendor %q: %w", id, ErrNotFound)
	}
	e.Enabled = enabled
	r.entries[id] = e
	return nil
}
