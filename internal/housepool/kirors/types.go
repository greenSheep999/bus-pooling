package kirors

import "encoding/json"

// housepool wire 类型。**不外暴** —— 上层只见 housepool 的归一化类型。
//
// 全部 camelCase：housepool admin 类型都带 #[serde(rename_all = "camelCase")]。
// 这个文件 + mapper.go 是两种命名的唯一交界处（docs/08-housepool-contract.md §10b ①）。

type wireCredentialList struct {
	Total         int              `json:"total"`
	Available     int              `json:"available"`
	DisabledCount int              `json:"disabledCount"`
	CoolingCount  int              `json:"coolingCount"`
	InFlightTotal int              `json:"inFlightTotal"`
	RPMTotal      int              `json:"rpmTotal"`
	TPMTotal      int64            `json:"tpmTotal"`
	CurrentID     uint64           `json:"currentId"`
	Credentials   []wireCredential `json:"credentials"`
}

type wireCredential struct {
	ID                uint64   `json:"id"`
	Priority          uint32   `json:"priority"`
	Disabled          bool     `json:"disabled"`
	FailureCount      uint32   `json:"failureCount"`
	TotalFailureCount uint64   `json:"totalFailureCount"`
	IsCurrent         bool     `json:"isCurrent"`
	ExpiresAt         *string  `json:"expiresAt"`
	AuthMethod        *string  `json:"authMethod"`
	Provider          *string  `json:"provider"`
	Email             *string  `json:"email"`
	SuccessCount      uint64   `json:"successCount"`
	LastUsedAt        *string  `json:"lastUsedAt"`
	DisabledReason    *string  `json:"disabledReason"`
	Endpoint          string   `json:"endpoint"`
	Groups            []string `json:"groups"`
	SourceChannel     *string  `json:"sourceChannel"`
	Subscription      *string  `json:"subscription"`
	AccruedCost       *float64 `json:"accruedCost"`
	BilledRequests    *uint64  `json:"billedRequests"`
	CreatedAt         *string  `json:"createdAt"`
	// Balance · ListCredentials 每条 credential 内嵌一份 balance（上游 1.8.3+ 起就有）·
	// 之前 mapper 没接 · deathwatch 拿不到用量 → snapshot 永远为空
	Balance *wireBalance `json:"balance"`
}

type wireBalance struct {
	ID                uint64  `json:"id"`
	SubscriptionTitle *string `json:"subscriptionTitle"`
	CurrentUsage      float64 `json:"currentUsage"`
	UsageLimit        float64 `json:"usageLimit"`
	Remaining         float64 `json:"remaining"`
	UsagePercentage   float64 `json:"usagePercentage"`
	// NextResetAt · kiro.rs 1.8.3 返 unix epoch number(不是 string)·
	// **强健用 json.Number** · 兼容 upstream 后续改回 string 也不 break
	NextResetAt    *json.Number `json:"nextResetAt"`
	OverageEnabled *bool        `json:"overageEnabled"`
	OverageCapable *bool        `json:"overageCapable"`
}

type wireGroupList struct {
	Total  int         `json:"total"`
	Groups []wireGroup `json:"groups"`
}

type wireGroup struct {
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	CacheMode        *string  `json:"cacheMode"`
	CacheMetering    *string  `json:"cacheMetering"`
	CompactThreshold *float32 `json:"compactThreshold"`
	CreatedAt        string   `json:"createdAt"`
	CredentialCount  int      `json:"credentialCount"`
	ClientKeyCount   int      `json:"clientKeyCount"`
}

type wireCreateGroupRequest struct {
	Name             string   `json:"name"`
	Description      *string  `json:"description,omitempty"`
	CacheMode        *string  `json:"cacheMode,omitempty"`
	CacheMetering    *string  `json:"cacheMetering,omitempty"`
	CompactThreshold *float32 `json:"compactThreshold,omitempty"`
}

type wireUpdateGroupRequest struct {
	NewName          *string  `json:"newName,omitempty"`
	Description      *string  `json:"description,omitempty"`
	CacheMode        *string  `json:"cacheMode,omitempty"`
	CacheMetering    *string  `json:"cacheMetering,omitempty"`
	CompactThreshold *float32 `json:"compactThreshold,omitempty"`
}

// wireSetDisabledRequest 只有一个字段 —— housepool 的 SetDisabledRequest 没有 reason，
// 我方传不进自定义 disable 原因（§10b ④）
type wireSetDisabledRequest struct {
	Disabled bool `json:"disabled"`
}

type wireBatchSetDisabledRequest struct {
	IDs      []uint64 `json:"ids"`
	Disabled bool     `json:"disabled"`
}

type wireBatchDeleteRequest struct {
	IDs []uint64 `json:"ids"`
}

type wireUpdateCredentialRequest struct {
	Email            *string   `json:"email,omitempty"`
	ProxyURL         *string   `json:"proxyUrl,omitempty"`
	ProxyUsername    *string   `json:"proxyUsername,omitempty"`
	ProxyPassword    *string   `json:"proxyPassword,omitempty"`
	Groups           *[]string `json:"groups,omitempty"`
	SourceChannel    *string   `json:"sourceChannel,omitempty"`
	ConcurrencyLimit *uint32   `json:"concurrencyLimit,omitempty"`
}

type wireBatchImportRequest struct {
	Credentials []wireImportCredential `json:"credentials"`
	Concurrency *uint8                 `json:"concurrency,omitempty"`
	Verify      *bool                  `json:"verify,omitempty"`
}

type wireImportCredential struct {
	RefreshToken     string   `json:"refreshToken,omitempty"`
	AccessToken      string   `json:"accessToken,omitempty"`
	KiroAPIKey       string   `json:"kiroApiKey,omitempty"`
	Email            string   `json:"email,omitempty"`
	IssuerURL        string   `json:"issuerUrl,omitempty"`
	StartURL         string   `json:"startUrl,omitempty"`
	TokenEndpoint    string   `json:"tokenEndpoint,omitempty"`
	Scopes           string   `json:"scopes,omitempty"`
	Region           string   `json:"region,omitempty"`
	Groups           []string `json:"groups,omitempty"`
	Priority         uint32   `json:"priority,omitempty"`
	SourceChannel    string   `json:"sourceChannel,omitempty"`
	ConcurrencyLimit *uint32  `json:"concurrencyLimit,omitempty"`
}

// wireBatchImportEvent 是 SSE 流里的一条。
// status: "verified" | "duplicate" | "failed" | "summary"
// summary 事件的 Summary 字段才有值（§10b ③）
type wireBatchImportEvent struct {
	Index        *int                    `json:"index"`
	Status       string                  `json:"status"`
	CredentialID *uint64                 `json:"credentialId"`
	Email        *string                 `json:"email"`
	Usage        *string                 `json:"usage"`
	Subscription *string                 `json:"subscription"`
	Error        *string                 `json:"error"`
	RolledBack   *bool                   `json:"rolledBack"`
	Summary      *wireBatchImportSummary `json:"summary"`
}

type wireBatchImportSummary struct {
	Total      int `json:"total"`
	Imported   int `json:"imported"`
	Verified   int `json:"verified"`
	Duplicate  int `json:"duplicate"`
	Failed     int `json:"failed"`
	RolledBack int `json:"rolledBack"`
}

// wireError housepool 的错误响应。字段名不确定的都收一下，取第一个非空的。
type wireError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

func (e wireError) text() string {
	for _, s := range []string{e.Message, e.Error, e.Detail} {
		if s != "" {
			return s
		}
	}
	return ""
}
