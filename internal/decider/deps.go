package decider

import (
	"context"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// VendorClient 是 decider 用到的 vendor 能力子集。
//
// 不直接用 providers.Vendor：窄接口让依赖关系清楚，测试里 mock 也不用实现全部方法。
type VendorClient interface {
	ID() providers.VendorID
	Capability() providers.Capability
	Stock(ctx context.Context, opts providers.StockOptions) (*providers.StockSnapshot, error)
	Purchase(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error)
	OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error)
}

// PoolClient 是 decider 用到的号池能力子集。
//
// 两组用途:
//   - BatchImport · 前 6 家 vendor 拉到号后**新导入**号池
//   - UpdateCredential · 我方手工池路径 · 号已在池 · 只搬 group（Step 3f · docs/24 §3）
type PoolClient interface {
	BatchImport(ctx context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error)
	UpdateCredential(ctx context.Context, id housepool.CredentialID, patch housepool.CredentialPatch) error
	// GetCredential · 手工池路径拿打码 key 用（号早在池里 · 我方无明文可算）
	GetCredential(ctx context.Context, id housepool.CredentialID) (*housepool.Credential, error)
}
