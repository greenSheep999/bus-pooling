package passengerpool

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-35 · passengerpool.PushCredentialFrom · canonical Credential → PushCredential 分派

func TestPushCredentialFrom_RefreshToken(t *testing.T) {
	c := providers.Credential{
		AuthMethod:   providers.AuthRefreshToken,
		RefreshToken: "abc:def",
		Email:        "u@x",
		Region:       "personal",
	}
	p := PushCredentialFrom(c, "ledger-1", "AWS-Q Kiro Vendor 06")
	if p.RefreshToken != "abc:def" {
		t.Errorf("RefreshToken 丢了: %+v", p)
	}
	if p.KiroAPIKey != "" {
		t.Error("refresh_token 号 KiroAPIKey 不该有值")
	}
	if p.CredentialID != "ledger-1" || p.VendorLabel != "AWS-Q Kiro Vendor 06" {
		t.Errorf("元数据丢了: %+v", p)
	}
}

func TestPushCredentialFrom_APIKey(t *testing.T) {
	c := providers.Credential{
		AuthMethod: providers.AuthAPIKey,
		KiroAPIKey: "ksk_x",
		Email:      "svc@x",
		Region:     "us-east-1",
	}
	p := PushCredentialFrom(c, "ledger-2", "vendor-label")
	if p.KiroAPIKey != "ksk_x" {
		t.Errorf("KiroAPIKey 丢了: %+v", p)
	}
	if p.RefreshToken != "" {
		t.Error("api_key 号 RefreshToken 不该有值")
	}
	if p.Region != "us-east-1" {
		t.Errorf("Region 丢了: %+v", p)
	}
}

func TestPushCredentialFrom_EmptyAuthMethod_APIKeyFallback(t *testing.T) {
	c := providers.Credential{KiroAPIKey: "ksk_legacy"}
	p := PushCredentialFrom(c, "id", "vlabel")
	if p.KiroAPIKey != "ksk_legacy" {
		t.Errorf("兜底应走 api_key · 实际 %+v", p)
	}
}
