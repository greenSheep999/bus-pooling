package housepool

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrNotFound 号池里没这个号 / group / key
	ErrNotFound = errors.New("housepool: 资源不存在")
	// ErrUnauthorized admin key 不对
	ErrUnauthorized = errors.New("housepool: 号池鉴权失败")
	// ErrUnavailable 号池连不上或 5xx —— 对外映射成 housepool_unavailable（503）
	ErrUnavailable = errors.New("housepool: 号池暂时不可用")
	// ErrNotSupported 号池没这个能力（例：并发查询 · 契约 §7）
	ErrNotSupported = errors.New("housepool: 号池不支持这个操作")
	// ErrConflict 重名 group 之类
	ErrConflict = errors.New("housepool: 冲突")
)

// Error 带上号池返回的细节，便于日志排查。**不要直接把它的 Message 透给用户**
// —— 里面可能有内部术语（CLAUDE.md §12.6）。
type Error struct {
	Op      string // 哪个操作
	Status  int    // HTTP 状态
	Message string // 号池返回的错误文本
	Err     error  // 归类到的 sentinel
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("housepool: %s 失败（HTTP %d）", e.Op, e.Status)
	}
	return fmt.Sprintf("housepool: %s 失败（HTTP %d）: %s", e.Op, e.Status, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// HousePool 是号池抽象。
//
// **阶段 1a 的实现范围**（sprint Iss #7）：Credential 的增删改查 + Group 基本操作
// + BatchImport + Ping。ClientKey 和 Stats 组留在接口里但 1a 不实现（返回
// ErrNotSupported）—— 1e 推 passengerpool 和 1d 采数据时才用到。
type HousePool interface {
	// ── Credential ─────────────────────────────────────

	BatchImport(ctx context.Context, req BatchImportRequest) (*BatchImportResult, error)
	UpdateCredential(ctx context.Context, id CredentialID, patch CredentialPatch) error
	SetDisabled(ctx context.Context, id CredentialID, disabled bool) error
	SetDisabledBatch(ctx context.Context, ids []CredentialID, disabled bool) error
	DeleteCredential(ctx context.Context, id CredentialID) error
	DeleteCredentialBatch(ctx context.Context, ids []CredentialID) error
	// ListCredentials 顺带返回池子快照（§10b ② · 列表端点免费给的聚合值）
	ListCredentials(ctx context.Context, filter CredentialFilter) ([]Credential, *PoolSnapshot, error)
	GetCredential(ctx context.Context, id CredentialID) (*Credential, error)
	GetBalance(ctx context.Context, id CredentialID) (*Balance, error)
	// TestCredential 探活。**这是唯一可靠的主动判死手段**（返回 error 即判死）
	TestCredential(ctx context.Context, id CredentialID) error
	RefreshToken(ctx context.Context, id CredentialID) error

	// ── Group ──────────────────────────────────────────

	ListGroups(ctx context.Context) ([]Group, error)
	CreateGroup(ctx context.Context, req GroupRequest) (*Group, error)
	UpdateGroup(ctx context.Context, name string, req GroupRequest) error
	DeleteGroup(ctx context.Context, name string) error

	// ── ClientKey（1e 才实现） ─────────────────────────

	ListClientKeys(ctx context.Context, filter ClientKeyFilter) ([]ClientKey, error)
	CreateClientKey(ctx context.Context, req ClientKeyRequest) (*ClientKey, error)
	RotateClientKey(ctx context.Context, id ClientKeyID) (*ClientKey, error)
	UpdateClientKey(ctx context.Context, id ClientKeyID, req ClientKeyRequest) error
	DeleteClientKey(ctx context.Context, id ClientKeyID) error
	SetClientKeyDisabled(ctx context.Context, id ClientKeyID, disabled bool) error

	// ── Stats（1d 才实现） ────────────────────────────

	StatsOverview(ctx context.Context) (*StatsOverview, error)
	StatsByCredential(ctx context.Context, opts StatsOptions) ([]CredentialStats, error)
	StatsByModel(ctx context.Context, opts StatsOptions) ([]ModelStats, error)
	StatsTimeSeries(ctx context.Context, opts StatsOptions) ([]TimeSeriesPoint, error)

	// GetConcurrency 号池没这个端点（契约 §7）· 恒返回 ErrNotSupported
	GetConcurrency(ctx context.Context, id CredentialID) (*Concurrency, error)

	// ── 生命周期 ───────────────────────────────────────

	Ping(ctx context.Context) error
	Close() error
}
