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

// oidc mode is real now, so the contract is no longer "refuse startup". It is: refuse
// if the trust anchors are missing or unusable, and accept only when they are valid.
func TestOIDCRequiresIssuerAndAudience(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("APP_ENV", "development")

	t.Setenv("WORKFORCE_OIDC_ISSUER", "")
	t.Setenv("WORKFORCE_OIDC_AUDIENCE", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("oidc mode with no issuer accepted at startup")
	}

	// An issuer that is not an absolute URL must be refused, not deferred to runtime.
	t.Setenv("WORKFORCE_OIDC_ISSUER", "not-a-url")
	t.Setenv("WORKFORCE_OIDC_AUDIENCE", "https://workforce.internal/api")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("oidc mode with a malformed issuer accepted at startup")
	}

	// A plaintext issuer is refused: a misconfigured deployment must not verify tokens
	// it fetched in the clear.
	t.Setenv("WORKFORCE_OIDC_ISSUER", "http://auth.example.test/oidc")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("oidc mode with an http issuer accepted at startup")
	}

	t.Setenv("WORKFORCE_OIDC_ISSUER", "https://auth.example.test/oidc")
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("a valid oidc configuration must load: %v", err)
	}
	if c.OIDC.Issuer != "https://auth.example.test/oidc" ||
		c.OIDC.Audience != "https://workforce.internal/api" {
		t.Fatalf("oidc config not carried through: %#v", c.OIDC)
	}
}

// An audience is part of the trust decision, not an optional refinement: accepting a
// token minted for another API would let a different application's token act here.
func TestOIDCRequiresAudience(t *testing.T) {
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("APP_ENV", "development")
	t.Setenv("WORKFORCE_OIDC_ISSUER", "https://auth.example.test/oidc")
	t.Setenv("WORKFORCE_OIDC_AUDIENCE", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("oidc mode with no audience accepted at startup")
	}
}

// With oidc implemented there is a temptation to let production fall back to local
// auth when the issuer is unreachable. It must not: that would turn a transient identity
// outage into an authentication bypass.
func TestProductionStillRefusesLocalAuth(t *testing.T) {
	t.Setenv("AUTH_MODE", "local")
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOCAL_AUTH_TOKENS", "token=org:user:requester")
	t.Setenv("WORKFORCE_OIDC_ISSUER", "https://auth.example.test/oidc")
	t.Setenv("WORKFORCE_OIDC_AUDIENCE", "https://workforce.internal/api")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production accepted local auth")
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
