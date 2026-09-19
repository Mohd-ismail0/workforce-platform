package api

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// serveUI serves the built single-page app from the SAME origin as the API.
//
// Why this exists: the browser identity flow uses an opaque __Host- session
// cookie, which only works when the UI and the API share an origin. Running the
// UI on a separate dev-server port therefore needs a proxy and careful cookie
// configuration, and getting that wrong is what broke sign-in once already. For
// a deployment exposed on one address, one origin is simply fewer things that can
// disagree.
//
// It is deliberately opt-in: with no WORKFORCE_UI_DIR the binary serves only the
// API. A server should not pretend to have a UI it was not given.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	// The API must never answer with HTML. An unknown /api/ path is a client
	// mistake, and returning an index page would make it look like a routing
	// success while the caller silently got the wrong thing.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		failCode(w, 404, "not_found", "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		failCode(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	dir := s.cfg.UIDir
	if dir == "" {
		uiNotBuilt(w)
		return
	}
	// Clean the request path before touching the filesystem. A path such as
	// /../../etc/passwd collapses here, and the prefix check below is a second
	// guard rather than the only one.
	rel := path.Clean("/" + r.URL.Path)
	full := filepath.Join(dir, filepath.FromSlash(rel))
	base := filepath.Clean(dir)
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		failCode(w, 404, "not_found", "not found")
		return
	}

	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	// SPA fallback: a client route such as /tasks/123 is not a file on disk, and
	// the app resolves it once loaded. Reached only when the file is genuinely
	// absent, so a real asset typo still surfaces as the app's own 404.
	index := filepath.Join(base, "index.html")
	if _, err := os.Stat(index); err != nil {
		uiNotBuilt(w)
		return
	}
	http.ServeFile(w, r, index)
}

// uiNotBuilt says so plainly rather than serving a 404 that reads like a routing
// bug. An operator who forgot to build the UI should be told that, not left to
// guess why every page is missing.
func uiNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w,
		"The user interface is not built into this deployment.\n\n"+
			"Build it with `npm --prefix web run build` and point WORKFORCE_UI_DIR at the output directory.\n")
}
