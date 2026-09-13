package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

func (s *Store) ResolveIdentity(ctx context.Context, claimed platform.Identity) (platform.Identity, error) {
	var actual platform.Identity
	err := s.WithOrg(ctx, claimed.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id,org_id,role,name FROM principals WHERE org_id=$1 AND id=$2 AND active", claimed.OrgID, claimed.ID).Scan(&actual.ID, &actual.OrgID, &actual.Role, &actual.Name)
	})
	return actual, err
}
