package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OAuth authorization-code flow with PKCE, for the browser (BFF) client.
//
// TWO DIFFERENT AUDIENCES, and conflating them is a real bug rather than a detail:
//
//   * The ID token's audience is the APPLICATION CLIENT ID. It proves "this login happened
//     for our application". That is what the callback verifies.
//   * An API access token's audience is the API RESOURCE INDICATOR (https://workforce.internal/api).
//     It authorises calls to the kernel API, and it is what OIDCConfig.Audience covers for
//     bearer clients.
//
// A single "audience" setting would have to be wrong for one of the two, so they are
// separate fields on separate paths.

type OAuthConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes defaults to openid/profile/email. `offline_access` is deliberately NOT
	// requested: nothing refreshes a token yet, and asking for a refresh token we cannot
	// store or rotate would be worse than not asking.
	Scopes []string
	// AllowInsecureIssuer is for hermetic tests against a loopback issuer only.
	AllowInsecureIssuer bool
	// AllowedAlgs is the ID-token signing-algorithm allowlist. Empty means ES384, which is
	// what this deployment advertises for ID tokens.
	AllowedAlgs []string
	// SessionTTL bounds a browser session.
	SessionTTL time.Duration
}

// OAuthClient drives the interactive login.
type OAuthClient struct {
	cfg      OAuthConfig
	client   *http.Client
	provider *oidc.Provider
	endpoint oauth2.Endpoint
	verifier *oidc.IDTokenVerifier
}

func NewOAuthClient(ctx context.Context, cfg OAuthConfig) (*OAuthClient, error) {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	if cfg.Issuer == "" {
		return nil, errors.New("OIDC issuer is required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("OIDC client id is required (the ID token audience)")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("OIDC client secret is required for a confidential client")
	}
	if !strings.HasPrefix(cfg.RedirectURL, "http://") && !strings.HasPrefix(cfg.RedirectURL, "https://") {
		return nil, errors.New("OIDC redirect URL must be absolute")
	}
	if err := checkIssuerScheme(cfg.Issuer, cfg.AllowInsecureIssuer); err != nil {
		return nil, err
	}
	if err := ValidateAlgs(cfg.AllowedAlgs); err != nil {
		return nil, err
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	// A dedicated client, not http.DefaultClient: the default has no timeout, so a hung
	// issuer would stall a login indefinitely.
	client := &http.Client{Timeout: 15 * time.Second}
	ctxWithClient := oidc.ClientContext(ctx, client)

	// The insecure-issuer context is REQUIRED for a plain-http loopback issuer, and its
	// absence was a real defect: AllowInsecureIssuer passed validation but was then
	// ignored here, so a hermetic issuer could never actually be used and the option was
	// fiction. A configuration knob that is silently dropped is worse than no knob.
	if cfg.AllowInsecureIssuer {
		ctxWithClient = oidc.InsecureIssuerURLContext(ctxWithClient, cfg.Issuer)
	}

	provider, err := oidc.NewProvider(ctxWithClient, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering the issuer: %w", err)
	}
	return &OAuthClient{
		cfg:      cfg,
		client:   client,
		provider: provider,
		endpoint: provider.Endpoint(),
		// The audience here is the CLIENT ID. This is the login-time check, and it is a
		// DIFFERENT audience from the API resource indicator used by bearer clients.
		verifier: provider.Verifier(&oidc.Config{
			ClientID:             cfg.ClientID,
			SupportedSigningAlgs: OIDCConfig{AllowedAlgs: cfg.AllowedAlgs}.algs(),
		}),
	}, nil
}

// AuthCodeURL builds the authorization request.
//
// Response type and PKCE method are set explicitly rather than left to defaults, so the
// request cannot silently degrade to implicit flow or to "plain" PKCE.
func (c *OAuthClient) AuthCodeURL(state, nonce, codeChallenge string) string {
	return c.oauth2Config().AuthCodeURL(state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (c *OAuthClient) oauth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		Endpoint:     c.endpoint,
		RedirectURL:  c.cfg.RedirectURL,
		Scopes:       c.cfg.Scopes,
	}
}

// Exchange trades an authorization code for tokens and returns the VERIFIED ID token claims.
//
// The code verifier must be the one bound to this transaction: sending a different one is
// rejected by the issuer, which is what makes PKCE a defence against an intercepted code.
func (c *OAuthClient) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (Claims, error) {
	if code == "" {
		return Claims{}, errors.New("authorization code is required")
	}
	if err := ValidatePKCEVerifier(codeVerifier); err != nil {
		return Claims{}, fmt.Errorf("code verifier: %w", err)
	}

	ctxWithClient := oidc.ClientContext(ctx, c.client)
	token, err := c.oauth2Config().Exchange(ctxWithClient, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	if err != nil {
		return Claims{}, fmt.Errorf("code exchange failed: %w", err)
	}

	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Claims{}, errors.New("the token response contained no id_token")
	}
	// Verified against the CLIENT ID as audience, and with the pinned algorithm allowlist.
	tok, err := c.verifier.Verify(ctxWithClient, rawID)
	if err != nil {
		return Claims{}, errors.New("the id token could not be verified")
	}

	// The nonce binds this ID token to the login we started, which is what prevents a token
	// minted for another session being replayed into this one.
	var nonce struct {
		Nonce string `json:"nonce"`
	}
	if err := tok.Claims(&nonce); err != nil {
		return Claims{}, errors.New("the id token claims could not be read")
	}
	if expectedNonce == "" || !TokensEqual(nonce.Nonce, expectedNonce) {
		return Claims{}, errors.New("the id token does not match this login")
	}

	now := time.Now()
	// Absent iat means the age bound below is unenforceable, so it is refused rather than
	// treated as fresh. A future iat is refused too: a wild clock is not something to
	// absorb silently.
	if tok.IssuedAt.IsZero() || tok.IssuedAt.After(now.Add(2*time.Minute)) ||
		now.After(tok.IssuedAt.Add(time.Hour)) {
		return Claims{}, errors.New("the id token's issue time is not acceptable")
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

// LogoutURL returns the issuer's end-session endpoint when it publishes one, so logging out
// can end the session at the identity provider rather than only locally. An empty string
// means the provider advertises none, in which case local logout is all that is possible
// and the caller must say so rather than implying a full sign-out.
func (c *OAuthClient) LogoutURL(postLogoutRedirect string) string {
	var meta struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := c.provider.Claims(&meta); err != nil || meta.EndSessionEndpoint == "" {
		return ""
	}
	u, err := url.Parse(meta.EndSessionEndpoint)
	if err != nil {
		return ""
	}
	q := u.Query()
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// SessionTTL exposes the configured session lifetime.
func (c *OAuthClient) SessionTTL() time.Duration { return c.cfg.SessionTTL }

// ClientID exposes the configured client id, which is the ID-token audience.
func (c *OAuthClient) ClientID() string { return c.cfg.ClientID }
