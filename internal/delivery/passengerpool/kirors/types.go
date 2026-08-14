package kirors

// wire 类型跟 housepool/kirors/types.go 是同一份协议(camelCase) ·
// 但这里是**给对家**发的·所以：
//   - 只保留 BatchImport 相关的 struct
//   - 不需要 ClientKey / Stats / Group 那些(对家不给我方管)
//   - 错误响应 wireError 也复用同样的兜底逻辑

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

// wireError 对家错误响应 · 兜底几种字段名。
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
