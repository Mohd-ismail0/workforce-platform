package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// errNoLinkedIdentity is deliberately opaque. Callers must not distinguish "unknown
// external account" from "revoked principal" or "wrong org" in a response — a precise
// reason is a probe oracle that tells an attacker which subjects are linked.
var errNoLinkedIdentity = errors.New("no platform identity is linked to this external account")

// ResolveOIDCIdentity maps a VERIFIED external identity to a platform principal.
//
// Two steps, and the split is required rather than stylistic:
//
//  1. Resolve the link. `identity_links` is deliberately NOT tenant-scoped (see the
//     migration): at this moment the organization is unknown, it is what we are
//     discovering, so `current_setting('app.org_id')` is unset.
//  2. Read the principal through the tenant-scoped path. `principals` carries the tenant
//     RLS policy, so a direct JOIN in step 1 would silently match ZERO rows while
//     `app.org_id` was unset. Going through ResolveIdentity means the org is set first
//     and the same active-principal check the local path uses applies here too.
//
// An unmapped identity is REFUSED. It authenticates successfully and still gets no
// platform identity: there is no auto-provisioning on first login, and no linking by
// email, because email is reassignable and linking on it is an account-takeover
// primitive.
func (s *Store) ResolveOIDCIdentity(ctx context.Context, issuer, subject string) (platform.Identity, error) {
	if issuer == "" || subject == "" {
		return platform.Identity{}, errNoLinkedIdentity
	}
	var orgID, principalID string
	err := s.Pool.QueryRow(ctx, `
		SELECT org_id, principal_id
		  FROM identity_links
		 WHERE issuer = $1 AND subject = $2 AND active`,
		issuer, subject).Scan(&orgID, &principalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Identity{}, errNoLinkedIdentity
		}
		return platform.Identity{}, err
	}
	// Org now known: the tenant-scoped read enforces RLS and rejects inactive principals.
	id, err := s.ResolveIdentity(ctx, platform.Identity{OrgID: orgID, ID: principalID})
	if err != nil {
		return platform.Identity{}, errNoLinkedIdentity
	}
	return id, nil
}

// LinkOIDCIdentity binds a verified external identity to an existing principal.
//
// Linking is an administrative act, never a side effect of signing in. It fails if the
// principal is not present in the stated org, and it fails when the same
// (issuer, subject) is already bound to a DIFFERENT principal: silently repointing an
// existing identity would hand one person another person's history and authority.
func (s *Store) LinkOIDCIdentity(ctx context.Context, orgID, issuer, subject, principalID, createdBy string) error {
	if orgID == "" || issuer == "" || subject == "" || principalID == "" || createdBy == "" {
		return errors.New("org, issuer, subject, principal and actor are required")
	}
	return s.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		// The principal must exist here. The foreign key enforces this, but a clear
		// error beats a constraint violation.
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
			// Idempotent: the same binding already exists. Re-activate if it was
			// deactivated, so a documented re-link is possible and explicit.
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
	})
}

// UnlinkOIDCIdentity deactivates a binding. It deactivates rather than deletes so the
// audit trail still records that this external account once mapped here, and so that
// re-linking is an explicit act rather than an accident.
func (s *Store) UnlinkOIDCIdentity(ctx context.Context, orgID, issuer, subject string) error {
	return s.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE identity_links SET active = false
			 WHERE org_id = $1 AND issuer = $2 AND subject = $3 AND active`, orgID, issuer, subject)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("no active link found for this external account in this organization")
		}
		return nil
	})
}
