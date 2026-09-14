package platform

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDC verification for the workforce kernel.
//
// This delegates the cryptography to the maintained go-oidc library rather than
// hand-rolling JWS parsing. The reason is not taste: the first revision of this file
// constructed the JWKS URL as `issuer + "/jwks"` instead of using the discovery
// document's `jwks_uri`, ignored the JWK's own algorithm, and could trust a cached key
// indefinitely after a failed refresh. Those are the exact classes of defect that a
// maintained library already got right, so the trust boundary is drawn at the library
// and the POLICY stays here.
//
// Scope, deliberately narrow: this establishes that a token really was issued by the
// configured issuer, for the configured audience, inside its validity window, signed
// with an algorithm we allow. It does NOT decide what the caller may do. Organization
// membership, role, task ownership, approval jurisdiction and separation of duties
// remain kernel records. Authentication answers "who", never "what".

// ErrUnauthenticated is returned for every verification failure. Callers must not
// distinguish these to a client — a precise reason is a probe oracle.
var ErrUnauthenticated = errors.New("unauthenticated")

type OIDCConfig struct {
	Issuer   string
	Audience string
	// MaxTokenAge bounds how old a token's issue time may be, catching long-lived
	// tokens even when exp is distant. go-oidc does not enforce this, so we do.
	MaxTokenAge time.Duration
	// AllowInsecureIssuer permits a plain-http issuer. Only for hermetic tests against
	// a loopback issuer; it is refused for any other host.
	AllowInsecureIssuer bool
}

// Claims is the verified subset we consume. Everything else is ignored rather than
// trusted, so a richer token cannot widen authority.
type Claims struct {
	Issuer   string
	Subject  string
	Audience []string
	Expiry   time.Time
	IssuedAt time.Time
	Name     string
	Email    string
}

// allowedAlgs is the signing-algorithm allowlist. It is OUR list, never the token's —
// accepting whatever the token advertises is the classic "alg" substitution hole.
var allowedAlgs = []string{"ES384"}

type OIDCVerifier struct {
	cfg OIDCConfig

	// The provider is created lazily so constructing a verifier performs no network
	// I/O, which keeps the constructor usable in tests and at config-parse time.
	mu sync.Mutex
	v  *oidc.IDTokenVerifier

	// client serves discovery and JWKS fetches. It is a DEDICATED client rather than
	// http.DefaultClient because the default client has NO timeout, so a hung or slow
	// issuer would stall a verification indefinitely. Setting this field is what makes
	// the bound real; tests may replace it to inject a transport.
	//
	// (An earlier revision also claimed the shared connection pool caused intermittent
	// failures. That was tested and disproven -- a per-verifier transport failed the same
	// way. The real cause was a malformed fixture key. The comment is corrected rather
	// than deleted so the wrong theory is not silently re-adopted.)
	client *http.Client
}

func NewOIDCVerifier(cfg OIDCConfig) (*OIDCVerifier, error) {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	if cfg.Issuer == "" {
		return nil, errors.New("OIDC issuer is required")
	}
	if cfg.Audience == "" {
		return nil, errors.New("OIDC audience is required")
	}
	if cfg.MaxTokenAge <= 0 {
		cfg.MaxTokenAge = time.Hour
	}
	if err := checkIssuerScheme(cfg.Issuer, cfg.AllowInsecureIssuer); err != nil {
		return nil, err
	}
	return &OIDCVerifier{cfg: cfg}, nil
}

// checkIssuerScheme refuses a plaintext issuer except on loopback, so a misconfigured
// production deployment cannot silently verify tokens fetched over http.
func checkIssuerScheme(issuer string, allowInsecure bool) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" {
		return errors.New("OIDC issuer is not a valid absolute URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if allowInsecure && loopback {
		return nil
	}
	return errors.New("OIDC issuer must use https (http is permitted only for a loopback issuer during tests)")
}

// verifier returns the library verifier, creating it from the issuer's discovery
// document on first use.
func (o *OIDCVerifier) verifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.v != nil {
		return o.v, nil
	}
	if o.client == nil {
		o.client = &http.Client{Timeout: 15 * time.Second}
	}
	// Supplying the client through the context is how go-oidc is told to use it; without
	// this it silently falls back to http.DefaultClient and the timeout above does
	// nothing. The RemoteKeySet captures this context when it is created, so the same
	// client also serves later JWKS fetches.
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, o.client), o.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	o.v = provider.Verifier(&oidc.Config{
		// Audience must match exactly. A token minted for another API is not usable
		// here even though its signature is valid.
		ClientID: o.cfg.Audience,
		// Pinned allowlist; anything else in the token header is refused.
		SupportedSigningAlgs: append([]string(nil), allowedAlgs...),
	})
	return o.v, nil
}

// Verify checks a raw token and returns its verified claims.
func (o *OIDCVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	v, err := o.verifier(ctx)
	if err != nil {
		return Claims{}, ErrUnauthenticated
	}
	tok, err := v.Verify(ctx, strings.TrimSpace(raw))
	if err != nil {
		// The library's specific reason is not propagated: it would be a probe oracle.
		return Claims{}, ErrUnauthenticated
	}

	now := time.Now()
	// A token must state when it was issued. Without iat the age bound below cannot be
	// applied, so an absent iat is refused rather than treated as "fresh".
	if tok.IssuedAt.IsZero() {
		return Claims{}, ErrUnauthenticated
	}
	// A wild clock is not an accident we should absorb silently.
	if tok.IssuedAt.After(now.Add(2 * time.Minute)) {
		return Claims{}, ErrUnauthenticated
	}
	if now.After(tok.IssuedAt.Add(o.cfg.MaxTokenAge)) {
		return Claims{}, ErrUnauthenticated
	}

	var extra struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	_ = tok.Claims(&extra)

	return Claims{
		Issuer:   tok.Issuer,
		Subject:  tok.Subject,
		Audience: tok.Audience,
		Expiry:   tok.Expiry,
		IssuedAt: tok.IssuedAt,
		Name:     extra.Name,
		Email:    extra.Email,
	}, nil
}
