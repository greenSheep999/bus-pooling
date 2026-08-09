package api

import (
	"context"
	"sync"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// fullMockPool 满足 housepool.HousePool 接口·测试用。
// 记录所有 UpdateCredential 调用·让测试断言"迁 group 真被调"。
type fullMockPool struct {
	mu             sync.Mutex
	updateCalls    []updateCall
	deleteCalls    []housepool.CredentialID
	failUpdateNext bool // true = 下次 UpdateCredential 返错·测失败回滚
}

type updateCall struct {
	ID    housepool.CredentialID
	Patch housepool.CredentialPatch
}

func (p *fullMockPool) UpdateCredential(_ context.Context, id housepool.CredentialID, patch housepool.CredentialPatch) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failUpdateNext {
		p.failUpdateNext = false
		return &housepool.Error{Op: "UpdateCredential", Err: housepool.ErrUnavailable, Message: "mock fail"}
	}
	p.updateCalls = append(p.updateCalls, updateCall{ID: id, Patch: patch})
	return nil
}

func (p *fullMockPool) DeleteCredential(_ context.Context, id housepool.CredentialID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleteCalls = append(p.deleteCalls, id)
	return nil
}

// ── 以下方法 stub·测试不用直接返错 ────────────────

func (p *fullMockPool) BatchImport(_ context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error) {
	return nil, &housepool.Error{Op: "BatchImport", Err: housepool.ErrNotSupported}
}
func (p *fullMockPool) ListCredentials(_ context.Context, _ housepool.CredentialFilter) ([]housepool.Credential, *housepool.PoolSnapshot, error) {
	return nil, nil, nil
}
func (p *fullMockPool) GetCredential(_ context.Context, _ housepool.CredentialID) (*housepool.Credential, error) {
	return nil, &housepool.Error{Op: "GetCredential", Err: housepool.ErrNotFound}
}
func (p *fullMockPool) SetDisabled(_ context.Context, _ housepool.CredentialID, _ bool) error {
	return nil
}
func (p *fullMockPool) SetDisabledBatch(_ context.Context, _ []housepool.CredentialID, _ bool) error {
	return nil
}
func (p *fullMockPool) DeleteCredentialBatch(_ context.Context, _ []housepool.CredentialID) error {
	return nil
}
func (p *fullMockPool) GetBalance(_ context.Context, _ housepool.CredentialID) (*housepool.Balance, error) {
	return nil, &housepool.Error{Op: "GetBalance", Err: housepool.ErrNotSupported}
}
func (p *fullMockPool) TestCredential(_ context.Context, _ housepool.CredentialID) error {
	return nil
}
func (p *fullMockPool) RefreshToken(_ context.Context, _ housepool.CredentialID) error {
	return nil
}
func (p *fullMockPool) ListGroups(_ context.Context) ([]housepool.Group, error) {
	return nil, nil
}
func (p *fullMockPool) CreateGroup(_ context.Context, _ housepool.GroupRequest) (*housepool.Group, error) {
	return nil, nil
}
func (p *fullMockPool) UpdateGroup(_ context.Context, _ string, _ housepool.GroupRequest) error {
	return nil
}
func (p *fullMockPool) DeleteGroup(_ context.Context, _ string) error { return nil }
func (p *fullMockPool) Ping(_ context.Context) error                  { return nil }
func (p *fullMockPool) Close() error                                  { return nil }

// ClientKey 组·1e 才实现
func (p *fullMockPool) ListClientKeys(_ context.Context, _ housepool.ClientKeyFilter) ([]housepool.ClientKey, error) {
	return nil, nil
}
func (p *fullMockPool) CreateClientKey(_ context.Context, _ housepool.ClientKeyRequest) (*housepool.ClientKey, error) {
	return nil, &housepool.Error{Op: "CreateClientKey", Err: housepool.ErrNotSupported}
}
func (p *fullMockPool) RotateClientKey(_ context.Context, _ housepool.ClientKeyID) (*housepool.ClientKey, error) {
	return nil, &housepool.Error{Op: "RotateClientKey", Err: housepool.ErrNotSupported}
}
func (p *fullMockPool) UpdateClientKey(_ context.Context, _ housepool.ClientKeyID, _ housepool.ClientKeyRequest) error {
	return nil
}
func (p *fullMockPool) DeleteClientKey(_ context.Context, _ housepool.ClientKeyID) error {
	return nil
}
func (p *fullMockPool) SetClientKeyDisabled(_ context.Context, _ housepool.ClientKeyID, _ bool) error {
	return nil
}

// Stats 组·1d 才实现
func (p *fullMockPool) StatsOverview(_ context.Context) (*housepool.StatsOverview, error) {
	return nil, &housepool.Error{Op: "StatsOverview", Err: housepool.ErrNotSupported}
}
func (p *fullMockPool) StatsByCredential(_ context.Context, _ housepool.StatsOptions) ([]housepool.CredentialStats, error) {
	return nil, nil
}
func (p *fullMockPool) StatsByModel(_ context.Context, _ housepool.StatsOptions) ([]housepool.ModelStats, error) {
	return nil, nil
}
func (p *fullMockPool) StatsTimeSeries(_ context.Context, _ housepool.StatsOptions) ([]housepool.TimeSeriesPoint, error) {
	return nil, nil
}
func (p *fullMockPool) GetConcurrency(_ context.Context, _ housepool.CredentialID) (*housepool.Concurrency, error) {
	return nil, &housepool.Error{Op: "GetConcurrency", Err: housepool.ErrNotSupported}
}

// 编译期检查接口实现
var _ housepool.HousePool = (*fullMockPool)(nil)
