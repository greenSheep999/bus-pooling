package housepool

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-35 · housepool.ImportCredentialFrom · canonical Credential → ImportCredential 分派

func TestImportCredentialFrom_RefreshToken(t *testing.T) {
	c := providers.Credential{
		AuthMethod:   providers.AuthRefreshToken,
		RefreshToken: "sso:rt",
		Email:        "u@x",
		Region:       "personal",
	}
	imp := ImportCredentialFrom(c, []string{"bus-1"})
	if imp.RefreshToken != "sso:rt" {
		t.Errorf("refresh_token 号 RefreshToken 丢了: %+v", imp)
	}
	if imp.KiroAPIKey != "" {
		t.Error("refresh_token 号 KiroAPIKey 不该有值")
	}
	if imp.Email != "u@x" || imp.Region != "personal" {
		t.Errorf("元数据丢了: %+v", imp)
	}
	if len(imp.Groups) != 1 || imp.Groups[0] != "bus-1" {
		t.Errorf("Groups 丢了: %+v", imp.Groups)
	}
}

func TestImportCredentialFrom_APIKey(t *testing.T) {
	c := providers.Credential{
		AuthMethod: providers.AuthAPIKey,
		KiroAPIKey: "ksk_abc",
		Email:      "svc@x",
		IssuerURL:  "https://idp/",
		Region:     "us-east-1",
	}
	imp := ImportCredentialFrom(c, []string{"record-p1"})
	if imp.KiroAPIKey != "ksk_abc" {
		t.Errorf("api_key 号 KiroAPIKey 丢了: %+v", imp)
	}
	if imp.RefreshToken != "" {
		t.Error("api_key 号 RefreshToken 不该有值")
	}
	if imp.IssuerURL != "https://idp/" {
		t.Errorf("IssuerURL 丢了: %+v", imp)
	}
}

func TestImportCredentialFrom_EmptyAuthMethod_APIKeyFallback(t *testing.T) {
	c := providers.Credential{
		AuthMethod: "", // 老 adapter 未打标
		KiroAPIKey: "ksk_legacy",
	}
	imp := ImportCredentialFrom(c, []string{"g"})
	if imp.KiroAPIKey != "ksk_legacy" {
		t.Errorf("兜底应走 api_key · 实际 %+v", imp)
	}
}
