package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/store"
)

// Browser (BFF) authentication: login, callback, logout, and session authorisation.
//
// The browser holds ONE cookie: an opaque random session id. It contains no claims, so
// nothing in it can be edited into authority. Everything the platform needs — organisation,
// role, and whether the account is still active — is read from the database on every
// request, which means revocation takes effect immediately rather than when a cookie
// happens to expire.
//
// Two cookie properties are load-bearing rather than boilerplate:
//
//   * HttpOnly, so a script cannot read the session id. That is also what keeps the
//     response to any XSS from being "full account takeover".
//   * A CSRF synchroniser token in addition to SameSite. Cookies are scoped to the HOST,
//     not the port, so a session cookie set by the API is automatically attached to
//     requests from the SPA's own origin; SameSite mitigates cross-SITE requests but says
//     nothing about a same-site request the user did not intend. The header token is what
//     makes a state-changing request deliberate.

const (
	// sessionCookieName is the single browser credential.
	sessionCookieName = "__Host-workforce_session"
	// loginTTL bounds how long a pending login may sit before it is refused. Short, because
	// it only has to survive the redirect round trip.
	loginTTL = 10 * time.Minute
	// csrfHeaderName carries the synchroniser token on state-changing requests.
	csrfHeaderName = "X-CSRF-Token"
)

// bff is the browser-auth surface. It is nil unless the server was configured for OIDC, so
// the absence of configuration cannot be mistaken for a permissive mode.
type bff struct {
	oauth *platform.OAuthClient
	store *store.Store
	// secureCookies is false only for a plain-http local development origin. In production
	// it must be true; the check lives in config, not here.
	secureCookies bool
	// postLogoutURL is where the provider returns the browser after signing out.
	postLogoutURL string
}

// authError renders a failure to the browser without revealing which check failed.
//
// The reason is logged, never returned: telling a caller whether an account is unlinked,
// deactivated or merely unlucky turns the endpoint into a probe oracle.
func (s *Server) authError(w http.ResponseWriter, reason string) {
	log.Printf("auth: refusing request: %s", reason)
	http.Error(w, "authentication failed", http.StatusUnauthorized)
}

// handleLogin starts an interactive login.
//
// GET /auth/login?return_to=/tasks
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.bff == nil {
		s.authError(w, "oidc not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state, err := platform.NewOpaqueToken()
	if err != nil {
		s.authError(w, "randomness unavailable")
		return
	}
	nonce, err := platform.NewOpaqueToken()
	if err != nil {
		s.authError(w, "randomness unavailable")
		return
	}
	verifier, err := platform.NewPKCEVerifier()
	if err != nil {
		s.authError(w, "randomness unavailable")
		return
	}
	returnTo := platform.SafeReturnTo(r.URL.Query().Get("return_to"))

	// Only the HASH of state is stored: the raw value lives in the browser, so a database
	// read cannot be used to forge a login.
	if err := s.bff.store.CreateLoginTransaction(r.Context(), platform.HashToken(state),
		verifier, nonce, returnTo, loginTTL); err != nil {
		s.authError(w, "could not record the login transaction")
		return
	}

	http.Redirect(w, r, s.bff.oauth.AuthCodeURL(state, nonce, platform.PKCEChallenge(verifier)),
		http.StatusFound)
}

// handleCallback completes a login.
//
// GET /auth/callback?code=...&state=...
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.bff == nil {
		s.authError(w, "oidc not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	// The provider may report a refusal instead of a code; that is a normal outcome and must
	// not be treated as a server error.
	if e := q.Get("error"); e != "" {
		s.authError(w, "provider refused: "+e)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		s.authError(w, "callback is missing code or state")
		return
	}

	// Single-use claim. This is what makes a replayed callback fail: the row can only move
	// from unconsumed to consumed once, so two concurrent callbacks cannot both mint a
	// session.
	tx, err := s.bff.store.ConsumeLoginTransaction(r.Context(), platform.HashToken(state))
	if err != nil {
		s.authError(w, "login transaction unusable")
		return
	}

	claims, err := s.bff.oauth.Exchange(r.Context(), code, tx.Verifier, tx.Nonce)
	if err != nil {
		s.authError(w, "code exchange or id token verification failed")
		return
	}

	// Authentication succeeded; authority is separate. An identity with no explicit link is
	// refused here, and there is no auto-provisioning: authenticating is not an entitlement.
	identity, err := s.bff.store.ResolveOIDCIdentity(r.Context(), claims.Issuer, claims.Subject)
	if err != nil {
		s.authError(w, "no platform identity is linked to this account")
		return
	}
	if identity.OrgID == "" {
		s.authError(w, "identity has no organisation")
		return
	}

	csrf, err := platform.NewOpaqueToken()
	if err != nil {
		s.authError(w, "randomness unavailable")
		return
	}
	rawSession, err := s.bff.store.CreateSession(r.Context(), identity.OrgID, identity.ID,
		claims.Issuer, claims.Subject, csrf, s.bff.oauth.SessionTTL())
	if err != nil {
		s.authError(w, "could not create a session")
		return
	}

	s.setSessionCookie(w, rawSession, int(s.bff.oauth.SessionTTL().Seconds()))
	// The CSRF token goes to the page, NOT in an HttpOnly cookie: the client must be able to
	// read it and echo it back in a header, which is what proves the request was deliberate.
	s.setCSRFCookie(w, csrf, int(s.bff.oauth.SessionTTL().Seconds()))

	http.Redirect(w, r, platform.SafeReturnTo(tx.ReturnTo), http.StatusFound)
}

// handleLogout ends the session.
//
// POST /auth/logout — CSRF-protected, and POST rather than GET so it cannot be triggered by
// an image tag or a prefetch.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.bff == nil {
		s.authError(w, "oidc not configured")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := s.sessionCookie(r)
	if err == nil {
		sess, _, rerr := s.bff.store.ReadSession(r.Context(), raw)
		if rerr == nil && !s.csrfOK(r, sess.CSRFToken) {
			http.Error(w, "csrf check failed", http.StatusForbidden)
			return
		}
		// Revocation is a tombstone, so reuse of this cookie fails afterwards. Logout is
		// idempotent: an already-invalid session is not an error worth reporting, because
		// doing so would confirm whether the cookie was valid.
		_ = s.bff.store.RevokeSession(r.Context(), raw)
	}
	s.clearSessionCookie(w)
	s.setCSRFCookie(w, "", -1)

	// Only send the browser to the provider's end-session endpoint if one is advertised.
	// Otherwise local logout is all that happened, and claiming a full sign-out would be
	// false.
	if target := s.bff.oauth.LogoutURL(s.bff.postLogoutURL); target != "" {
		jsonWrite(w, http.StatusOK, map[string]string{"logout_url": target})
		return
	}
	jsonWrite(w, http.StatusOK, map[string]string{
		"status": "logged out locally",
		"detail": "the identity provider advertises no end-session endpoint, so only this session was ended",
	})
}

// authorize resolves the caller from EITHER a bearer token or a browser session cookie.
//
// The third return value says which, because the two are not equally trustworthy in one
// specific way: a browser attaches a cookie automatically to any request it makes,
// including one the user did not intend, whereas a bearer token must be set explicitly by
// the caller. That difference is exactly what CSRF exists to cover, so the caller of this
// function must know which path was taken.
func (s *Server) authorize(r *http.Request) (platform.Identity, store.Session, bool, error) {
	// A bearer token takes precedence when present: an API client that presents one is
	// making an explicit statement about how it wants to authenticate.
	if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(h, "Bearer ") {
		id, err := s.auth(r)
		return id, store.Session{}, false, err
	}
	if id, sess, ok := s.sessionIdentity(r); ok {
		return id, sess, true, nil
	}
	return platform.Identity{}, store.Session{}, false, errors.New("no credentials")
}

// stateChanging reports whether a method can alter state. GET/HEAD/OPTIONS are excluded
// because they are expected to be safe; note that this is a real constraint on the API's
// design, not a convention -- an endpoint that changes state on GET would escape CSRF
// protection entirely.
func stateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// handleSession reports the current browser session.
//
// GET /auth/session
//
// It returns the identity and the CSRF token the client must echo back. It deliberately
// does NOT return the session cookie, and it distinguishes "no session" (200 with
// authenticated:false) from "not configured" (401): the SPA needs the first to decide
// whether to show a login link, and the second is a deployment fault.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.bff == nil {
		s.authError(w, "oidc not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	identity, sess, ok := s.sessionIdentity(r)
	if !ok {
		jsonWrite(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	jsonWrite(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"identity":      identity,
		"csrf_token":    sess.CSRFToken,
		"expires_at":    sess.ExpiresAt,
	})
}

// sessionCookie resolves a request's browser session into a platform identity.
//
// It returns ok=false when there is no usable session, so a caller cannot accidentally treat
// "no session" as "anonymous but allowed".
func (s *Server) sessionIdentity(r *http.Request) (platform.Identity, store.Session, bool) {
	if s.bff == nil {
		return platform.Identity{}, store.Session{}, false
	}
	raw, err := s.sessionCookie(r)
	if err != nil {
		return platform.Identity{}, store.Session{}, false
	}
	// ReadSession re-reads the principal, so a deactivated employee is refused immediately
	// even though their cookie is still cryptographically fine.
	sess, identity, err := s.bff.store.ReadSession(r.Context(), raw)
	if err != nil {
		return platform.Identity{}, store.Session{}, false
	}
	return identity, sess, true
}

func (s *Server) sessionCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", errors.New("no session cookie")
	}
	return c.Value, nil
}

// csrfOK compares the header token against the session's, in constant time.
func (s *Server) csrfOK(r *http.Request, want string) bool {
	if want == "" {
		return false
	}
	return platform.TokensEqual(strings.TrimSpace(r.Header.Get(csrfHeaderName)), want)
}

// setSessionCookie writes the session cookie.
//
// Secure is set from configuration and is required in production. SameSite=Lax rather than
// Strict because the callback arrives as a top-level cross-site navigation, and Strict would
// drop the cookie on the way back in.
func (s *Server) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: value, Path: "/",
		HttpOnly: true, Secure: s.bff.secureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func (s *Server) setCSRFCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: "workforce_csrf", Value: value, Path: "/",
		// Readable by the client on purpose: it must be echoed in a header. Not HttpOnly,
		// because that would make the synchroniser token unusable.
		HttpOnly: false, Secure: s.bff.secureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: s.bff.secureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}
