package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/store"
)

type Server struct {
	cfg      platform.Config
	store    *store.Store
	registry *connectors.Registry
	// oidc verifies bearer tokens in oidc mode. It is built at construction; the
	// verifier performs no network I/O until the first token arrives, so startup does
	// not depend on the issuer being reachable.
	oidc *platform.OIDCVerifier
	// bff drives the interactive browser login. It is non-nil ONLY when the browser flow
	// is actually configured, so its absence can never be mistaken for a permissive mode:
	// every /auth route refuses when it is nil.
	bff *bff
}

func New(c platform.Config, st *store.Store) *Server {
	s := &Server{cfg: c, store: st, registry: connectors.NewRegistry()}
	if c.AuthMode == "oidc" {
		if v, err := platform.NewOIDCVerifier(c.OIDC); err == nil {
			s.oidc = v
		}
		// Discovery is performed here, so a misconfigured issuer or client fails at
		// startup rather than on a colleague's first login attempt. A failure is logged
		// and the browser routes stay disabled instead of booting half-configured.
		if c.Browser.Enabled() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			oc, err := platform.NewOAuthClient(ctx, c.Browser.OAuth(c.OIDC.Issuer))
			if err != nil {
				log.Printf("browser login is DISABLED: %v", err)
			} else {
				s.bff = &bff{
					oauth:         oc,
					store:         st,
					secureCookies: c.Browser.CookieSecure,
					postLogoutURL: c.Browser.PostLogoutURL,
					uiOrigin:      c.Browser.UIOrigin,
				}
			}
		}
	}
	return s
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { jsonWrite(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			failCode(w, 503, "not_ready", "database unavailable")
			return
		}
		if e := s.store.Pool.Ping(r.Context()); e != nil {
			failCode(w, 503, "not_ready", "database unavailable")
			return
		}
		jsonWrite(w, 200, map[string]string{"status": "ready"})
	})
	// Browser (BFF) routes. Each refuses when the flow is not configured.
	m.HandleFunc("/auth/login", s.handleLogin)
	m.HandleFunc("/auth/callback", s.handleCallback)
	m.HandleFunc("/auth/logout", s.handleLogout)
	m.HandleFunc("/auth/session", s.handleSession)
	m.HandleFunc("/api/v1/", s.api)
	return m
}

// auth turns a request into an authority-bearing identity.
//
// Authentication establishes WHO the caller is; authority (org, role, ownership,
// jurisdiction) comes from kernel rows and is never taken from a token. Both modes end
// at a principal row, so a token cannot mint authority that the database does not
// already grant.
func (s *Server) auth(r *http.Request) (platform.Identity, error) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(h, "Bearer ") {
		return platform.Identity{}, errors.New("missing bearer token")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	switch s.cfg.AuthMode {
	case "local":
		// Dev-only. Production refuses this mode at config load.
		id, ok := s.cfg.Tokens[raw]
		if !ok {
			return platform.Identity{}, errors.New("invalid token")
		}
		actual, err := s.store.ResolveIdentity(r.Context(), id)
		if err != nil {
			return platform.Identity{}, errors.New("identity unavailable or revoked")
		}
		return actual, nil
	case "oidc":
		if s.oidc == nil {
			return platform.Identity{}, errors.New("authentication is not configured")
		}
		claims, err := s.oidc.Verify(r.Context(), raw)
		if err != nil {
			return platform.Identity{}, errors.New("identity could not be verified")
		}
		// A verified token with NO linked principal is refused. There is no
		// auto-provisioning and no email-based linking: authenticating successfully is
		// not the same as being entitled to act.
		actual, err := s.store.ResolveOIDCIdentity(r.Context(), claims.Issuer, claims.Subject)
		if err != nil {
			return platform.Identity{}, errors.New("identity could not be verified")
		}
		return actual, nil
	default:
		return platform.Identity{}, errors.New("authentication is not configured")
	}
}
func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	id, sess, viaCookie, e := s.authorize(r)
	if e != nil {
		// Deliberately generic: telling a caller whether a token was malformed, expired,
		// unlinked or revoked turns the endpoint into a probe oracle.
		failCode(w, 401, "unauthenticated", "authentication failed")
		return
	}
	// A browser attaches the session cookie automatically, including to a request the user
	// did not intend to make. A state-changing request authenticated that way must
	// therefore carry the synchroniser token as proof it was deliberate. A bearer token
	// needs no such proof, because the browser never attaches one by itself.
	if viaCookie && stateChanging(r.Method) && !s.csrfOK(r, sess.CSRFToken) {
		failCode(w, 403, "csrf_failed", "request could not be verified as deliberate")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if path == "/me" {
		jsonWrite(w, 200, id)
		return
	}
	if id.OrgID == "" {
		failCode(w, 403, "forbidden", "organization required")
		return
	}
	ctx := r.Context()
	switch {
	case path == "/projects" && r.Method == "GET":
		x, e := s.store.ListProjects(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/projects" && r.Method == "POST":
		var q struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if decode(r, &q) != nil || q.Name == "" {
			failCode(w, 422, "invalid_request", "name is required")
			return
		}
		x, e := s.store.CreateProject(ctx, id.OrgID, q.Name, q.Description)
		if e == nil {
			jsonWrite(w, 201, x)
		} else {
			fail(w, e)
		}
	case path == "/tasks" && r.Method == "GET":
		x, e := s.store.ListTasks(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/tasks" && r.Method == "POST":
		var q struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			AssigneeID  string `json:"assignee_id"`
			ProjectID   string `json:"project_id"`
		}
		if decode(r, &q) != nil || q.Title == "" {
			failCode(w, 422, "invalid_request", "title is required")
			return
		}
		x, e := s.store.CreateTask(ctx, id.OrgID, q.Title, q.Description, id.ID, q.AssigneeID, q.ProjectID)
		if e == nil {
			jsonWrite(w, 201, x)
		} else {
			fail(w, e)
		}
	case path == "/agents" && r.Method == "GET":
		x, e := s.store.ListAgents(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/agents" && r.Method == "POST":
		var q struct {
			Name         string   `json:"name"`
			Harness      string   `json:"harness"`
			OwnerID      string   `json:"owner_id"`
			Capabilities []string `json:"capabilities"`
		}
		if decode(r, &q) != nil || q.Name == "" || q.Harness == "" {
			failCode(w, 422, "invalid_request", "name and harness are required")
			return
		}
		owner := q.OwnerID
		if owner == "" {
			owner = id.ID
		}
		x, e := s.store.CreateAgent(ctx, id.OrgID, q.Name, q.Harness, owner, q.Capabilities)
		if e == nil {
			jsonWrite(w, 201, x)
		} else {
			fail(w, e)
		}
	case path == "/registry/releases" && r.Method == "GET":
		x, e := s.store.ListRegistryReleases(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/registry/releases" && r.Method == "POST":
		var q store.RegistryCreate
		if decode(r, &q) != nil || q.Family == "" || q.Kind == "" || q.Version == "" || q.Digest == "" {
			failCode(w, 422, "invalid_request", "family, kind, version, and digest are required")
			return
		}
		if q.Kind != "connector" && q.Kind != "harness" && q.Kind != "renderer" && q.Kind != "automation" {
			failCode(w, 422, "invalid_request", "unsupported registry kind")
			return
		}
		x, e := s.store.CreateRegistryRelease(ctx, id.OrgID, id.ID, q)
		if e == nil {
			jsonWrite(w, 201, x)
		} else {
			fail(w, e)
		}
	case strings.HasPrefix(path, "/registry/releases/") && r.Method == "GET":
		p := strings.Split(strings.Trim(path, "/"), "/")
		if len(p) != 3 {
			failCode(w, 404, "not_found", "not found")
			return
		}
		x, e := s.store.GetRegistryRelease(ctx, id.OrgID, p[2])
		if e == nil {
			jsonWrite(w, 200, x)
		} else {
			fail(w, e)
		}
	case strings.HasPrefix(path, "/registry/releases/") && strings.HasSuffix(path, "/transition") && r.Method == "POST":
		p := strings.Split(strings.Trim(path, "/"), "/")
		if len(p) != 4 {
			failCode(w, 404, "not_found", "not found")
			return
		}
		var q struct {
			ExpectedVersion int64  `json:"expected_version"`
			TargetState     string `json:"target_state"`
			Reason          string `json:"reason"`
		}
		if decode(r, &q) != nil || q.ExpectedVersion < 1 || q.TargetState == "" {
			failCode(w, 422, "invalid_request", "expected_version and target_state are required")
			return
		}
		x, e := s.store.TransitionRegistryRelease(ctx, id.OrgID, p[2], id.ID, id.Role, q.TargetState, q.Reason, q.ExpectedVersion)
		if e == nil {
			jsonWrite(w, 200, x)
		} else {
			fail(w, e)
		}
	case strings.HasPrefix(path, "/registry/releases/") && strings.HasSuffix(path, "/grant") && r.Method == "POST":
		p := strings.Split(strings.Trim(path, "/"), "/")
		if len(p) != 4 {
			failCode(w, 404, "not_found", "not found")
			return
		}
		var q struct {
			ExpectedVersion int64    `json:"expected_version"`
			Capabilities    []string `json:"capabilities"`
		}
		if decode(r, &q) != nil || q.ExpectedVersion < 1 || len(q.Capabilities) == 0 {
			failCode(w, 422, "invalid_request", "expected_version and capabilities are required")
			return
		}
		x, e := s.store.GrantRegistryCapabilities(ctx, id.OrgID, id.ID, id.Role, p[2], q.Capabilities, q.ExpectedVersion)
		if e == nil {
			jsonWrite(w, 200, x)
		} else {
			fail(w, e)
		}
	case path == "/capabilities/check" && r.Method == "POST":
		// "What would be denied" BEFORE anything is launched.
		var q struct {
			Harness      string   `json:"harness"`
			Capabilities []string `json:"capabilities"`
		}
		if decode(r, &q) != nil || q.Harness == "" {
			failCode(w, 422, "invalid_request", "harness is required")
			return
		}
		x, e := s.store.CheckCapabilities(ctx, id.OrgID, q.Harness, q.Capabilities)
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, x)
	case path == "/integrations" && r.Method == "GET":
		jsonWrite(w, 200, map[string]any{"items": s.registry.List()})
	case strings.HasPrefix(path, "/integrations/") && strings.HasSuffix(path, "/records") && r.Method == "GET":
		parts := strings.Split(path, "/")
		x, e := s.store.ListRecords(ctx, id.OrgID, parts[2])
		respond(w, x, e)
	case path == "/gates" && r.Method == "GET":
		x, e := s.store.ListGates(ctx, id.OrgID, id.ID)
		respond(w, x, e)
	case strings.HasPrefix(path, "/gates/") && strings.HasSuffix(path, "/respond") && r.Method == "POST":
		parts := strings.Split(strings.Trim(path, "/"), "/")
		var q struct {
			Revision int64           `json:"revision"`
			Response json.RawMessage `json:"response"`
		}
		if decode(r, &q) != nil {
			failCode(w, 422, "invalid_request", "revision and response are required")
			return
		}
		x, e := s.store.ResolveGate(ctx, id.OrgID, parts[1], id.ID, q.Revision, q.Response)
		if e == nil {
			jsonWrite(w, 200, x)
		} else if errors.Is(e, store.ErrGateNotFound) {
			failCode(w, 404, "not_found", "gate not found")
		} else if errors.Is(e, store.ErrGateNotRespondent) {
			failCode(w, 403, "forbidden", "not the gate respondent")
		} else if errors.Is(e, store.ErrGateInvalidResponse) {
			failCode(w, 422, "invalid_response", "response does not match the requested input")
		} else {
			failCode(w, 409, "conflict", "gate has changed")
		}
	case strings.HasPrefix(path, "/gates/") && r.Method == "GET":
		x, e := s.store.ReadGate(ctx, id.OrgID, strings.TrimPrefix(path, "/gates/"), id.ID)
		if e == nil {
			jsonWrite(w, 200, x)
		} else {
			failCode(w, 404, "not_found", "gate not found")
		}
	case path == "/runs" && r.Method == "GET":
		x, e := s.store.ListAgentRuns(ctx, id.OrgID)
		respond(w, x, e)
	case strings.HasPrefix(path, "/runs/") && r.Method == "GET":
		x, e := s.store.GetAgentRun(ctx, id.OrgID, strings.TrimPrefix(path, "/runs/"))
		if e == nil {
			jsonWrite(w, 200, x)
		} else {
			fail(w, e)
		}
	case path == "/proposals" && r.Method == "GET":
		x, e := s.store.ListProposals(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/decisions" && r.Method == "GET":
		x, e := s.store.ListDecisions(ctx, id.OrgID, id.ID, id.Role)
		respond(w, x, e)
	case path == "/events" && r.Method == "GET":
		x, e := s.store.ListEvents(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/handoffs" && r.Method == "GET":
		x, e := s.store.ListHandoffs(ctx, id.OrgID, id.ID)
		respond(w, x, e)
	case path == "/work/board" && r.Method == "GET":
		// The caller's own board. Identity comes from the authenticated
		// principal, never from a query parameter, so one person cannot ask for
		// another's board.
		x, e := s.store.WorkBoard(ctx, id.OrgID, id.ID)
		respond(w, x, e)
	case path == "/people" && r.Method == "GET":
		x, e := s.store.ListPeople(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/positions" && r.Method == "GET":
		x, e := s.store.ListPositions(ctx, id.OrgID)
		respond(w, x, e)
	case path == "/positions" && r.Method == "POST":
		var q struct {
			Name     string `json:"name"`
			ParentID string `json:"parent_id"`
		}
		if decode(r, &q) != nil || strings.TrimSpace(q.Name) == "" {
			failCode(w, 422, "invalid_request", "a position name is required")
			return
		}
		x, e := s.store.CreatePosition(ctx, id.OrgID, id.ID, q.Name, q.ParentID)
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 201, x)
	case path == "/relationships" && r.Method == "GET":
		// Structure is a question with a time in it. `as_of` defaults to now, so
		// the caller must ask for history deliberately rather than getting it by
		// accident.
		asOf := time.Now()
		if raw := r.URL.Query().Get("as_of"); raw != "" {
			ts, e := time.Parse(time.RFC3339, raw)
			if e != nil {
				failCode(w, 422, "invalid_request", "as_of must be an RFC3339 instant")
				return
			}
			asOf = ts
		}
		x, e := s.store.RelationshipsAsOf(ctx, id.OrgID, asOf)
		respond(w, x, e)
	case path == "/relationships" && r.Method == "POST":
		var q struct {
			SubjectID string `json:"subject_id"`
			Kind      string `json:"kind"`
			ObjectID  string `json:"object_id"`
			From      string `json:"from"`
			To        string `json:"to"`
		}
		if decode(r, &q) != nil || q.SubjectID == "" || q.Kind == "" || q.ObjectID == "" {
			failCode(w, 422, "invalid_request", "subject_id, kind and object_id are required")
			return
		}
		from := time.Now()
		if q.From != "" {
			ts, e := time.Parse(time.RFC3339, q.From)
			if e != nil {
				failCode(w, 422, "invalid_request", "from must be an RFC3339 instant")
				return
			}
			from = ts
		}
		var to *time.Time
		if q.To != "" {
			ts, e := time.Parse(time.RFC3339, q.To)
			if e != nil {
				failCode(w, 422, "invalid_request", "to must be an RFC3339 instant")
				return
			}
			to = &ts
		}
		x, e := s.store.CreateRelationship(ctx, id.OrgID, id.ID, q.SubjectID, q.Kind, q.ObjectID, from, to)
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 201, x)
	case path == "/identity/links":
		s.handleIdentityLinks(w, r, id)
	case path == "/identity/links/unlink":
		s.handleIdentityUnlink(w, r, id)
	case path == "/receipts" && r.Method == "GET":
		x, e := s.store.ListReceipts(ctx, id.OrgID)
		respond(w, x, e)
	default:
		s.routeResource(w, r, id, path)
	}
}
func (s *Server) routeResource(w http.ResponseWriter, r *http.Request, id platform.Identity, path string) {
	p := strings.Split(strings.Trim(path, "/"), "/")
	ctx := r.Context()
	if len(p) >= 2 && p[0] == "tasks" {
		tid := p[1]
		if len(p) == 2 && r.Method == "GET" {
			x, e := s.store.GetTask(ctx, id.OrgID, tid)
			if e == nil {
				jsonWrite(w, 200, x)
			} else {
				fail(w, e)
			}
			return
		}
		if len(p) == 3 && p[2] == "runs" && r.Method == "POST" {
			var q struct {
				AgentID string `json:"agent_id"`
				Intent  string `json:"intent"`
			}
			if decode(r, &q) != nil || q.AgentID == "" {
				failCode(w, 422, "invalid_request", "agent_id is required")
				return
			}
			x, e := s.store.CreateAgentRun(ctx, id.OrgID, tid, id.ID, q.AgentID, q.Intent)
			if e == nil {
				jsonWrite(w, 201, x)
			} else if errors.As(e, &store.ErrHarnessUnsupported{}) {
				failCode(w, 409, "harness_unsupported", "the configured harness is unsupported by this binary")
			} else if errors.As(e, &store.ErrNoActiveHarness{}) {
				failCode(w, 409, "no_active_harness", "an active harness release is required in the registry")
			} else {
				fail(w, e)
			}
			return
		}
		if len(p) == 3 && p[2] == "dependencies" && r.Method == "POST" {
			var q struct {
				ParentID string `json:"parent_id"`
			}
			if decode(r, &q) != nil || q.ParentID == "" {
				failCode(w, 422, "invalid_request", "parent_id is required")
				return
			}
			if e := s.store.AddDependency(ctx, id.OrgID, tid, q.ParentID); e != nil {
				fail(w, e)
			} else {
				jsonWrite(w, 201, map[string]string{"status": "created"})
			}
			return
		}
		if len(p) == 3 && p[2] == "cancel" && r.Method == "POST" {
			var q struct {
				ExpectedVersion int64 `json:"expected_version"`
			}
			if decode(r, &q) != nil {
				failCode(w, 422, "invalid_request", "expected_version is required")
				return
			}
			if e := s.store.CancelTask(ctx, id.OrgID, tid, q.ExpectedVersion); e != nil {
				fail(w, e)
			} else {
				jsonWrite(w, 200, map[string]string{"status": "cancelled"})
			}
			return
		}
		if len(p) == 3 && p[2] == "proposals" && r.Method == "POST" {
			var q struct {
				Summary    string                 `json:"summary"`
				Operations []connectors.Operation `json:"operations"`
			}
			if decode(r, &q) != nil || len(q.Operations) == 0 {
				failCode(w, 422, "invalid_request", "operations are required")
				return
			}
			x, e := s.store.CreateProposal(ctx, id.OrgID, tid, id.ID, q.Summary, q.Operations)
			if e == nil {
				jsonWrite(w, 201, x)
			} else {
				fail(w, e)
			}
			return
		}
		if len(p) == 3 && p[2] == "handoffs" && r.Method == "POST" {
			var q struct {
				RecipientID string `json:"recipient_id"`
				Role        string `json:"role"`
				Summary     string `json:"summary"`
			}
			if decode(r, &q) != nil || q.RecipientID == "" || q.Role == "" {
				failCode(w, 422, "invalid_request", "recipient_id and role are required")
				return
			}
			x, e := s.store.CreateHandoff(ctx, id.OrgID, tid, id.ID, q.RecipientID, q.Role, q.Summary)
			if e == nil {
				jsonWrite(w, 201, x)
			} else {
				fail(w, e)
			}
			return
		}
	}
	if len(p) >= 2 && p[0] == "proposals" {
		pid := p[1]
		if len(p) == 2 && r.Method == "GET" {
			x, e := s.store.GetProposal(ctx, id.OrgID, pid)
			if e == nil {
				jsonWrite(w, 200, x)
			} else {
				fail(w, e)
			}
			return
		}
		if len(p) == 3 && (p[2] == "endorse" || p[2] == "approve" || p[2] == "reject") && r.Method == "POST" {
			var q struct {
				Revision int64  `json:"revision"`
				Digest   string `json:"digest"`
				Reason   string `json:"reason"`
			}
			if decode(r, &q) != nil || q.Revision < 1 || q.Digest == "" {
				failCode(w, 422, "invalid_request", "revision and digest are required")
				return
			}
			if e := s.store.Decide(ctx, id.OrgID, pid, id.ID, id.Role, p[2], q.Revision, q.Digest, q.Reason); e != nil {
				fail(w, e)
			} else {
				jsonWrite(w, 200, map[string]string{"status": "accepted"})
			}
			return
		}
	}
	// Milestones: planning that is separate from doing.
	if len(p) == 3 && p[0] == "projects" && p[2] == "milestones" {
		switch r.Method {
		case "GET":
			x, e := s.store.ListMilestones(ctx, id.OrgID, p[1])
			respond(w, x, e)
			return
		case "POST":
			var q struct {
				Name               string `json:"name"`
				AcceptanceEvidence string `json:"acceptance_evidence"`
				CommittedDate      string `json:"committed_date"`
				ForecastDate       string `json:"forecast_date"`
			}
			if decode(r, &q) != nil {
				failCode(w, 422, "invalid_request", "a request body is required")
				return
			}
			// Required fields are a validation failure (422), not a conflict: the
			// request is malformed, not in tension with current state.
			if strings.TrimSpace(q.Name) == "" || strings.TrimSpace(q.AcceptanceEvidence) == "" {
				failCode(w, 422, "invalid_request", "name and acceptance_evidence are required")
				return
			}
			committed, ok := parseDay(w, q.CommittedDate)
			if !ok {
				return
			}
			forecast, ok := parseDay(w, q.ForecastDate)
			if !ok {
				return
			}
			x, e := s.store.CreateMilestone(ctx, id.OrgID, id.ID, p[1], q.Name, q.AcceptanceEvidence, committed, forecast)
			if e != nil {
				fail(w, e)
				return
			}
			jsonWrite(w, 201, x)
			return
		}
	}
	if len(p) == 3 && p[0] == "milestones" && r.Method == "POST" {
		switch p[2] {
		case "complete":
			var q struct {
				Evidence string `json:"evidence"`
				Version  int64  `json:"version"`
			}
			if decode(r, &q) != nil || strings.TrimSpace(q.Evidence) == "" {
				failCode(w, 422, "invalid_request", "evidence and version are required")
				return
			}
			if e := s.store.CompleteMilestone(ctx, id.OrgID, id.ID, p[1], q.Evidence, q.Version); e != nil {
				fail(w, e)
				return
			}
			jsonWrite(w, 200, map[string]string{"status": "met"})
			return
		case "forecast":
			var q struct {
				ForecastDate string `json:"forecast_date"`
			}
			_ = decode(r, &q)
			forecast, ok := parseDay(w, q.ForecastDate)
			if !ok {
				return
			}
			x, e := s.store.SetMilestoneForecast(ctx, id.OrgID, id.ID, p[1], forecast)
			if e != nil {
				fail(w, e)
				return
			}
			jsonWrite(w, 200, x)
			return
		case "dependencies":
			var q struct {
				ParentID string `json:"parent_id"`
			}
			if decode(r, &q) != nil || q.ParentID == "" {
				failCode(w, 422, "invalid_request", "parent_id is required")
				return
			}
			if e := s.store.AddMilestoneDependency(ctx, id.OrgID, p[1], q.ParentID); e != nil {
				fail(w, e)
				return
			}
			jsonWrite(w, 201, map[string]string{"status": "linked"})
			return
		}
	}
	if len(p) == 3 && p[0] == "agents" && p[2] == "configuration" && r.Method == "GET" {
		// Effective configuration: what the agent declares, the ceiling its
		// harness permits, anything outside that ceiling, and the headroom left.
		x, e := s.store.AgentConfiguration(ctx, id.OrgID, p[1])
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, x)
		return
	}
	if len(p) == 3 && p[0] == "agents" && p[2] == "board" && r.Method == "GET" {
		// One agent's current state: what it is reasoning about, what is queued,
		// and what is parked waiting on a person. Terminal runs are omitted.
		x, e := s.store.AgentBoard(ctx, id.OrgID, p[1])
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, x)
		return
	}
	if len(p) == 3 && p[0] == "relationships" && p[2] == "end" && r.Method == "POST" {
		var q struct {
			At string `json:"at"`
		}
		_ = decode(r, &q)
		at := time.Now()
		if q.At != "" {
			ts, e := time.Parse(time.RFC3339, q.At)
			if e != nil {
				failCode(w, 422, "invalid_request", "at must be an RFC3339 instant")
				return
			}
			at = ts
		}
		if e := s.store.EndRelationship(ctx, id.OrgID, id.ID, p[1], at); e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, map[string]string{"status": "ended"})
		return
	}
	if len(p) == 2 && p[0] == "handoffs" && r.Method == "GET" {
		x, e := s.store.PeekHandoff(ctx, id.OrgID, p[1], id.ID)
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, x)
		return
	}
	if len(p) == 3 && p[0] == "handoffs" && p[2] == "accept" && r.Method == "POST" {
		if e := s.store.AcceptHandoff(ctx, id.OrgID, p[1], id.ID); e != nil {
			fail(w, e)
		} else {
			jsonWrite(w, 200, map[string]string{"status": "accepted"})
		}
		return
	}
	// Handoff lifecycle replies. Each reads its own note from the body: the
	// recipient declines with a reason or asks a question; the creator withdraws
	// with a reason. Who may take each action is enforced in the store, not here.
	if len(p) == 3 && p[0] == "handoffs" && r.Method == "POST" &&
		(p[2] == "decline" || p[2] == "clarify" || p[2] == "cancel") {
		var q struct {
			Reason string `json:"reason"`
		}
		if decode(r, &q) != nil {
			failCode(w, 422, "invalid_request", "a reason is required")
			return
		}
		var e error
		switch p[2] {
		case "decline":
			if strings.TrimSpace(q.Reason) == "" {
				failCode(w, 422, "invalid_request", "a decline reason is required")
				return
			}
			e = s.store.DeclineHandoff(ctx, id.OrgID, p[1], id.ID, q.Reason)
		case "clarify":
			if strings.TrimSpace(q.Reason) == "" {
				failCode(w, 422, "invalid_request", "a clarification question is required")
				return
			}
			e = s.store.ClarifyHandoff(ctx, id.OrgID, p[1], id.ID, q.Reason)
		case "cancel":
			e = s.store.CancelHandoff(ctx, id.OrgID, p[1], id.ID, q.Reason)
		}
		if e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, 200, map[string]string{"status": p[2] + "d"})
		return
	}
	failCode(w, 404, "not_found", "not found")
}
func respond(w http.ResponseWriter, x any, e error) {
	if e != nil {
		fail(w, e)
		return
	}
	jsonWrite(w, 200, map[string]any{"items": x})
}
func jsonWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	if errors.Is(e, store.ErrNotFound) {
		failCode(w, 404, "not_found", "resource not found")
		return
	}
	failCode(w, 409, "conflict", safeMessage(e))
}
func safeMessage(e error) string {
	m := e.Error()
	for _, x := range []string{"SQLSTATE", "select ", "insert ", "update ", "violates", "constraint"} {
		if strings.Contains(strings.ToLower(m), strings.ToLower(x)) {
			return "request could not be completed"
		}
	}
	return m
}
func failCode(w http.ResponseWriter, status int, code, msg string) {
	jsonWrite(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// parseDay reads a calendar date (YYYY-MM-DD); empty means "not set". A bad
// value is refused rather than silently dropped, because a plan that quietly
// loses a date is worse than one that says the date was unreadable.
func parseDay(w http.ResponseWriter, raw string) (*time.Time, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, true
	}
	t, e := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if e != nil {
		failCode(w, 422, "invalid_request", "dates must be YYYY-MM-DD")
		return nil, false
	}
	return &t, true
}
