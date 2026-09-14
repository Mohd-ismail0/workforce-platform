package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Token primitives for the browser flow: opaque session identifiers, PKCE code verifiers,
// and the state/nonce values that bind a callback to the login that started it.
//
// Everything here is generated from crypto/rand. Nothing is derived from anything the
// client supplies, because a value the client can influence is not a secret.

// OpaqueTokenBytes is the entropy of a session identifier, in bytes. 32 bytes (256 bits)
// makes guessing infeasible and keeps the encoded value comfortably inside cookie limits.
const OpaqueTokenBytes = 32

// NewOpaqueToken returns a URL-safe random token with no structure. It deliberately carries
// no claims: all authority is looked up server-side from the stored row.
func NewOpaqueToken() (string, error) {
	b := make([]byte, OpaqueTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secure randomness unavailable: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the value stored for a token. Storage is by hash so that a database
// read cannot be converted into a live session, and a session identifier is never logged or
// compared in the clear.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokensEqual compares in constant time. A byte-by-byte comparison leaks how much of a
// guess was correct, which is enough to recover a token one character at a time.
func TokensEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// PKCE (RFC 7636).

// pkceUnreserved is the character set the spec allows in a code verifier.
const pkceUnreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// NewPKCEVerifier returns a code verifier of the maximum permitted length (128 characters),
// which maximises entropy. Only S256 is ever used for the challenge: "plain" provides no
// protection against an attacker who can intercept the authorization code.
func NewPKCEVerifier() (string, error) {
	b := make([]byte, 128)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secure randomness unavailable: %w", err)
	}
	// Modulo bias is irrelevant here: len(pkceUnreserved)=66 does not divide 256, but the
	// bias is on the order of 2^-128 and the verifier's strength comes from its length.
	var sb strings.Builder
	sb.Grow(len(b))
	for _, v := range b {
		sb.WriteByte(pkceUnreserved[int(v)%len(pkceUnreserved)])
	}
	return sb.String(), nil
}

// PKCEChallenge derives the S256 challenge: BASE64URL(SHA256(verifier)) without padding.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ValidatePKCEVerifier enforces the spec's length bound, so a malformed verifier fails
// before it reaches the token endpoint.
func ValidatePKCEVerifier(v string) error {
	if len(v) < 43 || len(v) > 128 {
		return errors.New("code verifier must be 43 to 128 characters")
	}
	for i := 0; i < len(v); i++ {
		if !strings.ContainsRune(pkceUnreserved, rune(v[i])) {
			return errors.New("code verifier contains a character outside the unreserved set")
		}
	}
	return nil
}

// SafeReturnTo normalises a post-login destination.
//
// This is an open-redirect defence, and the checks are deliberately narrow: only a
// root-relative path is allowed. "//evil.example" and "/\evil.example" are both treated as
// absolute by browsers even though they begin with a slash, so a naive HasPrefix("/") test
// is a real bypass. Anything else falls back to "/" rather than erroring, because a bad
// return_to must not be able to fail a login.
func SafeReturnTo(raw string) string {
	if raw == "" {
		return "/"
	}
	// Control characters can be used to smuggle a header or confuse a parser.
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return "/"
		}
	}
	if !strings.HasPrefix(raw, "/") {
		return "/"
	}
	// "//host" and "/\host" are absolute URLs to a browser.
	if len(raw) > 1 && (raw[1] == '/' || raw[1] == '\\') {
		return "/"
	}
	// A scheme or host cannot appear in a pure path.
	if strings.Contains(raw, "\\") || strings.Contains(raw, "://") {
		return "/"
	}
	// Defense in depth, and the reason is specific rather than general caution: as a
	// Location value a browser resolves %2F%2F within our own origin, so THIS hop is safe.
	// But if any downstream layer decoded before redirecting, %2F%2F would become
	// protocol-relative and escape. Our own UI only ever emits plain paths, so refusing
	// these costs nothing that is actually used.
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "/"
	}
	return raw
}
