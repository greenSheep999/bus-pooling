package providers

import "testing"

// I-35 · canonical Credential + FromKeyPayload/ToKeyPayload 一致性测试。
//
// **契约**:老 KeyPayload → Credential → KeyPayload 应该 round-trip 无损。
// 未来加字段时 · 转换函数漏映射会立即挂。

func TestKeyPayload_RoundTrip_RefreshToken(t *testing.T) {
	original := KeyPayload{
		VendorKeyID: "k1",
		Key:         "sso:refresh",
		AuthMethod:  AuthRefreshToken,
		Account:     "user@example.com",
		IssuerURL:   "",
		Region:      "personal",
		Free:        false,
	}
	cred := FromKeyPayload(original)
	if cred.AuthMethod != AuthRefreshToken {
		t.Errorf("AuthMethod 丢了: %q", cred.AuthMethod)
	}
	if cred.RefreshToken != "sso:refresh" {
		t.Errorf("refresh_token 号 · Key 应进 Credential.RefreshToken · 实际 %+v", cred)
	}
	if cred.KiroAPIKey != "" {
		t.Error("refresh_token 号 · KiroAPIKey 不该有值")
	}
	// 反向
	back := cred.ToKeyPayload()
	if back.Key != original.Key || back.Account != original.Account ||
		back.Region != original.Region || back.AuthMethod != original.AuthMethod {
		t.Errorf("round-trip 丢字段: orig=%+v back=%+v", original, back)
	}
}

func TestKeyPayload_RoundTrip_APIKey(t *testing.T) {
	original := KeyPayload{
		VendorKeyID: "k2",
		Key:         "ksk_abc123",
		AuthMethod:  AuthAPIKey,
		Account:     "svc@example.com",
		IssuerURL:   "https://idp/",
		Region:      "us-east-1",
	}
	cred := FromKeyPayload(original)
	if cred.KiroAPIKey != "ksk_abc123" {
		t.Errorf("api_key 号 · Key 应进 KiroAPIKey · 实际 %+v", cred)
	}
	if cred.RefreshToken != "" {
		t.Error("api_key 号 · RefreshToken 不该有值")
	}
	if cred.Email != "svc@example.com" {
		t.Errorf("Email 丢了: %q", cred.Email)
	}
	if cred.IssuerURL != "https://idp/" {
		t.Errorf("IssuerURL 丢了: %q", cred.IssuerURL)
	}
}

// TestKeyPayload_EmptyAuthMethod_DefaultsToAPIKey · 老 adapter 未打标兜底
func TestKeyPayload_EmptyAuthMethod_DefaultsToAPIKey(t *testing.T) {
	original := KeyPayload{
		Key: "ksk_legacy", AuthMethod: "", // 老 4-tuple 未声明
	}
	cred := FromKeyPayload(original)
	if cred.AuthMethod != AuthAPIKey {
		t.Errorf("空 AuthMethod 应兜底 AuthAPIKey · 实际 %q", cred.AuthMethod)
	}
	if cred.KiroAPIKey != "ksk_legacy" {
		t.Errorf("兜底应进 KiroAPIKey · 实际 %+v", cred)
	}
}

// TestCredential_ToKeyPayload_RefreshToken · Credential 反向转 KeyPayload · 老代码兼容
func TestCredential_ToKeyPayload_RefreshToken(t *testing.T) {
	c := Credential{
		AuthMethod:   AuthRefreshToken,
		RefreshToken: "abc:def",
		Email:        "u@x",
		Region:       "personal",
	}
	kp := c.ToKeyPayload()
	if kp.Key != "abc:def" {
		t.Errorf("Key 应等于 RefreshToken · 实际 %q", kp.Key)
	}
	if kp.Account != "u@x" {
		t.Errorf("Account 应等于 Email · 实际 %q", kp.Account)
	}
}
