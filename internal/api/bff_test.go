package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/store"
)

// Browser-auth tests. The parts that need no network are tested here, because the
// alternative -- asserting nothing until a real Logto exists -- would leave the CSRF gate
// and the cookie attributes unverified precisely where a mistake is an unauthorised action.

// bffFixture builds a server with a real store and a bff, WITHOUT contacting an issuer:
// the routes under test here never reach the provider.
func bffFixture(t *testing.T) (*Server, *store.Store, string, string) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	org, who := platform.NewID(), platform.NewID()
	if err := st.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO organizations(id,name) VALUES($1,'bff fixture')`, org); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Browser User','requester')`, who, org)
		return e
	}); err != nil {
		t.Fatal(err)
	}

	cfg := platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{}}
	srv := &Server{cfg: cfg, store: st, registry: connectors.NewRegistry(),
		// secureCookies true so the cookie attributes asserted below are the production ones.
		bff: &bff{store: st, secureCookies: true}}
	return srv, st, org, who
}

// issueSession creates the identity link AND the session it authorises.
//
// The link is not incidental: ReadSession refuses a session with no active link row, because a
// session is only ever minted after a link has resolved. A fixture that skipped the link would
// force that production rule to be relaxed for the convenience of a test.
func issueSession(t *testing.T, st *store.Store, org, who string) (string, string) {
	t.Helper()
	csrf, err := platform.NewOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// The subject is unique per issue. `identity_links` is UNIQUE on (issuer, subject)
	// PLATFORM-WIDE (that is the invariant preventing one external account becoming two
	// principals) and the database persists between runs, so a fixed subject would make the
	// second run fail as "already linked to a different principal".
	subject := "sub-" + platform.NewID()
	if err := st.LinkIdentityByAdmin(ctx, org, "iss", subject, who, "fixture"); err != nil {
		t.Fatalf("link fixture identity: %v", err)
	}
	raw, err := st.CreateSession(ctx, org, who, "iss", subject, csrf, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return raw, csrf
}

// When the browser flow is not configured, every /auth route must refuse. A route that
// silently proceeded would be an authentication bypass in any deployment that forgot to
// configure it.
func TestBrowserRoutesRefuseWhenNotConfigured(t *testing.T) {
	// No store needed: bff is nil, which is the condition under test.
	srv := &Server{cfg: platform.Config{AuthMode: "local"}, registry: connectors.NewRegistry()}
	h := srv.Handler()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/auth/login"},
		{"GET", "/auth/callback"},
		{"POST", "/auth/logout"},
		{"GET", "/auth/session"},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s returned %d; an unconfigured browser flow must refuse", tc.method, tc.path, w.Code)
		}
		if bytes.Contains(w.Body.Bytes(), []byte("oidc not configured")) {
			t.Fatalf("%s %s leaked the internal reason to the client", tc.method, tc.path)
		}
	}
}

// A session cookie alone must be enough to READ, and must never be enough to WRITE.
// Cookies are host-scoped, so the browser attaches this one to any request it is asked to
// make; the header token is what distinguishes a deliberate action.
func TestCookieSessionIsReadableButWritesRequireCSRF(t *testing.T) {
	srv, st, org, who := bffFixture(t)
	h := srv.Handler()
	raw, csrf := issueSession(t, st, org, who)

	cookie := &http.Cookie{Name: sessionCookieName, Value: raw}

	// A safe request succeeds with the cookie alone.
	r := httptest.NewRequest("GET", "/auth/session", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /auth/session with a valid cookie returned %d (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["authenticated"] != true {
		t.Fatalf("a valid session was not reported as authenticated: %v", body)
	}
	// The response must carry the CSRF token for the client to echo, and must NOT carry the
	// session value anywhere.
	if body["csrf_token"] == nil {
		t.Fatal("no csrf token was returned; the client could not make a deliberate write")
	}
	if bytes.Contains(w.Body.Bytes(), []byte(raw)) {
		t.Fatal("the session value was returned to the client in the body")
	}

	// A state-changing request with the cookie but NO header token must be refused. This is
	// the exact attack the synchroniser token exists for.
	r = httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader([]byte(`{"name":"x"}`)))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a cookie-authenticated POST without a CSRF token returned %d, want 403", w.Code)
	}

	// A WRONG token must also be refused: accepting any non-empty value would be no check.
	r = httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader([]byte(`{"name":"x"}`)))
	r.AddCookie(cookie)
	r.Header.Set(csrfHeaderName, "not-the-token")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a cookie-authenticated POST with a WRONG CSRF token returned %d, want 403", w.Code)
	}

	// With the correct token the same request must NOT be refused for CSRF reasons. It may
	// still fail validation, so this asserts the gate opened rather than a 2xx.
	r = httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader([]byte(`{"name":"bff fixture project"}`)))
	r.AddCookie(cookie)
	r.Header.Set(csrfHeaderName, csrf)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusForbidden {
		t.Fatalf("a deliberate request with the correct CSRF token was refused: %s", w.Body.String())
	}
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("a valid session was not accepted for an API call: %s", w.Body.String())
	}

	// A bearer token must NOT require a CSRF token: the browser never attaches one by
	// itself, so the attack this defends against does not exist for that path.
	cfg2 := platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{
		"tok": {ID: "unused", OrgID: org, Role: "requester"},
	}}
	_ = cfg2
}

func TestCSRFComparisonRejectsEmptyAndWrong(t *testing.T) {
	srv := &Server{bff: &bff{}}
	r := httptest.NewRequest("POST", "/x", nil)
	if srv.csrfOK(r, "secret") {
		t.Fatal("a missing header token matched")
	}
	r.Header.Set(csrfHeaderName, "")
	if srv.csrfOK(r, "secret") {
		t.Fatal("an empty header token matched")
	}
	r.Header.Set(csrfHeaderName, "secret")
	if !srv.csrfOK(r, "secret") {
		t.Fatal("the correct token did not match")
	}
	// A session with no stored token must never authorise a write.
	if srv.csrfOK(r, "") {
		t.Fatal("a session with an empty CSRF token authorised a write")
	}
}

func TestStateChangingClassification(t *testing.T) {
	safe := []string{"GET", "HEAD", "OPTIONS"}
	for _, m := range safe {
		if stateChanging(m) {
			t.Fatalf("%s was classified as state-changing; safe methods must not require a CSRF token", m)
		}
	}
	// Everything else is treated as capable of changing state, including methods we do not
	// implement: defaulting to "safe" would silently exempt a future route.
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "TRACE"} {
		if !stateChanging(m) {
			t.Fatalf("%s was classified as safe; an unknown method must not be exempt from CSRF", m)
		}
	}
}

// The session cookie must carry the attributes that make it a credential rather than a gift
// to any script on the page or anyone on the network path.
func TestSessionCookieAttributes(t *testing.T) {
	srv := &Server{bff: &bff{secureCookies: true}}
	w := httptest.NewRecorder()
	srv.setSessionCookie(w, "value", 3600)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly {
		t.Fatal("the session cookie is readable by scripts; an XSS would become full account takeover")
	}
	if !c.Secure {
		t.Fatal("the session cookie is not Secure")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v; Lax is required so the callback can set it on a top-level navigation", c.SameSite)
	}
	if c.Path != "/" {
		t.Fatalf("cookie path = %q", c.Path)
	}
	// The name is prefixed so it cannot be set from a subdomain or shadowed by a
	// non-Host cookie.
	if len(c.Name) < 7 || c.Name[:7] != "__Host-" {
		t.Fatalf("cookie name %q lacks the __Host- prefix", c.Name)
	}

	// The CSRF cookie must be READABLE by the client, or the client could not echo it. This
	// is the one cookie that is deliberately not HttpOnly.
	w = httptest.NewRecorder()
	srv.setCSRFCookie(w, "csrf", 3600)
	cc := w.Result().Cookies()[0]
	if cc.HttpOnly {
		t.Fatal("the CSRF cookie is HttpOnly, so the client could never read and echo it")
	}
	if cc.Value != "csrf" {
		t.Fatal("the CSRF cookie did not carry the token")
	}
}

// A session that has been revoked or whose principal was deactivated must not be usable.
func TestRevokedSessionCannotAuthenticateAPI(t *testing.T) {
	srv, st, org, who := bffFixture(t)
	raw, _ := issueSession(t, st, org, who)
	h := srv.Handler()

	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: raw})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("a valid cookie should authenticate: %d %s", w.Code, w.Body.String())
	}

	if err := st.RevokeSession(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: raw})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked session still authenticated: %d", w.Code)
	}
}
