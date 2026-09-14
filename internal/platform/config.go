package platform

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Identity struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	Role  string `json:"role"`
	Name  string `json:"name"`
}
type Config struct {
	AuthMode    string
	Tokens      map[string]Identity
	DatabaseURL string
	Address     string
	// OIDC is populated only in oidc mode. It carries the trust anchors the verifier
	// needs; the verifier itself performs NO network I/O at construction, so loading
	// config stays cheap and testable.
	OIDC OIDCConfig
	// Browser configures the interactive BFF login. It is separate from OIDC because the
	// two have DIFFERENT audiences: OIDC.Audience is the API resource indicator (for bearer
	// clients), while the ID token's audience is the application client id below.
	Browser BrowserConfig
}

// BrowserConfig is the confidential backend-for-frontend client.
//
// The backend performs the code exchange and holds the secret; the browser never receives
// one. Session state is server-side.
type BrowserConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// PostLogoutURL is where the provider returns the browser after an end-session call.
	PostLogoutURL string
	// CookieSecure must be true in production. It is a configuration decision rather than
	// inferred from the request, because inferring it would let a proxied plaintext request
	// silently downgrade the cookie.
	CookieSecure bool
	// AllowInsecureIssuer permits a plain-http loopback issuer. It is NEVER set from the
	// environment; only a test constructs it, so it cannot be enabled in a deployment by
	// setting a variable.
	AllowInsecureIssuer bool
	// AllowedAlgs overrides the ID-token signing-algorithm allowlist.
	AllowedAlgs []string
}

// Enabled reports whether the browser flow is configured at all. Partial configuration is
// treated as DISABLED rather than half-working: a login that starts and cannot finish is
// worse than one that never starts.
func (b BrowserConfig) Enabled() bool {
	return b.ClientID != "" && b.ClientSecret != "" && b.RedirectURL != ""
}

// OAuth assembles the client. The issuer comes from the OIDC block so there is exactly one
// issuer in the configuration rather than two that can disagree.
func (b BrowserConfig) OAuth(issuer string) OAuthConfig {
	return OAuthConfig{
		Issuer:       issuer,
		ClientID:     b.ClientID,
		ClientSecret: b.ClientSecret,
		RedirectURL:  b.RedirectURL,
		// Passed through, but only a test ever sets it true: LoadConfig never reads it from
		// the environment, and checkIssuerScheme still refuses anything but loopback.
		AllowInsecureIssuer: b.AllowInsecureIssuer,
		AllowedAlgs:         append([]string(nil), b.AllowedAlgs...),
	}
}

func LoadConfig() (Config, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE")))
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if mode == "" {
		return Config{}, errors.New("AUTH_MODE must be explicitly set; configure OIDC for non-local deployments")
	}
	if env == "production" && mode != "oidc" {
		return Config{}, errors.New("production refuses local authentication; configure AUTH_MODE=oidc")
	}
	if mode != "local" && mode != "oidc" {
		return Config{}, fmt.Errorf("unsupported AUTH_MODE %q", mode)
	}
	c := Config{AuthMode: mode, Tokens: map[string]Identity{}, DatabaseURL: os.Getenv("DATABASE_URL"), Address: os.Getenv("HTTP_ADDR")}
	if mode == "oidc" {
		// Bounded scope of this mode today: it verifies a bearer token and resolves the
		// verified (issuer, subject) to a platform principal. The INTERACTIVE browser
		// flow — authorization code + PKCE, server-side sessions, cookies, CSRF — is
		// not wired yet, so oidc mode currently serves API clients that present a
		// token. Production refuses local auth (checked above) either way.
		c.OIDC = OIDCConfig{
			Issuer:   strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_ISSUER")),
			Audience: strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_AUDIENCE")),
		}
		// Validated here so a misconfigured issuer or audience fails at startup rather
		// than on the first request.
		if _, err := NewOIDCVerifier(c.OIDC); err != nil {
			return Config{}, fmt.Errorf("AUTH_MODE=oidc: %w", err)
		}

		c.Browser = BrowserConfig{
			ClientID:      strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_CLIENT_ID")),
			ClientSecret:  strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_CLIENT_SECRET")),
			RedirectURL:   strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_REDIRECT_URL")),
			PostLogoutURL: strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_POST_LOGOUT_URL")),
			CookieSecure:  cookieSecureFromEnv(),
		}
		// A plaintext session cookie in production would let anyone on the path read a
		// credential that grants the whole account. Refused at startup rather than warned
		// about, because a warning is easy to leave in place.
		if env == "production" && !c.Browser.CookieSecure {
			return Config{}, errors.New("production requires secure session cookies; set WORKFORCE_COOKIE_SECURE=true")
		}
	}
	if c.Address == "" {
		c.Address = ":8080"
	}
	if mode == "local" {
		raw := strings.TrimSpace(os.Getenv("LOCAL_AUTH_TOKENS"))
		if raw == "" {
			return Config{}, errors.New("LOCAL_AUTH_TOKENS is required in local mode")
		}
		for _, entry := range strings.Split(raw, ",") {
			p := strings.SplitN(entry, "=", 2)
			if len(p) != 2 || p[0] == "" {
				return Config{}, errors.New("LOCAL_AUTH_TOKENS entries must be token=org:id:role[:name]")
			}
			f := strings.Split(p[1], ":")
			if len(f) < 3 || f[0] == "" || f[1] == "" {
				return Config{}, errors.New("invalid local identity")
			}
			org, name, role := f[0], f[1], f[2]
			if role != "requester" && role != "approver" && role != "admin" {
				return Config{}, errors.New("invalid local role")
			}
			if _, exists := c.Tokens[p[0]]; exists {
				return Config{}, errors.New("duplicate local token")
			}
			c.Tokens[p[0]] = Identity{ID: name, OrgID: org, Role: role, Name: name}
		}
	}
	return c, nil
}

// cookieSecureFromEnv reads WORKFORCE_COOKIE_SECURE, defaulting to TRUE.
//
// The default is secure because the failure modes are asymmetric: a secure cookie over a
// plaintext development origin is an obvious, immediate inconvenience, whereas an insecure
// cookie in production silently exposes a full session to anyone on the network path.
// Only an explicit "false" turns it off.
func cookieSecureFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WORKFORCE_COOKIE_SECURE")))
	return v != "false" && v != "0" && v != "no"
}

func NewID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
