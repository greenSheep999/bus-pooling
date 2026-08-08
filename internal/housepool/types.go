// Package housepool 是我方号池的抽象层。
//
// **上层（decider / pullrecord / bus / deathwatch）只 import 这个包**，不 import
// 具体实现（kirors）。换号池实现只需加一个新子包。
//
// 这里的类型是**归一化**后的形状 —— 具体实现负责把各自的 wire 格式翻译过来
// （kiro.rs 是 camelCase 且列表端点包了一层，见 docs/08-housepool-contract.md §10b）。
package housepool

import "time"

// CredentialID 是号池侧的 id。kiro.rs 用 u64。
//
// **注意跟我方 `credential_ledger.id` 区分**：那个是 UUID v7 字符串，是对外 API 的
// `credential_id`；这个只在跟号池对账时出现（05-api-contract §5 的 ID 口径）。
type CredentialID uint64

type Credential struct {
	ID       CredentialID
	Email    string
	Priority uint32
	Disabled bool
	// DisabledReason 是号池侧的**闭合枚举**，我方传不进自定义值。
	// 判死规则见 docs/08-housepool-contract.md §DisabledReason 判据：
	// Manual = 我方主动 disable（不是死号）· Suspended/QuotaExceeded/InvalidRefreshToken = 判死
	DisabledReason    string
	Subscription      string
	Provider          string
	AuthMethod        string
	Endpoint          string
	SourceChannel     string // 我方用它标 vendor id（"kiro91" / …）
	Groups            []string
	ExpiresAt         *time.Time
	LastUsedAt        *time.Time
	CreatedAt         time.Time
	SuccessCount      uint64
	FailureCount      uint32
	TotalFailureCount uint64
	AccruedCost       float64
	BilledRequests    uint64
	Balance           *Balance
}

// DisabledReason 的取值（kiro.rs src/kiro/token_manager.rs 的闭合枚举）。
const (
	// ReasonManual 我方 Admin API disable 的都是这个 —— **不是死号**
	//（拉号记录待派 / handoff 待确认 / 成员挂起都落这个）
	ReasonManual = "Manual"
	// 以下三个是明确的失效，可直接判死
	ReasonSuspended           = "Suspended"
	ReasonQuotaExceeded       = "QuotaExceeded"
	ReasonInvalidRefreshToken = "InvalidRefreshToken"
	// 以下是号池自愈机制触发的，**可能自动恢复** → 要用 TestCredential 复核
	ReasonTooManyFailures        = "TooManyFailures"
	ReasonTooManyRefreshFailures = "TooManyRefreshFailures"
	ReasonAutoThrottled          = "AutoThrottled"
	// ReasonInvalidConfig 我方导入错了，不是死号，该报警人工看
	ReasonInvalidConfig = "InvalidConfig"
)

// IsDeadReason 判断这个 disabled 原因是否可以**直接**判死。
//
// 返回 false 不等于"活着" —— 可能是 Manual（我方主动）也可能是需要复核的自愈态，
// 调用方要按 §DisabledReason 判据分流。
func IsDeadReason(reason string) bool {
	switch reason {
	case ReasonSuspended, ReasonQuotaExceeded, ReasonInvalidRefreshToken:
		return true
	}
	return false
}

// NeedsProbe 判断这个 disabled 原因是否需要 TestCredential 复核。
func NeedsProbe(reason string) bool {
	switch reason {
	case ReasonTooManyFailures, ReasonTooManyRefreshFailures, ReasonAutoThrottled:
		return true
	}
	return false
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

// CredentialPatch 只有非 nil 字段才写入号池。
type CredentialPatch struct {
	Email         *string
	ProxyURL      *string
	ProxyUsername *string
	ProxyPassword *string
	// Groups nil = 不动 · 非 nil = **整体替换**（跟 kiro.rs 语义一致）
	Groups           *[]string
	SourceChannel    *string
	ConcurrencyLimit *uint32 // 0 = 清除 override
}

type CredentialFilter struct {
	// Groups 按 group 过滤（"bus-<id>" / "record-<pid>" / "market"）
	Groups          []string
	IncludeDisabled bool
}

// ── BatchImport ──────────────────────────────────────

type BatchImportRequest struct {
	Credentials []ImportCredential
	// Concurrency 号池侧的并发导入上限 · 0 = 用号池默认
	Concurrency uint8
	// Verify 是否验活。**注意**：不验活的话导入的号可能一上线就是死的
	Verify bool
}

type ImportCredential struct {
	RefreshToken     string
	AccessToken      string
	KiroAPIKey       string
	Email            string
	IssuerURL        string
	StartURL         string
	TokenEndpoint    string
	Scopes           string
	Region           string
	Groups           []string
	Priority         uint32
	SourceChannel    string
	ConcurrencyLimit *uint32
}

type BatchImportStatus string

const (
	ImportStatusVerified  BatchImportStatus = "verified"
	ImportStatusDuplicate BatchImportStatus = "duplicate"
	ImportStatusFailed    BatchImportStatus = "failed"
	// ImportStatusSummary 是流里的**最后一个事件** —— 号池不是分两个流返回的
	ImportStatusSummary BatchImportStatus = "summary"
)

type BatchImportEvent struct {
	Index        *int
	Status       BatchImportStatus
	CredentialID *CredentialID
	Email        string
	Usage        string
	Subscription string
	Error        string
	// RolledBack failed 且号池已把它删掉时为 true
	RolledBack *bool
}

type BatchImportSummary struct {
	Total      int
	Imported   int
	Verified   int
	Duplicate  int
	Failed     int
	RolledBack int
}

// BatchImportResult 是 SSE 流的两个出口。
//
// 实现上是**一个** HTTP 流：读到 status=="summary" 的事件时塞进 Summary 再关闭两者。
// 上层用法：for-range Events 到关闭，然后从 Summary 取一次。
type BatchImportResult struct {
	Events  <-chan BatchImportEvent
	Summary <-chan BatchImportSummary
	// Err 在两个 channel 都关闭后可读 —— 流中断时拿原因
	Err func() error
}

// ── Group ────────────────────────────────────────────

type Group struct {
	Name             string
	Description      string
	CacheMode        string
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

// GroupName 拼我方约定的 group 名（CLAUDE.md §1.1）。
//
// 集中在这里是为了别在各处手拼字符串 —— 拼错一个字号就进错组，而且很难查。
func BusGroup(busID string) string          { return "bus-" + busID }
func RecordGroup(passengerID string) string { return "record-" + passengerID }

const MarketGroup = "market"

// ── ClientKey ────────────────────────────────────────

type ClientKeyID uint64

type ClientKey struct {
	ID          ClientKeyID
	Name        string
	Description string
	Group       string
	MaskedKey   string
	// PlaintextKey **只在 Create / Rotate 时有值**，List 里恒为空
	PlaintextKey             string
	Disabled                 bool
	IsSystem                 bool
	CreatedAt                time.Time
	LastUsedAt               *time.Time
	TotalCalls               uint64
	TotalInputTokens         uint64
	TotalOutputTokens        uint64
	TotalCacheCreationTokens uint64
	TotalCacheReadTokens     uint64
}

type ClientKeyRequest struct {
	Name        string
	Description string
	Group       string
	// UsageLimit / UsageUnit 对号池是 **advisory** —— 它不强制。
	// 真正的额度限制由我方上层（bus + deathwatch）执行。
	UsageLimit int64
	UsageUnit  string
}

type ClientKeyFilter struct {
	Group string
}

// ── Stats ────────────────────────────────────────────

type Window string

const (
	Window24h Window = "24h"
	Window7d  Window = "7d"
	Window30d Window = "30d"
)

type StatsOptions struct {
	Window Window
	KeyID  *string
	Group  *string
}

type StatsOverview struct {
	TodayCalls        int64
	TodayInputTokens  int64
	TodayOutputTokens int64
	TodayErrors       int64
	TodayCredits      int64 // microunit
	WeekCalls         int64
	WeekInputTokens   int64
	WeekOutputTokens  int64
	WeekCredits       int64
	ActiveClientKeys  int64
	ActiveCredentials int64
	ObservedAt        time.Time
}

type CredentialStats struct {
	CredentialID CredentialID
	Email        string
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	Errors       int64
	// ConcurrencyAvg 号池没有这个端点（契约 §7）· 恒为 nil，UI 显示 "—"
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
	At           time.Time
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	Errors       int64
}

type Concurrency struct {
	CredentialID CredentialID
	InFlight     int
	Average      *int
}

// PoolSnapshot 是列表端点顺带给的聚合值。
//
// kiro.rs 的 GET /credentials 响应里包了这些字段（§10b ②），拿列表时**免费**得到，
// 不用单独打 stats 端点。
type PoolSnapshot struct {
	Total         int
	Available     int
	DisabledCount int
	CoolingCount  int
	InFlightTotal int
	RPMTotal      int
	TPMTotal      int64
}
