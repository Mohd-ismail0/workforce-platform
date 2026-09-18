package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Identity onboarding: the administrative direction of identity_links, plus the one
// operation that cannot be authenticated — bootstrapping the first administrator.

// IdentityLink is one (issuer, subject) -> principal mapping as an operator sees it.
type IdentityLink struct {
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"`
	OrgID       string    `json:"org_id"`
	PrincipalID string    `json:"principal_id"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Active      bool      `json:"active"`
}

// auditTx appends one domain event inside an existing transaction.
//
// Auditing shares the caller's transaction on purpose: an onboarding change and the record
// that it happened must commit together, or the trail can claim an act the database refused.
//
// The aggregate version is COMPUTED, not hardcoded. `domain_events` carries a uniqueness
// constraint over (org_id, aggregate_id, aggregate_version, event_type), so writing a fixed
// version makes the second legitimate event about the same subject fail outright — which is
// exactly what an idempotent re-run of an onboarding command does. Incrementing per subject
// keeps re-runs working and leaves the trail in order.
func auditTx(ctx context.Context, tx pgx.Tx, org, aggregateID, eventType, actor string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO domain_events(id, org_id, aggregate_id, aggregate_version, event_type, actor_id, payload)
		VALUES ($1, $2, $3,
		        (SELECT coalesce(max(aggregate_version), 0) + 1
		           FROM domain_events WHERE org_id = $2 AND aggregate_id = $3),
		        $4, $5, $6)`,
		platform.NewID(), org, aggregateID, eventType, actor, b)
	return err
}

// ListIdentityLinks returns the links belonging to ONE organization.
//
// identity_links carries no tenant RLS policy (resolution happens before the tenant is
// known), so this org filter is explicit and load-bearing rather than implied: without it
// an operator would see every tenant's external accounts.
func (s *Store) ListIdentityLinks(ctx context.Context, orgID string) ([]IdentityLink, error) {
	if orgID == "" {
		return nil, errors.New("org is required")
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT issuer, subject, org_id, principal_id, created_at, created_by, active
		  FROM identity_links
		 WHERE org_id = $1
		 ORDER BY created_at DESC, subject`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdentityLink{}
	for rows.Next() {
		var l IdentityLink
		if err := rows.Scan(&l.Issuer, &l.Subject, &l.OrgID, &l.PrincipalID,
			&l.CreatedAt, &l.CreatedBy, &l.Active); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// linkIdentityInTx binds an external identity to a principal INSIDE a caller's transaction.
//
// This is the single source of truth for the uniqueness rule, and it takes a transaction so
// callers can compose it with their own audit write. Two separate transactions would allow a
// committed change with no record of it, which is exactly what an audit trail must not permit.
func linkIdentityInTx(ctx context.Context, tx pgx.Tx, orgID, issuer, subject, principalID, createdBy string) error {
	// The principal must exist here. The foreign key enforces this, but a clear error beats a
	// constraint violation.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT true FROM principals WHERE org_id = $1 AND id = $2`,
		orgID, principalID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("principal does not exist in this organization")
		}
		return err
	}

	var existing string
	err := tx.QueryRow(ctx, `
		SELECT principal_id FROM identity_links WHERE issuer = $1 AND subject = $2`,
		issuer, subject).Scan(&existing)
	switch {
	case err == nil && existing == principalID:
		// Idempotent: the same binding already exists. Re-activate if it was deactivated, so a
		// documented re-link is possible and explicit.
		_, e := tx.Exec(ctx, `
			UPDATE identity_links SET active = true
			 WHERE issuer = $1 AND subject = $2 AND NOT active`, issuer, subject)
		return e
	case err == nil && existing != principalID:
		return errors.New("this external account is already linked to a different principal")
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO identity_links (issuer, subject, org_id, principal_id, created_by)
		VALUES ($1, $2, $3, $4, $5)`,
		issuer, subject, orgID, principalID, createdBy)
	return err
}

// principalInTx reads a principal through a caller's transaction.
func principalInTx(ctx context.Context, tx pgx.Tx, orgID, principalID string) (platform.Identity, error) {
	var id platform.Identity
	err := tx.QueryRow(ctx,
		`SELECT id, org_id, role, name FROM principals WHERE org_id = $1 AND id = $2 AND active`,
		orgID, principalID).Scan(&id.ID, &id.OrgID, &id.Role, &id.Name)
	return id, err
}

// countActiveLinksInTx counts live external accounts for one principal inside a transaction.
// Unlinking refuses to remove the last one from an active administrator, which would lock the
// organization out of its own onboarding surface with no authenticated way back in.
func countActiveLinksInTx(ctx context.Context, tx pgx.Tx, orgID, principalID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM identity_links
		 WHERE org_id = $1 AND principal_id = $2 AND active`,
		orgID, principalID).Scan(&n)
	return n, err
}

// LinkIdentityByAdmin binds an external account to a principal, with an audit record.
//
// Authority is NOT evaluated here: the caller is responsible for having resolved the
// requesting administrator from the database. This function enforces the data invariants and
// commits the change and its audit record together.
func (s *Store) LinkIdentityByAdmin(ctx context.Context, orgID, issuer, subject, principalID, actor string) error {
	if orgID == "" || issuer == "" || subject == "" || principalID == "" || actor == "" {
		return errors.New("org, issuer, subject, principal and actor are required")
	}
	return s.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		if err := linkIdentityInTx(ctx, tx, orgID, issuer, subject, principalID, actor); err != nil {
			return err
		}
		return auditTx(ctx, tx, orgID, principalID, "identity_link.created", actor, map[string]any{
			"issuer": issuer, "subject": subject, "principal_id": principalID,
		})
	})
}

// UnlinkIdentityByAdmin removes a mapping, ends the sessions it authorised, and records it —
// all in ONE transaction.
//
// Two halves, and neither is optional:
//
//  1. Deactivate the link (not delete), so the audit trail still shows this account once
//     mapped here and re-linking stays an explicit act.
//  2. Revoke the sessions that link created. The link is what made those sessions valid, so
//     leaving them live would mean "access removed" did not remove access. ReadSession also
//     re-checks the link on every request, which covers removal by any other path; this call
//     is what gives the operator immediate, observable effect.
func (s *Store) UnlinkIdentityByAdmin(ctx context.Context, orgID, issuer, subject, actor string) (int64, error) {
	if orgID == "" || issuer == "" || subject == "" || actor == "" {
		return 0, errors.New("org, issuer, subject and actor are required")
	}
	var revoked int64
	err := s.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		var principalID string
		if err := tx.QueryRow(ctx, `
			SELECT principal_id FROM identity_links
			 WHERE org_id = $1 AND issuer = $2 AND subject = $3 AND active`,
			orgID, issuer, subject).Scan(&principalID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("no active link found for this external account in this organization")
			}
			return err
		}
		// Read the principal inside this transaction. A pool query here would run with no
		// app.org_id set, so forced RLS would match ZERO rows and this would fail as if the
		// principal did not exist — including for a principal that is right there.
		principal, err := principalInTx(ctx, tx, orgID, principalID)
		if err != nil {
			return err
		}
		n, err := countActiveLinksInTx(ctx, tx, orgID, principalID)
		if err != nil {
			return err
		}
		if principal.Role == "admin" && n <= 1 {
			return errors.New("refusing to unlink the last external account of an active administrator")
		}

		if _, err := tx.Exec(ctx, `
			UPDATE identity_links SET active = false
			 WHERE org_id = $1 AND issuer = $2 AND subject = $3 AND active`,
			orgID, issuer, subject); err != nil {
			return err
		}
		// sessions carries no tenant RLS policy, so this is valid inside the org transaction.
		tag, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at = clock_timestamp()
			 WHERE issuer = $1 AND subject = $2 AND revoked_at IS NULL`, issuer, subject)
		if err != nil {
			return err
		}
		revoked = tag.RowsAffected()
		return auditTx(ctx, tx, orgID, principalID, "identity_link.revoked", actor, map[string]any{
			"issuer": issuer, "subject": subject, "principal_id": principalID,
			"sessions_revoked": revoked,
		})
	})
	if err != nil {
		return 0, err
	}
	return revoked, nil
}

// BootstrapRequest establishes the FIRST administrator of an organization.
type BootstrapRequest struct {
	OrgID       string
	OrgName     string
	PrincipalID string
	// PrincipalName defaults to PrincipalID.
	PrincipalName string
	// Role applies only when the principal does not already exist; an existing principal's
	// role is never silently rewritten by a bootstrap.
	Role    string
	Issuer  string
	Subject string
	Actor   string
}

type BootstrapResult struct {
	OrgCreated       bool `json:"org_created"`
	PrincipalCreated bool `json:"principal_created"`
	Linked           bool `json:"linked"`
}

// BootstrapIdentity performs the one onboarding step that cannot be authenticated.
//
// There is a chicken-and-egg problem at the root of every identity system: linking an
// external account requires an administrator, and the first administrator has no external
// account linked yet. The resolution here is deliberate:
//
//   - it is reachable ONLY from the operator CLI, never from the HTTP API, so it cannot be
//     an escalation path for an anonymous caller;
//   - it requires the exact (issuer, subject) of an already-authenticated identity, so it
//     cannot be used to pre-create access for an account nobody controls;
//   - it never links by email, and it records the human operator as the actor rather than
//     inventing an authenticated administrator;
//   - it is idempotent, so re-running it is safe and a re-link is explicit.
//
// Everything after this first administrator goes through the authenticated admin routes.
func (s *Store) BootstrapIdentity(ctx context.Context, q BootstrapRequest) (BootstrapResult, error) {
	var res BootstrapResult
	q.OrgID = strings.TrimSpace(q.OrgID)
	q.PrincipalID = strings.TrimSpace(q.PrincipalID)
	q.Issuer = strings.TrimSpace(q.Issuer)
	q.Subject = strings.TrimSpace(q.Subject)
	q.Actor = strings.TrimSpace(q.Actor)
	if q.OrgID == "" || q.PrincipalID == "" || q.Issuer == "" || q.Subject == "" || q.Actor == "" {
		return res, errors.New("org, principal, issuer, subject and actor are required")
	}
	if q.PrincipalName == "" {
		q.PrincipalName = q.PrincipalID
	}
	if q.OrgName == "" {
		q.OrgName = q.OrgID
	}
	if q.Role != "requester" && q.Role != "approver" && q.Role != "admin" {
		return res, errors.New("role must be requester, approver or admin")
	}

	err := s.WithOrg(ctx, q.OrgID, func(tx pgx.Tx) error {
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT true FROM organizations WHERE id = $1`, q.OrgID).Scan(&seen); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if !seen {
			if _, err := tx.Exec(ctx, `INSERT INTO organizations(id, name) VALUES ($1, $2)`, q.OrgID, q.OrgName); err != nil {
				return err
			}
			res.OrgCreated = true
		}

		if err := tx.QueryRow(ctx, `SELECT true FROM principals WHERE org_id = $1 AND id = $2`, q.OrgID, q.PrincipalID).Scan(&seen); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if seen {
			// Re-activate but do not rewrite the role: an operator bootstrap must not become
			// a quiet privilege change on a principal that already exists.
			if _, err := tx.Exec(ctx, `UPDATE principals SET active = true WHERE org_id = $1 AND id = $2`, q.OrgID, q.PrincipalID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `INSERT INTO principals(id, org_id, name, role, active) VALUES ($1, $2, $3, $4, true)`,
				q.PrincipalID, q.OrgID, q.PrincipalName, q.Role); err != nil {
				return err
			}
			res.PrincipalCreated = true
		}

		// The link is created in the SAME transaction as the organization and principal.
		//
		// An earlier revision linked in a second transaction, which allowed a committed
		// half-built onboarding: the org and principal would persist while the link was
		// refused (because the subject already maps to another principal), leaving an
		// administrator that cannot sign in and no record explaining why.
		if err := linkIdentityInTx(ctx, tx, q.OrgID, q.Issuer, q.Subject, q.PrincipalID, q.Actor); err != nil {
			return err
		}
		res.Linked = true

		return auditTx(ctx, tx, q.OrgID, q.PrincipalID, "identity.bootstrap", q.Actor, map[string]any{
			"org_id": q.OrgID, "principal_id": q.PrincipalID, "issuer": q.Issuer, "subject": q.Subject,
			"org_created": res.OrgCreated, "principal_created": res.PrincipalCreated,
		})
	})
	if err != nil {
		return res, err
	}
	return res, nil
}
