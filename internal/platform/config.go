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
func NewID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
