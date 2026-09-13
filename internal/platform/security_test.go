package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityWireNames(t *testing.T) {
	raw, _ := json.Marshal(Identity{ID: "u", OrgID: "o", Role: "requester", Name: "User"})
	if !strings.Contains(string(raw), `"org_id":"o"`) {
		t.Fatalf("not snake case: %s", raw)
	}
}
func TestUnimplementedOIDCRefusesStartup(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("unsupported OIDC accepted at startup")
	}
}
func TestDuplicateTokenRefused(t *testing.T) {
	t.Setenv("AUTH_MODE", "local")
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOCAL_AUTH_TOKENS", "same=org:user:requester,same=org:boss:approver")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("duplicate token silently changed authority")
	}
}
