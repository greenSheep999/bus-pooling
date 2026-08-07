# bus-pooling · Housepool (kiro.rs) 客户端契约

> 前置：`03-modules.md § Layer 4` · kiro.rs 源码 `src/admin/router.rs`
> 本文钉 **`internal/housepool` 抽象层 + `housepool/kirors` 具体实现的 Go interface + 请求响应 struct**。

## 1. 目录布局

```
internal/housepool/
├── housepool.go            HousePool interface（抽象）
├── types.go                归一化 struct（跨实现共享）
├── errors.go               错误 sentinel + wrap
└── kirors/
    ├── client.go           kiro.rs 具体 client（httpx 出向 + rate limit）
    ├── types.go            kiro.rs wire type（不外暴）
    ├── mapper.go           wire → 归一化的翻译
    └── client_test.go
```

**上层代码（decider / pullrecord / bus / deathwatch）永远只 import `internal/housepool`**，不 import `kirors` 子包。**换其它号池实现只需加 `housepool/otherimpl/`**。

## 2. HousePool interface（抽象）

```go
// internal/housepool/housepool.go
package housepool

type HousePool interface {
    // ── Credential 管理 ─────────────────────────────────────────

    BatchImport(ctx context.Context, req BatchImportRequest) (*BatchImportResult, error)
    UpdateCredential(ctx context.Context, id CredentialID, patch CredentialPatch) error
    SetDisabled(ctx context.Context, id CredentialID, disabled bool) error
    SetDisabledBatch(ctx context.Context, ids []CredentialID, disabled bool) error
    DeleteCredential(ctx context.Context, id CredentialID) error
    DeleteCredentialBatch(ctx context.Context, ids []CredentialID) error
    ListCredentials(ctx context.Context, filter CredentialFilter) ([]Credential, error)
    GetCredential(ctx context.Context, id CredentialID) (*Credential, error)
    GetBalance(ctx context.Context, id CredentialID) (*Balance, error)
    TestCredential(ctx context.Context, id CredentialID) error
    RefreshToken(ctx context.Context, id CredentialID) error

    // ── Group 管理（bus / record / market） ────────────────────

    ListGroups(ctx context.Context) ([]Group, error)
    CreateGroup(ctx context.Context, req GroupRequest) (*Group, error)
    UpdateGroup(ctx context.Context, name string, req GroupRequest) error
    DeleteGroup(ctx context.Context, name string) error

    // ── Client Key 发放（下游 API 拉取用） ────────────────────

    ListClientKeys(ctx context.Context, filter ClientKeyFilter) ([]ClientKey, error)
    CreateClientKey(ctx context.Context, req ClientKeyRequest) (*ClientKey, error)
    RotateClientKey(ctx context.Context, id ClientKeyID) (*ClientKey, error)
    UpdateClientKey(ctx context.Context, id ClientKeyID, req ClientKeyRequest) error
    DeleteClientKey(ctx context.Context, id ClientKeyID) error
    SetClientKeyDisabled(ctx context.Context, id ClientKeyID, disabled bool) error

    // ── 统计（用于 bus 视角 + 平均寿命） ──────────────────────

    StatsOverview(ctx context.Context) (*StatsOverview, error)
    StatsByCredential(ctx context.Context, opts StatsOptions) ([]CredentialStats, error)
    StatsByModel(ctx context.Context, opts StatsOptions) ([]ModelStats, error)
    StatsTimeSeries(ctx context.Context, opts StatsOptions) ([]TimeSeriesPoint, error)

    // ── 并发（TBD，见 §7） ───────────────────────────────────

    GetConcurrency(ctx context.Context, id CredentialID) (*Concurrency, error)   // 暂返回 ErrNotSupported

    // ── 生命周期 ─────────────────────────────────────────────

    Ping(ctx context.Context) error
    Close() error
}
```

## 3. Credential 相关类型

```go
type CredentialID uint64   // kiro.rs 侧的 id 类型是 u64

type Credential struct {
    ID               CredentialID
    Email            string
    Priority         uint32
    Disabled         bool
    DisabledReason   string
    Subscription     string
    Provider         string          // 上游 provider 标记（未使用）
    AuthMethod       string
    Endpoint         string
    SourceChannel    string
    Groups           []string        // 一个 credential 可属多个 group
    ExpiresAt        *time.Time
    LastUsedAt       *time.Time
    CreatedAt        time.Time
    SuccessCount     uint64
    FailureCount     uint32
    TotalFailureCount uint64
    AccruedCost      float64
    BilledRequests   uint64
    Balance          *Balance
}

type Balance struct {
    ID                CredentialID
    SubscriptionTitle string
    CurrentUsage      float64
    UsageLimit        float64
    Remaining         float64
    UsagePercentage   float64
    NextResetAt       *time.Time
    OverageEnabled    *bool
    OverageCapable    *bool
}

type CredentialPatch struct {
    // 只有非零字段才写入 kiro.rs（对应 kiro.rs `UpdateCredentialRequest`）
    Email             *string
    ProxyURL          *string
    ProxyUsername     *string
    ProxyPassword     *string
    Groups            *[]string     // None=不动，Some=整体替换（跟 kiro.rs 一致）
    SourceChannel     *string
    ConcurrencyLimit  *uint32       // 0 = 清除 override
}

type CredentialFilter struct {
    Groups        []string        // 按 group 过滤（"bus-<id>" 或 "record-<pid>" 或 "market"）
    IncludeDisabled bool
    IncludeDead     bool          // dead 是我方内部状态；kiro.rs 侧只有 disabled
}
```

## 4. BatchImport 相关

```go
type BatchImportRequest struct {
    Credentials []ImportCredential
    Concurrency uint8             // kiro.rs 侧的并发导入上限；0 = 默认
    Verify      bool               // 是否验证凭证（默认 true）
}

type ImportCredential struct {
    // 各家 vendor 归一化后的凭证；具体是 kiro-key 或 AWS SSO refresh token 由字段决定
    RefreshToken   string
    AccessToken    string
    KiroAPIKey     string          // 直接用 kiro key 时填这里
    Email          string
    IssuerURL      string
    StartURL       string
    TokenEndpoint  string
    Scopes         string
    Region         string
    Groups         []string        // 归属 group（"bus-<id>" 或 "record-<pid>"）
    Priority       uint32
    SourceChannel  string          // 我方标记 vendor id（"kiro91" / ...）
    ConcurrencyLimit *uint32
    // ...其它字段见 kiro.rs `BatchImportCredential`
}

type BatchImportResult struct {
    Events  <-chan BatchImportEvent  // kiro.rs 是 SSE 流
    Summary <-chan BatchImportSummary // 最后一条汇总
}

type BatchImportEvent struct {
    Index        *int
    Status       BatchImportStatus  // "imported" | "verified" | "duplicate" | "failed" | "error"
    CredentialID *CredentialID
    Email        string
    Usage        string
    Error        string
    RolledBack   *bool
}

type BatchImportStatus string
const (
    ImportStatusImported  BatchImportStatus = "imported"
    ImportStatusVerified  BatchImportStatus = "verified"
    ImportStatusDuplicate BatchImportStatus = "duplicate"
    ImportStatusFailed    BatchImportStatus = "failed"
    ImportStatusError     BatchImportStatus = "error"
)

type BatchImportSummary struct {
    Total     int
    Imported  int
    Verified  int
    Duplicated int
    Failed    int
    Errored   int
}
```

**上层用法**：拿到 `Events` 后 for-range 读到 channel 关闭；`Summary` 拿到一次即可。

## 5. Group 相关

```go
type Group struct {
    Name             string
    Description      string
    CacheMode        string           // kiro.rs 侧的字段，我方一般不动
    CacheMetering    string
    CompactThreshold float32
    CreatedAt        time.Time
    CredentialCount  int
    ClientKeyCount   int
}

type GroupRequest struct {
    Name             string
    Description      string
    CacheMode        string
    CacheMetering    string
    CompactThreshold float32
}
```

**我方约定的 group 命名**：

- `bus-<bus_id>` · 拼车（1 人或多人）
- `record-<passenger_id>` · 单独拉号暂存
- `market` · 市场（阶段 3d）

## 6. ClientKey 相关

```go
type ClientKeyID uint64

type ClientKey struct {
    ID           ClientKeyID
    Name         string
    Description  string
    Group        string           // "bus-<id>" 等
    MaskedKey    string           // UI 展示用
    PlaintextKey string           // **只在 Create/Rotate 时返回一次**，List 里为空
    Disabled     bool
    IsSystem     bool
    CreatedAt    time.Time
    LastUsedAt   *time.Time
    TotalCalls   uint64
    TotalInputTokens         uint64
    TotalOutputTokens        uint64
    TotalCacheCreationTokens uint64
    TotalCacheReadTokens     uint64
}

type ClientKeyRequest struct {
    Name        string
    Description string
    Group       string
    UsageLimit  int64            // advisory only（kiro.rs 不强制；我方 Key Guard 靠自己）
    UsageUnit   string           // "requests" | "input_tokens" | "output_tokens" | "usd"
}

type ClientKeyFilter struct {
    Group string
}
```

**关键**：`UsageLimit / UsageUnit` **advisory** —— kiro.rs 不强制。我方**上层**（`bus` + `deathwatch`）负责实际执行 quota 限制。

## 7. Stats 相关（bus 视角号数据）

```go
type StatsOptions struct {
    Window  Window            // "24h" | "7d" | "30d"
    KeyID   *string           // 按 client key 过滤
    Group   *string           // 按 group 过滤（"bus-<id>" 等）
}

type StatsOverview struct {
    TodayCalls          int64
    TodayInputTokens    int64
    TodayOutputTokens   int64
    TodayErrors         int64
    TodayCredits        int64      // microunit
    WeekCalls           int64
    WeekInputTokens     int64
    WeekOutputTokens    int64
    WeekCredits         int64
    ActiveClientKeys    int64
    ActiveCredentials   int64
    ObservedAt          time.Time
}

type CredentialStats struct {
    CredentialID CredentialID
    Email        string
    Calls        int64
    InputTokens  int64
    OutputTokens int64
    Errors       int64
    // 平均并发（TBD，见 §8）
    ConcurrencyAvg *int
}

type ModelStats struct {
    Model        string
    Calls        int64
    InputTokens  int64
    OutputTokens int64
    Errors       int64
}

type TimeSeriesPoint struct {
    Timestamp    time.Time
    Calls        int64
    InputTokens  int64
    OutputTokens int64
    Errors       int64
    Credits      int64
}

type Concurrency struct {
    CredentialID CredentialID
    Current      int                // 瞬时活跃调用
    PeakLastMin  int
    AvgLastHour  float64
    ObservedAt   time.Time
}
```

## 8. 平均并发 · 特殊说明

**kiro.rs 当前未提供直接读端点**（`POST /credentials/{id}/clear-concurrency` 存在证明内部有并发计数）。

**方案**（三选一，等 kiro.rs 运维方拍板）：

- **(a) 推荐**：给 kiro.rs 加 `GET /credentials/{id}/concurrency`，我方 `HousePool.GetConcurrency` 直接调
- (b) 我方定时采样 kiro.rs（如每分钟一次，从 stats 里推算不可行，从活跃状态推算需 kiro.rs 加个 endpoint）
- (c) 反推（不可行，需响应时间）

**未拍板前**：`HousePool.GetConcurrency` 返回 `ErrNotSupported` sentinel；`CredentialStats.ConcurrencyAvg` 常态 `nil`；UI 显示 `—`。

## 9. 错误契约

```go
// internal/housepool/errors.go
package housepool

var (
    ErrAuth              = errors.New("housepool: auth failure")
    ErrNotFound          = errors.New("housepool: not found")
    ErrConflict          = errors.New("housepool: state conflict")
    ErrUpstream          = errors.New("housepool: upstream (kiro.rs) error")
    ErrTimeout           = errors.New("housepool: timeout")
    ErrRateLimited       = errors.New("housepool: rate limited by kiro.rs")
    ErrNotSupported      = errors.New("housepool: capability not supported by this backend")
    ErrCredentialDead    = errors.New("housepool: credential is dead")
)

type APIError struct {
    StatusCode  int
    Message     string
    Sentinel    error
    RawResponse json.RawMessage
}
```

## 10. kiro.rs 客户端具体实现（`housepool/kirors`）

```go
// internal/housepool/kirors/client.go
package kirors

type Client struct {
    baseURL   string             // "https://kiro.aibbq.xyz"
    adminKey  string             // 从 secrets 拿
    httpClient *http.Client      // 走 internal/httpx.Client
    logger    *log.Logger
}

type Config struct {
    BaseURL        string
    AdminKeyRef    string        // 指向 secrets 里加密存的 key
    Timeout        time.Duration
    RetryBackoff   time.Duration
    MaxRetries     int
}

func New(cfg Config, secretsSvc secrets.Service, httpClient *http.Client) (*Client, error) { ... }

// 实现 housepool.HousePool interface 全部方法
func (c *Client) BatchImport(ctx context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error) { ... }
// ... 其它方法
```

**约定**：
- 所有出向请求走 `internal/httpx.Client`（proxy / timeout / retry 统一）
- `adminKey` 明文永不落文件，从 `internal/secrets` 拿
- BatchImport 走 SSE（kiro.rs 是流式响应），我方需要一个 goroutine 读 stream 塞 channel

## 11. kiro.rs 端点对应表

| 我方法 | kiro.rs 端点 | 备注 |
|---|---|---|
| `BatchImport` | `POST /credentials/batch-import` | SSE 流 |
| `UpdateCredential` | `PUT /credentials/{id}` | body 只带需改字段 |
| `SetDisabled` | `POST /credentials/{id}/disabled` | body `{disabled: bool, reason?: string}` |
| `SetDisabledBatch` | `POST /credentials/batch/disabled` | body `{ids: [...], disabled: bool}` |
| `DeleteCredential` | `DELETE /credentials/{id}` | |
| `DeleteCredentialBatch` | `POST /credentials/batch/delete` | |
| `ListCredentials` | `GET /credentials` | 可加 query 过滤 |
| `GetCredential` | `GET /credentials/{id}` | 单个 |
| `GetBalance` | `GET /credentials/{id}/balance` | 号的用量 |
| `TestCredential` | `POST /credentials/{id}/test` | 探活 |
| `RefreshToken` | `POST /credentials/{id}/refresh` | 强制刷新 |
| `ListGroups` | `GET /groups` | |
| `CreateGroup` | `POST /groups` | |
| `UpdateGroup` | `PATCH /groups/{name}` | |
| `DeleteGroup` | `DELETE /groups/{name}` | |
| `ListClientKeys` | `GET /client-keys` | |
| `CreateClientKey` | `POST /client-keys` | 返回明文 |
| `RotateClientKey` | `POST /client-keys/{id}/rotate` | 返回新明文 |
| `UpdateClientKey` | `PUT /client-keys/{id}` | |
| `DeleteClientKey` | `DELETE /client-keys/{id}` | |
| `SetClientKeyDisabled` | `POST /client-keys/{id}/disabled` | |
| `StatsOverview` | `GET /stats/overview` | today + week 数据 |
| `StatsByCredential` | `GET /stats/by-credential?range=&group=` | 每 credential 明细 |
| `StatsByModel` | `GET /stats/by-model?range=` | 按模型 |
| `StatsTimeSeries` | `GET /stats/timeseries?range=&granularity=` | 时间序列 |
| `GetConcurrency` | ⚠️ 待 kiro.rs 加 `GET /credentials/{id}/concurrency` | 见 §8 |
| `Ping` | `GET /` 或 `GET /stats/overview` | 健康检查 |

**Auth**：所有 `/admin/*` 端点走 `Authorization: Bearer <admin-key>`（kiro.rs `src/admin/middleware.rs`）。

## 12. 事务顺序约定（重要）

**credential 状态转换时**：**先做外部动作，再改 housepool 状态**。

举例（handoff · 拿走）：
1. 从 kiro.rs 读 credential 明文
2. 返回给用户 / 保存到 UI 交付
3. **成功后**才 `DELETE /credentials/{id}` 让号离开 housepool

举例（推乘客 passengerpool · 双写）：
1. 从 kiro.rs 读明文
2. 调乘客的 kiro.rs `BatchImport` 复制
3. **成功后**才在我方 housepool 改 `disabled=false`

**理由**：顺序反了会出现 "号已经交出去 / 复制出去，但状态没改" 的不一致；反过来"状态改了但外部动作失败"至少还能回滚。

## 13. 幂等

kiro.rs 的 endpoint **不都幂等**：

- `BatchImport` **幂等**（`RefreshToken` 唯一约束）
- `PUT /credentials/{id}` **幂等**（同 body 多次 = 一次）
- `POST /credentials/{id}/disabled` **幂等**
- `DELETE /credentials/{id}` **幂等**（第二次调返回 404 / already deleted）

我方 `kirors.Client` **不需要**自己实现幂等键，直接调 kiro.rs 就行。**但要处理 404 幂等**（第二次 DELETE 视为成功）。

## 14. 测试约定

- `client_test.go` 用 `httptest.Server` mock kiro.rs 端点
- Mock 响应数据参考 kiro.rs 源码 `src/admin/handlers.rs` 里的实际返回结构
- 覆盖每个方法的成功 + 404 + 5xx + 超时 + rate limit 各一个用例
