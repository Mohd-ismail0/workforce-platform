package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListAgents(ctx context.Context, org string) ([]Agent, error) {
	out := []Agent{}
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT id,org_id,name,harness,status,owner_id,capabilities,
		                                 coalesce(template_id,''),coalesce(template_version,''),instructions
		                            FROM agents ORDER BY name,id`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var a Agent
			var raw []byte
			if e = rows.Scan(&a.ID, &a.OrgID, &a.Name, &a.Harness, &a.Status, &a.OwnerID, &raw,
				&a.TemplateID, &a.TemplateVersion, &a.Instructions); e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &a.Capabilities); e != nil {
				return e
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
