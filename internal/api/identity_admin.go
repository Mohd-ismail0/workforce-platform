package api

import (
	"net/http"
	"strings"

	"workforce.local/platform/internal/platform"
)

// Identity onboarding over the authenticated API.
//
// Authority here comes from the database and nothing else. The caller's role is read from
// the principal row on every request (see ResolveOIDCIdentity -> ResolveIdentity), so a
// token cannot claim onboarding rights it was never granted, and revoking an operator takes
// effect on their next request rather than when their token expires.
//
// The first administrator cannot be created here — that is the one step with no authenticated
// caller available yet, and it lives in the operator CLI (`cmd/workforce -bootstrap-identity`).
// Everything after it is this file.

// requireOperator refuses unless the resolved principal is an administrator of its own org.
//
// It returns false after writing the response, so callers can simply return.
func (s *Server) requireOperator(w http.ResponseWriter, id platform.Identity) bool {
	if id.OrgID == "" {
		failCode(w, 403, "forbidden", "organization required")
		return false
	}
	if id.Role != "admin" {
		// Deliberately uniform: whether the caller is a requester or an approver is not
		// information an unauthorised caller needs.
		failCode(w, 403, "forbidden", "identity administration requires an administrator")
		return false
	}
	return true
}

// handleIdentityLinks serves GET /identity/links and POST /identity/links.
func (s *Server) handleIdentityLinks(w http.ResponseWriter, r *http.Request, id platform.Identity) {
	if !s.requireOperator(w, id) {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		// Scoped to the caller's own organization. identity_links carries no tenant RLS
		// policy, so this filter is explicit rather than implied — without it an operator
		// would see every tenant's external accounts.
		x, e := s.store.ListIdentityLinks(ctx, id.OrgID)
		respond(w, x, e)
	case http.MethodPost:
		var q struct {
			Issuer      string `json:"issuer"`
			Subject     string `json:"subject"`
			PrincipalID string `json:"principal_id"`
		}
		if decode(r, &q) != nil {
			failCode(w, 422, "invalid_request", "issuer, subject and principal_id are required")
			return
		}
		q.Issuer, q.Subject = strings.TrimSpace(q.Issuer), strings.TrimSpace(q.Subject)
		q.PrincipalID = strings.TrimSpace(q.PrincipalID)
		if q.Issuer == "" || q.Subject == "" || q.PrincipalID == "" {
			failCode(w, 422, "invalid_request", "issuer, subject and principal_id are required")
			return
		}
		if e := s.store.LinkIdentityByAdmin(ctx, id.OrgID, q.Issuer, q.Subject, q.PrincipalID, id.ID); e != nil {
			fail(w, e)
			return
		}
		jsonWrite(w, http.StatusCreated, map[string]any{
			"issuer": q.Issuer, "subject": q.Subject, "principal_id": q.PrincipalID, "active": true,
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		failCode(w, 405, "method_not_allowed", "method not allowed")
	}
}

// handleIdentityUnlink serves POST /identity/links/unlink.
//
// POST rather than DELETE because it carries a body and must be CSRF-checked as a
// state-changing request; it also ends sessions, so it must not be reachable by a prefetch
// or an image tag.
func (s *Server) handleIdentityUnlink(w http.ResponseWriter, r *http.Request, id platform.Identity) {
	if !s.requireOperator(w, id) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		failCode(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	var q struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
	}
	if decode(r, &q) != nil {
		failCode(w, 422, "invalid_request", "issuer and subject are required")
		return
	}
	q.Issuer, q.Subject = strings.TrimSpace(q.Issuer), strings.TrimSpace(q.Subject)
	if q.Issuer == "" || q.Subject == "" {
		failCode(w, 422, "invalid_request", "issuer and subject are required")
		return
	}
	revoked, e := s.store.UnlinkIdentityByAdmin(r.Context(), id.OrgID, q.Issuer, q.Subject, id.ID)
	if e != nil {
		fail(w, e)
		return
	}
	jsonWrite(w, 200, map[string]any{"status": "unlinked", "sessions_revoked": revoked})
}
