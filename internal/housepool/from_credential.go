package housepool

import "github.com/bus-pooling/bus-pooling/internal/providers"

// ImportCredentialFrom · 从 canonical providers.Credential 构造 ImportCredential(I-35)。
//
// 收下游元数据(groups 谁分派 / priority 谁定)作为参数 · 因为这些不属于"号本身"·
// 由调用方(decider)按上下文决定。
//
// **AuthMethod 分派**:按 canonical AuthMethod 决定 RefreshToken/AccessToken/KiroAPIKey
// 哪个字段有值。老 4-tuple(AuthMethod 空)兜底走 AuthAPIKey。
func ImportCredentialFrom(c providers.Credential, groups []string) ImportCredential {
	imp := ImportCredential{
		Email:         c.Email,
		IssuerURL:     c.IssuerURL,
		StartURL:      c.StartURL,
		TokenEndpoint: c.TokenEndpoint,
		Scopes:        c.Scopes,
		Region:        c.Region,
		Groups:        groups,
	}
	// 按 AuthMethod 分派三选一
	authMethod := c.AuthMethod
	if authMethod == "" {
		authMethod = providers.AuthAPIKey
	}
	switch authMethod {
	case providers.AuthRefreshToken:
		imp.RefreshToken = c.RefreshToken
	case providers.AuthBearer:
		imp.AccessToken = c.AccessToken
	default:
		imp.KiroAPIKey = c.KiroAPIKey
	}
	return imp
}
