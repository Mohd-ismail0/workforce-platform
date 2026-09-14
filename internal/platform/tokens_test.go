package platform

import (
	"strings"
	"testing"
)

// PKCE has an authoritative test vector in RFC 7636 Appendix B. Using it means the
// challenge derivation is checked against the specification itself rather than against my
// own implementation of it, which would pass even if both were wrong.
func TestPKCEChallengeMatchesRFC7636Vector(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := PKCEChallenge(verifier); got != want {
		t.Fatalf("challenge = %q, want the RFC 7636 Appendix B value %q", got, want)
	}
}

func TestPKCEVerifierIsGeneratedAndValid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		v, err := NewPKCEVerifier()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePKCEVerifier(v); err != nil {
			t.Fatalf("generated a verifier rejected by our own validator: %v (%q)", err, v)
		}
		if len(v) != 128 {
			t.Fatalf("verifier length = %d, want the maximum of 128", len(v))
		}
		// A repeated verifier would mean the randomness is not random. At 128 chars modulo
		// bias is irrelevant, so this is a real check on crypto/rand usage.
		if seen[v] {
			t.Fatal("a verifier was generated twice: randomness is not working")
		}
		seen[v] = true
	}
}

func TestValidatePKCEVerifierRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"too short":          strings.Repeat("a", 42),
		"too long":           strings.Repeat("a", 129),
		"space not allowed":  "aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa",
		"plus not allowed":   strings.Repeat("a", 42) + "+",
		"slash not allowed":  strings.Repeat("a", 42) + "/",
		"equals not allowed": strings.Repeat("a", 42) + "=",
	}
	for name, v := range cases {
		if err := ValidatePKCEVerifier(v); err == nil {
			t.Fatalf("%s: malformed verifier was accepted", name)
		}
	}
	// A minimal valid verifier must still be accepted, so the validator is not simply
	// rejecting everything.
	if err := ValidatePKCEVerifier(strings.Repeat("a", 43)); err != nil {
		t.Fatalf("the minimum-length valid verifier was rejected: %v", err)
	}
}

func TestOpaqueTokensAreUniqueAndSufficientlyLong(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		tok, err := NewOpaqueToken()
		if err != nil {
			t.Fatal(err)
		}
		// The session cookie value must not be guessable, and must not contain characters
		// that need escaping in a cookie or URL.
		if len(tok) < 40 {
			t.Fatalf("opaque token too short to be safe: %d chars", len(tok))
		}
		if strings.ContainsAny(tok, "=+/ ") {
			t.Fatalf("opaque token contains a character that needs escaping: %q", tok)
		}
		if seen[tok] {
			t.Fatal("a token was generated twice")
		}
		seen[tok] = true
	}
}

// Storage is by hash, so the same token must always hash the same way and different tokens
// must not collide; and the stored value must not contain the token itself.
func TestHashTokenIsStableAndOneWay(t *testing.T) {
	tok, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	h1, h2 := HashToken(tok), HashToken(tok)
	if h1 != h2 {
		t.Fatal("hashing the same token produced different results")
	}
	if strings.Contains(h1, tok) {
		t.Fatal("the hash contains the token")
	}
	if len(h1) != 64 {
		t.Fatalf("hash length = %d, want 64 hex characters for SHA-256", len(h1))
	}
	other, err := NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	if HashToken(other) == h1 {
		t.Fatal("two different tokens hashed identically")
	}
}

func TestTokensEqual(t *testing.T) {
	if !TokensEqual("abc", "abc") {
		t.Fatal("identical tokens compared unequal")
	}
	for _, tc := range [][2]string{{"abc", "abd"}, {"abc", "ab"}, {"", "a"}, {"a", ""}} {
		if TokensEqual(tc[0], tc[1]) {
			t.Fatalf("%q and %q compared equal", tc[0], tc[1])
		}
	}
}

// SafeReturnTo is an open-redirect defence. The interesting cases are the ones that LOOK
// like a local path but are treated as absolute by browsers, because a naive
// HasPrefix("/") check accepts every one of them.
func TestSafeReturnToRefusesRedirectsOffSite(t *testing.T) {
	refused := []string{
		"//evil.example",           // protocol-relative: absolute to a browser
		"//evil.example/path",      //
		"/\\evil.example",          // backslash is normalised to / by browsers
		"\\\\evil.example",         //
		"https://evil.example",     // fully absolute
		"http://evil.example/path", //
		"javascript:alert(1)",      // scheme, not a path
		"data:text/html,<script>",  //
		"evil.example",             // no leading slash at all
		"",                         // empty is not a destination
		"/path\nSet-Cookie: x=y",   // control character used to smuggle a header
		"/path\r\nX-Injected: 1",   //
		"/path\x00trailing",        // NUL
		"/%2F%2Fevil.example",      // encoded slashes: see the defense-in-depth note
		"/%2f%2fevil.example",      // lower-case encoding must not slip through
		"/%5Cevil.example",         // encoded backslash
	}
	for _, raw := range refused {
		if got := SafeReturnTo(raw); got != "/" {
			t.Fatalf("SafeReturnTo(%q) = %q; an off-site or malformed destination must fall back to \"/\"", raw, got)
		}
	}
}

// Path traversal is NOT an open-redirect concern and must not be conflated with one. A
// browser resolving "../.." clamps at the origin root, so "/a/../../../../etc/passwd" is a
// same-origin path; what protects that file is AUTHORIZATION on the request, not this
// function. An earlier revision of these tests demanded "/" here, which asserted behaviour
// the function neither does nor should do. What this test does assert is the boundary that
// matters: the value never becomes an absolute URL.
func TestSafeReturnToKeepsTraversalSameOrigin(t *testing.T) {
	for _, raw := range []string{
		"/a/../../../../etc/passwd",
		"/../..",
		"/./tasks/../tasks",
	} {
		got := SafeReturnTo(raw)
		if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			t.Fatalf("SafeReturnTo(%q) = %q; must remain a same-origin path", raw, got)
		}
		if strings.Contains(got, "://") {
			t.Fatalf("SafeReturnTo(%q) = %q; must not become an absolute URL", raw, got)
		}
	}
}

// The real security property, tested as a property rather than against a list I can only
// be as imaginative as. Whatever the input, the output must be a same-origin path.
func TestSafeReturnToAlwaysYieldsASameOriginPath(t *testing.T) {
	adversarial := []string{
		"", "/", "//", "///", "////evil.example", "//evil.example", "/\\evil.example",
		"\\/evil.example", "https://evil.example", "HTTP://evil.example",
		"javascript:alert(1)", "data:text/html,x", "vbscript:x", "file:///etc/passwd",
		"evil.example", "..", "../", "	//evil.example", " /evil.example",
		"/path\nSet-Cookie: x=y", "/path\r\nX: 1", "/\x00", "/\x7f",
		"/%2F%2Fevil.example", "/%5Cevil.example", "/a%00b", "/tasks/abc?x=1#y",
		"/\u0000//evil.example", "http:/evil.example", "/http://evil.example",
	}
	for _, raw := range adversarial {
		got := SafeReturnTo(raw)
		switch {
		case got == "":
			t.Fatalf("SafeReturnTo(%q) returned empty; a destination must always be usable", raw)
		case !strings.HasPrefix(got, "/"):
			t.Fatalf("SafeReturnTo(%q) = %q; must start with /", raw, got)
		case len(got) > 1 && (got[1] == '/' || got[1] == '\\'):
			t.Fatalf("SafeReturnTo(%q) = %q; protocol-relative", raw, got)
		case strings.Contains(got, "\\"):
			t.Fatalf("SafeReturnTo(%q) = %q; contains a backslash", raw, got)
		case strings.Contains(got, "://"):
			t.Fatalf("SafeReturnTo(%q) = %q; contains a scheme", raw, got)
		}
		for i := 0; i < len(got); i++ {
			if got[i] < 0x20 || got[i] == 0x7f {
				t.Fatalf("SafeReturnTo(%q) = %q; contains a control character", raw, got)
			}
		}
	}
}

func TestSafeReturnToAllowsLocalPaths(t *testing.T) {
	allowed := map[string]string{
		"/":                    "/",
		"/tasks":               "/tasks",
		"/tasks/abc?tab=1":     "/tasks/abc?tab=1",
		"/needs-input#top":     "/needs-input#top",
		"/a/b/c/d/e":           "/a/b/c/d/e",
		"/path-with.dots.html": "/path-with.dots.html",
	}
	for raw, want := range allowed {
		if got := SafeReturnTo(raw); got != want {
			t.Fatalf("SafeReturnTo(%q) = %q, want %q", raw, got, want)
		}
	}
}
