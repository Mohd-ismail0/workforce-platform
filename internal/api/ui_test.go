package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workforce.local/platform/internal/platform"
)

// uiHandler builds a server whose UI is served from dir (empty = API only).
func uiHandler(t *testing.T, dir string) http.Handler {
	t.Helper()
	f := newFixture(t)
	cfg := platform.Config{
		AuthMode: "local",
		Tokens: map[string]platform.Identity{
			"req": {ID: "org-fixture-a-requester", OrgID: "org-fixture-a", Role: "requester", Name: "Requester"},
		},
		UIDir: dir,
	}
	return New(cfg, f.st).Handler()
}

func writeUI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app shell</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('asset')"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The API must never answer with HTML. Returning an index page for an unknown
// API path would look like a routing success while the caller silently got the
// wrong thing.
func TestUIServingNeverSwallowsTheAPI(t *testing.T) {
	h := uiHandler(t, writeUI(t))

	// A real API route still works with the UI mounted.
	code, body := do(h, "GET", "/health", nil)
	if code != 200 || body["status"] != "ok" {
		t.Fatalf("health with UI mounted: %d %v", code, body)
	}
	// An unknown API path is JSON, not the SPA.
	code, body = do(h, "GET", "/api/v1/definitely-not-a-route", nil)
	if code != 404 {
		t.Fatalf("unknown API path: got %d want 404", code)
	}
	if _, isJSON := body["error"]; !isJSON {
		t.Fatalf("unknown API path did not return the JSON error envelope: %v", body)
	}
}

// A client route is not a file, and the app resolves it once loaded. A real
// asset must still be served as itself.
func TestUIServingFallsBackToTheAppShell(t *testing.T) {
	h := uiHandler(t, writeUI(t))

	code, raw := rawDo(h, "GET", "/")
	if code != 200 || !strings.Contains(raw, "app shell") {
		t.Fatalf("root: %d %q", code, raw)
	}
	// A deep client route resolves to the shell rather than a 404.
	code, raw = rawDo(h, "GET", "/tasks/abc-123")
	if code != 200 || !strings.Contains(raw, "app shell") {
		t.Fatalf("client route: %d %q", code, raw)
	}
	// A genuine asset is served as the asset, not replaced by the shell.
	code, raw = rawDo(h, "GET", "/app.js")
	if code != 200 || !strings.Contains(raw, "console.log") {
		t.Fatalf("asset: %d %q", code, raw)
	}
	// The shell must not be served for a write method.
	if code, _ := rawDo(h, "POST", "/tasks/abc-123"); code != 405 {
		t.Fatalf("POST to a client route: got %d want 405", code)
	}
}

// The static handler reads the filesystem, so a crafted path must not escape the
// configured directory.
func TestUIServingRefusesPathTraversal(t *testing.T) {
	h := uiHandler(t, writeUI(t))
	for _, p := range []string{"/../../etc/passwd", "/..%2f..%2fetc/passwd", "/./../../etc/hosts"} {
		code, raw := rawDo(h, "GET", p)
		if strings.Contains(raw, "root:") || strings.Contains(raw, "localhost") {
			t.Fatalf("traversal served a file outside the UI directory for %q: %q", p, raw)
		}
		if code == 200 && strings.Contains(raw, "app shell") {
			// Falling back to the shell is acceptable; serving /etc/passwd is not.
			continue
		}
	}
}

// With no UI directory the binary serves only the API: unknown paths are a plain
// 404 rather than a page claiming the UI is missing.
func TestNoUIDirectoryMeansAPIModeOnly(t *testing.T) {
	h := uiHandler(t, "")
	if code, _ := rawDo(h, "GET", "/"); code != 404 {
		t.Fatalf("API-only mode served something at the root: %d", code)
	}
	if code, _ := rawDo(h, "GET", "/tasks/abc"); code != 404 {
		t.Fatalf("API-only mode served a client route: %d", code)
	}
	if code, body := do(h, "GET", "/health", nil); code != 200 || body["status"] != "ok" {
		t.Fatalf("API-only mode broke health: %d %v", code, body)
	}
}

// A configured directory with no build in it says so plainly, instead of a 404
// that reads like a routing bug.
func TestEmptyUIDirectoryIsReportedPlainly(t *testing.T) {
	h := uiHandler(t, t.TempDir())
	code, raw := rawDo(h, "GET", "/")
	if code != 503 {
		t.Fatalf("empty UI directory: got %d want 503", code)
	}
	if !strings.Contains(raw, "WORKFORCE_UI_DIR") {
		t.Fatalf("the message does not tell an operator how to fix it: %q", raw)
	}
}

// --- helpers ---------------------------------------------------------------

func do(h http.Handler, method, target string, body any) (int, map[string]any) {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, target, strings.NewReader(string(b)))
	}
	r.Header.Set("Authorization", "Bearer req")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return w.Code, v
}

func rawDo(h http.Handler, method, target string) (int, string) {
	r := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}
