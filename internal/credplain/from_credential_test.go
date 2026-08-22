package credplain

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-35 · credplain.SaveInputFrom · canonical Credential → SaveInput 分派 + 校验

func TestSaveInputFrom_RefreshToken(t *testing.T) {
	c := providers.Credential{
		AuthMethod:   providers.AuthRefreshToken,
		RefreshToken: "abc:def",
		Email:        "u@x",
	}
	in, err := SaveInputFrom(c, "ledger-1")
	if err != nil {
		t.Fatalf("SaveInputFrom: %v", err)
	}
	if in.AuthMethod != AuthRefreshToken {
		t.Errorf("AuthMethod 分派错: %q", in.AuthMethod)
	}
	if in.RefreshToken != "abc:def" {
		t.Errorf("RefreshToken 丢了: %+v", in)
	}
	if in.CredentialID != "ledger-1" {
		t.Errorf("CredentialID 丢了: %+v", in)
	}
}

func TestSaveInputFrom_APIKey(t *testing.T) {
	c := providers.Credential{
		AuthMethod: providers.AuthAPIKey,
		KiroAPIKey: "ksk_abc",
		Email:      "svc@x",
	}
	in, err := SaveInputFrom(c, "ledger-1")
	if err != nil {
		t.Fatal(err)
	}
	if in.KiroAPIKey != "ksk_abc" {
		t.Errorf("KiroAPIKey 丢了: %+v", in)
	}
}

func TestSaveInputFrom_EmptyCredentialID_Error(t *testing.T) {
	c := providers.Credential{AuthMethod: providers.AuthAPIKey, KiroAPIKey: "k"}
	_, err := SaveInputFrom(c, "")
	if err == nil {
		t.Error("空 credential_id 应报错")
	}
}

func TestSaveInputFrom_RefreshTokenButEmptyToken_Error(t *testing.T) {
	c := providers.Credential{AuthMethod: providers.AuthRefreshToken}
	_, err := SaveInputFrom(c, "ledger-1")
	if err == nil {
		t.Error("refresh_token 号但 RefreshToken 空 · 应报错")
	}
}

func TestSaveInputFrom_EmptyAuthMethod_APIKeyFallback(t *testing.T) {
	c := providers.Credential{KiroAPIKey: "ksk_x"} // 无 AuthMethod
	in, err := SaveInputFrom(c, "ledger-1")
	if err != nil {
		t.Fatal(err)
	}
	if in.AuthMethod != AuthAPIKey {
		t.Errorf("空 AuthMethod 应兜底 AuthAPIKey · 实际 %q", in.AuthMethod)
	}
}
