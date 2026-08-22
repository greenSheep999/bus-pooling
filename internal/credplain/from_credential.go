package credplain

import (
	"errors"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// SaveInputFrom · 从 canonical providers.Credential 构造 SaveInput(I-35)。
//
// **credentialID**:我方 credential_ledger.id(UUID v7) · settle 层 INSERT ledger 后
// 才拿得到 · 所以作参数。
//
// **AuthMethod 分派**:跟 housepool.ImportCredentialFrom 用同一份 canonical AuthMethod
// 决策 · 保证入库 auth_method 跟 housepool 后端里实际 credential 类型一致。
//
// **Key 非空校验**:canonical Credential 里 RefreshToken/AccessToken/KiroAPIKey 至少一个
// 非空(否则 vendor 拉了个空号 · 数据出错) · 让调用方决定是否 fatal。这里返 err
// 让 settle tx 回滚 · 崩溃安全。
func SaveInputFrom(c providers.Credential, credentialID string) (SaveInput, error) {
	if credentialID == "" {
		return SaveInput{}, errors.New("credplain: credential_id 不能空")
	}
	in := SaveInput{CredentialID: credentialID, Email: c.Email}
	authMethod := c.AuthMethod
	if authMethod == "" {
		authMethod = providers.AuthAPIKey
	}
	switch authMethod {
	case providers.AuthRefreshToken:
		if c.RefreshToken == "" {
			return SaveInput{}, errors.New("credplain: refresh_token 号但 RefreshToken 空")
		}
		in.AuthMethod = AuthRefreshToken
		in.RefreshToken = c.RefreshToken
	case providers.AuthBearer:
		if c.AccessToken == "" {
			return SaveInput{}, errors.New("credplain: bearer 号但 AccessToken 空")
		}
		in.AuthMethod = AuthBearer
		in.AccessToken = c.AccessToken
	default: // AuthAPIKey
		if c.KiroAPIKey == "" {
			return SaveInput{}, errors.New("credplain: api_key 号但 KiroAPIKey 空")
		}
		in.AuthMethod = AuthAPIKey
		in.KiroAPIKey = c.KiroAPIKey
	}
	return in, nil
}
