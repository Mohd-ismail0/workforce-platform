package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// issuerFixture stands up a fake issuer that serves a REAL discovery document plus a
// matching JWKS, so verification exercises the same discovery path production uses.
// The first revision of these tests pointed the verifier at a guessed JWKS path; this
// one makes the fixture publish discovery so the code has to consume it correctly.
type issuerFixture struct {
	srv *httptest.Server

	mu  sync.RWMutex
	key *ecdsa.PrivateKey
	kid string
}

func newIssuerFixture(t *testing.T) *issuerFixture {
	t.Helper()
	f := &issuerFixture{kid: "key-1"}
	if _, err := f.generate(); err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.RLock()
		key, kid := f.key, f.kid
		f.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                f.srv.URL,
				"jwks_uri":                              f.srv.URL + "/jwks",
				"authorization_endpoint":                f.srv.URL + "/auth",
				"token_endpoint":                        f.srv.URL + "/token",
				"id_token_signing_alg_values_supported": []string{"ES384"},
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
			})
		case "/jwks":
			pub := key.PublicKey
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
				"kty": "EC", "crv": "P-384", "use": "sig", "alg": "ES384", "kid": kid,
				"x": b64(pub.X.Bytes()), "y": b64(pub.Y.Bytes()),
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *issuerFixture) generate() (*ecdsa.PrivateKey, error) {
	k, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.key = k
	f.mu.Unlock()
	return k, nil
}

func (f *issuerFixture) issuer() string { return f.srv.URL }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// sign mints a token with the current fixture key.
func (f *issuerFixture) sign(t *testing.T, header, claims map[string]any) string {
	t.Helper()
	f.mu.RLock()
	key, kid := f.key, f.kid
	f.mu.RUnlock()
	if header == nil {
		header = map[string]any{}
	}
	if _, ok := header["kid"]; !ok {
		header["kid"] = kid
	}
	if _, ok := header["alg"]; !ok {
		header["alg"] = "ES384"
	}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signingInput := b64(h) + "." + b64(c)
	digest := sha512.Sum384([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return signingInput + "." + b64(sig)
}

func (f *issuerFixture) validClaims(aud string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": f.issuer(), "sub": "user-123", "aud": aud,
		"exp": now.Add(30 * time.Minute).Unix(),
		"iat": now.Unix(), "nbf": now.Add(-time.Minute).Unix(),
		"name": "Ada Lovelace", "email": "ada@example.test",
	}
}

func verifierFor(t *testing.T, f *issuerFixture, aud string) *OIDCVerifier {
	t.Helper()
	v, err := NewOIDCVerifier(OIDCConfig{
		Issuer: f.issuer(), Audience: aud, AllowInsecureIssuer: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

const testAudience = "https://workforce.internal/api"

func TestOIDCVerifiesGenuineToken(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)
	tok := f.sign(t, nil, f.validClaims(testAudience))

	claims, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("a genuine token must verify: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.Issuer != f.issuer() {
		t.Fatalf("issuer = %q", claims.Issuer)
	}
	if claims.Name != "Ada Lovelace" {
		t.Fatalf("name = %q", claims.Name)
	}
}

func TestOIDCRejectsTamperedSignatureAndPayload(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)
	tok := f.sign(t, nil, f.validClaims(testAudience))

	last := tok[len(tok)-1]
	repl := byte('A')
	if last == 'A' {
		repl = 'B'
	}
	tampered := tok[:len(tok)-1] + string(repl)
	if _, err := v.Verify(context.Background(), tampered); err == nil {
		t.Fatal("a tampered signature must be rejected")
	}

	// Re-encode the payload with an escalated subject but keep the original signature.
	parts := split3(tok)
	var claims map[string]any
	_ = json.Unmarshal([]byte(dec(parts[1])), &claims)
	claims["sub"] = "someone-else"
	swapped := parts[0] + "." + b64(mustJSON(t, claims)) + "." + parts[2]
	if _, err := v.Verify(context.Background(), swapped); err == nil {
		t.Fatal("a swapped payload must be rejected")
	}
}

func TestOIDCRejectsWrongAudienceIssuerAndExpiry(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)

	other := f.sign(t, nil, f.validClaims("https://other-api.example.test"))
	if _, err := v.Verify(context.Background(), other); err == nil {
		t.Fatal("a token for another audience must be rejected")
	}

	wrong := f.validClaims(testAudience)
	wrong["iss"] = "https://attacker.example.test"
	if _, err := v.Verify(context.Background(), f.sign(t, nil, wrong)); err == nil {
		t.Fatal("a token from another issuer must be rejected")
	}

	expired := f.validClaims(testAudience)
	expired["exp"] = time.Now().Add(-2 * time.Hour).Unix()
	if _, err := v.Verify(context.Background(), f.sign(t, nil, expired)); err == nil {
		t.Fatal("an expired token must be rejected")
	}

	noExp := f.validClaims(testAudience)
	delete(noExp, "exp")
	if _, err := v.Verify(context.Background(), f.sign(t, nil, noExp)); err == nil {
		t.Fatal("a token with no expiry must be rejected")
	}

	// A missing iat must not be read as "fresh": without it the age bound is
	// unenforceable, so the token is refused.
	noIat := f.validClaims(testAudience)
	delete(noIat, "iat")
	if _, err := v.Verify(context.Background(), f.sign(t, nil, noIat)); err == nil {
		t.Fatal("a token with no issue time must be rejected")
	}

	// exp still in the future, but issued too long ago for our age bound.
	old := f.validClaims(testAudience)
	old["iat"] = time.Now().Add(-48 * time.Hour).Unix()
	if _, err := v.Verify(context.Background(), f.sign(t, nil, old)); err == nil {
		t.Fatal("a token older than the age bound must be rejected")
	}

	// A wild future issue time is refused rather than absorbed.
	future := f.validClaims(testAudience)
	future["iat"] = time.Now().Add(3 * time.Hour).Unix()
	if _, err := v.Verify(context.Background(), f.sign(t, nil, future)); err == nil {
		t.Fatal("a token issued in the future must be rejected")
	}
}

// TestOIDCRejectsAlgorithmSubstitution is the classic JWT hole: the header advertises
// "none" or a symmetric algorithm and a naive verifier honours it. The allowlist is
// ours, not the token's.
func TestOIDCRejectsAlgorithmSubstitution(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)

	for _, alg := range []string{"none", "HS256", "RS256", "ES256"} {
		tok := f.sign(t, map[string]any{"alg": alg, "kid": "key-1"}, f.validClaims(testAudience))
		if _, err := v.Verify(context.Background(), tok); err == nil {
			t.Fatalf("algorithm %q must not be accepted", alg)
		}
	}

	// An unsigned "alg: none" token with an empty signature must also fail.
	h := b64(mustJSON(t, map[string]any{"alg": "none", "kid": "key-1"}))
	c := b64(mustJSON(t, f.validClaims(testAudience)))
	if _, err := v.Verify(context.Background(), h+"."+c+"."); err == nil {
		t.Fatal("an unsigned token must be rejected")
	}
}

func TestOIDCRejectsMalformedTokens(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)
	for _, bad := range []string{"", "not-a-jwt", "a.b", "a.b.c.d", "...", "!!!.???.###"} {
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Fatalf("malformed token %q must be rejected", bad)
		}
	}
}

// TestOIDCRefreshesKeysOnRotation proves a rotated key is picked up without a restart,
// which is what makes key rotation operationally safe.
func TestOIDCRefreshesKeysOnRotation(t *testing.T) {
	f := newIssuerFixture(t)
	v := verifierFor(t, f, testAudience)

	first := f.sign(t, nil, f.validClaims(testAudience))
	if _, err := v.Verify(context.Background(), first); err != nil {
		t.Fatalf("initial token: %v", err)
	}

	// Rotate: new key material published under a NEW key id.
	if _, err := f.generate(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.kid = "key-2"
	f.mu.Unlock()

	second := f.sign(t, nil, f.validClaims(testAudience))
	if _, err := v.Verify(context.Background(), second); err != nil {
		t.Fatalf("a token signed by a newly published key must verify without a restart: %v", err)
	}
}

func TestOIDCConfigRequiresIssuerAndAudience(t *testing.T) {
	if _, err := NewOIDCVerifier(OIDCConfig{Audience: "a"}); err == nil {
		t.Fatal("issuer is required")
	}
	if _, err := NewOIDCVerifier(OIDCConfig{Issuer: "https://i"}); err == nil {
		t.Fatal("audience is required")
	}
	if _, err := NewOIDCVerifier(OIDCConfig{Issuer: "not-a-url", Audience: "a"}); err == nil {
		t.Fatal("a non-absolute issuer must be rejected")
	}
	// Plaintext http to a non-loopback host is refused, so a misconfigured production
	// deployment cannot verify tokens fetched in the clear.
	if _, err := NewOIDCVerifier(OIDCConfig{
		Issuer: "http://auth.example.test", Audience: "a", AllowInsecureIssuer: true,
	}); err == nil {
		t.Fatal("a non-loopback http issuer must be rejected")
	}
	// ...but an https issuer is fine.
	if _, err := NewOIDCVerifier(OIDCConfig{Issuer: "https://auth.example.test", Audience: "a"}); err != nil {
		t.Fatalf("an https issuer must be accepted: %v", err)
	}
}

// --- helpers ---

func split3(s string) [3]string {
	i := indexByte(s, '.')
	j := indexByte(s[i+1:], '.') + i + 1
	return [3]string{s[:i], s[i+1 : j], s[j+1:]}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func dec(s string) string {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
