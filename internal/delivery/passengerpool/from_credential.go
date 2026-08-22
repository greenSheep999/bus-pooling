package passengerpool

import "github.com/bus-pooling/bus-pooling/internal/providers"

// PushCredentialFrom · 从 canonical providers.Credential 构造 PushCredential(I-35)。
//
// **credentialID / vendorLabel** 是下游元数据 · 由调用方(pusher)按 ledger 上下文决定 ·
// 不属于 vendor 拉回来的"号本身"字段。
//
// **AuthMethod 分派**:按 canonical AuthMethod 决定三个明文字段哪个非空。
// 老 4-tuple(AuthMethod 空)兜底走 AuthAPIKey。
func PushCredentialFrom(c providers.Credential, credentialID, vendorLabel string) PushCredential {
	push := PushCredential{
		CredentialID: credentialID,
		Email:        c.Email,
		Region:       c.Region,
		VendorLabel:  vendorLabel,
	}
	authMethod := c.AuthMethod
	if authMethod == "" {
		authMethod = providers.AuthAPIKey
	}
	switch authMethod {
	case providers.AuthRefreshToken:
		push.RefreshToken = c.RefreshToken
	case providers.AuthBearer:
		push.AccessToken = c.AccessToken
	default:
		push.KiroAPIKey = c.KiroAPIKey
	}
	return push
}
