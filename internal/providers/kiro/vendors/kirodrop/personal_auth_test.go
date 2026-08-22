package kirodrop

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-21 · toKeyPayloads 按 region 打 AuthMethod 标签
//
// personal region → AuthRefreshToken(号是 SSO refresh token 冒号串)
// us/eu region    → AuthAPIKey(老 4-tuple 号)

func TestToKeyPayloads_PersonalTagsRefreshToken(t *testing.T) {
	items := []keyItem{{
		ID: "k1", Key: "aor...:MGQ...", Region: "personal",
	}}
	out := toKeyPayloads(items)
	if len(out) != 1 {
		t.Fatalf("len = %d · want 1", len(out))
	}
	if out[0].AuthMethod != providers.AuthRefreshToken {
		t.Errorf("AuthMethod = %q · want refresh_token", out[0].AuthMethod)
	}
	if out[0].Key != "aor...:MGQ..." {
		t.Errorf("Key 丢了: %q", out[0].Key)
	}
	if out[0].Region != "personal" {
		t.Errorf("Region = %q · want personal", out[0].Region)
	}
}

func TestToKeyPayloads_EnterpriseTagsAPIKey(t *testing.T) {
	items := []keyItem{{
		ID: "k1", Key: "ksk_abc", Account: "user@x.com",
		IssuerURL: "https://idp/", Region: "us-east-1",
	}}
	out := toKeyPayloads(items)
	if out[0].AuthMethod != providers.AuthAPIKey {
		t.Errorf("us region AuthMethod = %q · want api_key", out[0].AuthMethod)
	}
	if out[0].Account != "user@x.com" || out[0].IssuerURL != "https://idp/" {
		t.Errorf("4-tuple 字段丢了: %+v", out[0])
	}
}

func TestToKeyPayloads_EUEnterpriseTagsAPIKey(t *testing.T) {
	items := []keyItem{{ID: "k1", Key: "ksk_eu", Region: "eu-central-1"}}
	out := toKeyPayloads(items)
	if out[0].AuthMethod != providers.AuthAPIKey {
		t.Errorf("eu region AuthMethod = %q · want api_key", out[0].AuthMethod)
	}
}
