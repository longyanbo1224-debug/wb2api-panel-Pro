package auth

import (
	"encoding/base64"
	"testing"
)

// mustJWT 用给定 payload 拼一个最小 JWT（header.payload.signature）。
func mustJWT(t *testing.T, payload string) string {
	t.Helper()
	return "hdr." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

// TestEnterpriseFormIgnoresTokenSource 企业形态只看 enterpriseId；JWT 的
// token_source 不参与判定。旧实现把 enterprise_switch 当企业上下文，实测
// （issue #240，2026-10-24）其行为事件照常计分，误挡了正常个人号。
func TestEnterpriseFormIgnoresTokenSource(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"enterprise_switch", mustJWT(t, `{"sub":"u1","token_source":"enterprise_switch"}`)},
		{"无 token_source 声明", mustJWT(t, `{"sub":"u1","scope":"openid profile"}`)},
		{"其他 token_source", mustJWT(t, `{"sub":"u1","token_source":"password"}`)},
		{"带填充 base64url", "hdr." + base64.URLEncoding.EncodeToString([]byte(`{"token_source":"enterprise_switch"}`)) + ".sig"},
		{"payload 非 JSON", "hdr." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".sig"},
		{"非 JWT 形态", "opaque-token"},
		{"空 token", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Auth{UID: "u1", AccessToken: c.token}
			if a.IsEnterprise() {
				t.Errorf("token_source=%s 不应被判定为企业版", c.name)
			}
		})
	}

	t.Run("enterpriseId 非空恒为真", func(t *testing.T) {
		a := &Auth{UID: "u1", EnterpriseID: "e1", AccessToken: mustJWT(t, `{"sub":"u1"}`)}
		if !a.IsEnterprise() {
			t.Error("enterpriseId 非空时应判定为企业版")
		}
	})
}
