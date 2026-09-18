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
	// UIOrigin is where the browser is sent after a completed login. It is required in
	// practice: the callback lands on the API origin, which serves no user interface, so
	// without this the user would finish authenticating and arrive at a blank API route.
	// Empty means no redirection (the browser stays on the API origin).
	UIOrigin string
	// PostLogoutURL is where the provider returns the browser after an end-session call.
	PostLogoutURL string
	// Cloudflare Access service-token headers for the protected issuer. They are held only
	// by the backend and never sent to the browser. Empty means the issuer is not behind
	// Access or that the deployment is not configured for this boundary.
	AccessClientID     string
	AccessClientSecret string
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
		AccessClientID:      b.AccessClientID,
		AccessClientSecret:  b.AccessClientSecret,
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
		// Bearer verification and the interactive BFF browser flow are both wired. The
		// issuer may sit behind Cloudflare Access, so the protected service-token headers
		// are carried for discovery and JWKS as well as for the OAuth exchange.
		c.OIDC = OIDCConfig{
			Issuer:             strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_ISSUER")),
			Audience:           strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_AUDIENCE")),
			AccessClientID:     strings.TrimSpace(os.Getenv("WORKFORCE_LOGTO_CF_ACCESS_CLIENT_ID")),
			AccessClientSecret: strings.TrimSpace(os.Getenv("WORKFORCE_LOGTO_CF_ACCESS_CLIENT_SECRET")),
		}
		// Validated here so a misconfigured issuer or audience fails at startup rather
		// than on the first request.
		if _, err := NewOIDCVerifier(c.OIDC); err != nil {
			return Config{}, fmt.Errorf("AUTH_MODE=oidc: %w", err)
		}

		c.Browser = BrowserConfig{
			ClientID:           strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_CLIENT_ID")),
			ClientSecret:       strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_CLIENT_SECRET")),
			RedirectURL:        strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_REDIRECT_URL")),
			PostLogoutURL:      strings.TrimSpace(os.Getenv("WORKFORCE_OIDC_POST_LOGOUT_URL")),
			UIOrigin:           strings.TrimRight(strings.TrimSpace(os.Getenv("WORKFORCE_UI_ORIGIN")), "/"),
			AccessClientID:     strings.TrimSpace(os.Getenv("WORKFORCE_LOGTO_CF_ACCESS_CLIENT_ID")),
			AccessClientSecret: strings.TrimSpace(os.Getenv("WORKFORCE_LOGTO_CF_ACCESS_CLIENT_SECRET")),
			CookieSecure:       cookieSecureFromEnv(),
		}
		// An unusable UI origin is refused at startup rather than producing a redirect to a
		// malformed URL after a successful login, which is both confusing and a way to point
		// freshly issued sessions at an unintended host.
		if c.Browser.UIOrigin != "" && !strings.HasPrefix(c.Browser.UIOrigin, "http://") && !strings.HasPrefix(c.Browser.UIOrigin, "https://") {
			return Config{}, errors.New("WORKFORCE_UI_ORIGIN must be an absolute http(s) origin")
		}
		if env == "production" && strings.HasPrefix(c.Browser.UIOrigin, "http://") {
			return Config{}, errors.New("production requires an https WORKFORCE_UI_ORIGIN")
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
