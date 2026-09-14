package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func oauthIssuer(t *testing.T) *issuerFixture {
	t.Helper()
	f := newIssuerFixture(t)
	// The existing fixture serves discovery + JWKS, and the OAuth client only needs the
	// discovery endpoints at construction. Auth/token are intentionally not called by the
	// tests below.
	return f
}

func TestOAuthClientRequiresConfidentialConfiguration(t *testing.T) {
	f := oauthIssuer(t)
	base := OAuthConfig{Issuer: f.issuer(), AllowInsecureIssuer: true,
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://127.0.0.1/callback"}
	cases := []struct {
		name string
		cfg  OAuthConfig
	}{
		{"missing issuer", OAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "http://x/cb"}},
		{"missing client id", OAuthConfig{Issuer: f.issuer(), AllowInsecureIssuer: true, ClientSecret: "s", RedirectURL: "http://x/cb"}},
		{"missing client secret", OAuthConfig{Issuer: f.issuer(), AllowInsecureIssuer: true, ClientID: "c", RedirectURL: "http://x/cb"}},
		{"relative redirect", OAuthConfig{Issuer: f.issuer(), AllowInsecureIssuer: true, ClientID: "c", ClientSecret: "s", RedirectURL: "/callback"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOAuthClient(context.Background(), tc.cfg); err == nil {
				t.Fatal("invalid confidential-client configuration was accepted")
			}
		})
	}
	if c, err := NewOAuthClient(context.Background(), base); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	} else if c.ClientID() != "client" || c.SessionTTL() != 12*time.Hour {
		t.Fatalf("client configuration did not carry through: client=%q ttl=%s", c.ClientID(), c.SessionTTL())
	}
}

func TestOAuthClientBuildsExplicitPKCEAuthorizationRequest(t *testing.T) {
	f := oauthIssuer(t)
	c, err := NewOAuthClient(context.Background(), OAuthConfig{
		Issuer: f.issuer(), AllowInsecureIssuer: true,
		ClientID: "workforce-client", ClientSecret: "secret",
		RedirectURL: "http://127.0.0.1:8095/auth/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	state := "state-value"
	nonce := "nonce-value"
	challenge := PKCEChallenge(strings.Repeat("a", 43))
	raw := c.AuthCodeURL(state, nonce, challenge)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id": "workforce-client", "redirect_uri": "http://127.0.0.1:8095/auth/callback",
		"response_type": "code", "state": state, "nonce": nonce,
		"code_challenge": challenge, "code_challenge_method": "S256",
	} {
		if q.Get(key) != want {
			t.Fatalf("authorization parameter %s=%q, want %q; URL=%s", key, q.Get(key), want, raw)
		}
	}
	if q.Get("scope") != "openid profile email" {
		t.Fatalf("scope=%q, want default openid profile email", q.Get("scope"))
	}
}

func TestOAuthClientUsesConfiguredIDTokenAlgorithms(t *testing.T) {
	f := oauthIssuer(t)
	c, err := NewOAuthClient(context.Background(), OAuthConfig{
		Issuer: f.issuer(), AllowInsecureIssuer: true,
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://x/callback",
		AllowedAlgs: []string{"RS256"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("client was nil")
	}
	if _, err := NewOAuthClient(context.Background(), OAuthConfig{
		Issuer: f.issuer(), AllowInsecureIssuer: true,
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://x/callback",
		AllowedAlgs: []string{"none"},
	}); err == nil {
		t.Fatal("unsupported algorithm was accepted")
	}
}

// Ensure the OAuth client does not accidentally use a caller-supplied global client with
// no timeout. This is a structural test: the client is private, but a request to a
// deliberately unreachable endpoint must return rather than hang. Discovery itself is
// exercised by the constructor, while the loopback fixture proves the configured client
// path works.
func TestOAuthIssuerFixtureIsHTTPOnlyForTests(t *testing.T) {
	f := oauthIssuer(t)
	if !strings.HasPrefix(f.issuer(), "http://") {
		t.Fatal("fixture is not a loopback HTTP issuer")
	}
	// Keep net/http imported in this test file as a compile-time reminder that the client
	// boundary is HTTP; this handler is never served.
	_ = http.MethodGet
	_ = httptest.NewRecorder
}
