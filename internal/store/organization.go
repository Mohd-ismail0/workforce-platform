package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Position is a node in the organization structure. A position with no holder is
// a vacancy, which is a state rather than an absence of data.
type Position struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
}

// Relationship is an effective-dated fact about a principal: who they report to,
// which teams they belong to, and who they cover for. It is a period, not a
// column, so history stays answerable and a change does not rewrite the past.
type Relationship struct {
	ID        string `json:"id"`
	SubjectID string `json:"subject_id"`
	Kind      string `json:"kind"`
	ObjectID  string `json:"object_id"`
	// ValidFrom/ValidTo form a half-open [from, to) interval. ValidTo is empty
	// while the relationship is still in effect.
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

// CreatePosition adds an organization-structure node.
func (s *Store) CreatePosition(ctx context.Context, org, actor, name, parent string) (Position, error) {
	var p Position
	var created time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		if parent != "" {
			var exists bool
			if e := tx.QueryRow(ctx, "select exists(select 1 from positions where org_id=$1 and id=$2)", org, parent).Scan(&exists); e != nil || !exists {
				return errors.New("parent position not found")
			}
		}
		id := platform.NewID()
		var parentArg any
		if parent != "" {
			parentArg = parent
		}
		return tx.QueryRow(ctx,
			"insert into positions(id,org_id,name,parent_id) values($1,$2,$3,$4) returning id,name,coalesce(parent_id,''),version,created_at",
			id, org, name, parentArg,
		).Scan(&p.ID, &p.Name, &p.ParentID, &p.Version, &created)
	})
	p.CreatedAt = scanTime(&created)
	return p, e
}

// ListPositions returns the organization-structure nodes.
func (s *Store) ListPositions(ctx context.Context, org string) ([]Position, error) {
	out := []Position{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`select id,name,coalesce(parent_id,''),version,created_at
			   from positions where org_id=$1 order by name, id`, org)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p Position
			var created time.Time
			if e := rows.Scan(&p.ID, &p.Name, &p.ParentID, &p.Version, &created); e != nil {
				return e
			}
			p.CreatedAt = scanTime(&created)
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, e
}

// CreateRelationship records an effective-dated relationship. `from` is
// inclusive; `to` is exclusive and nil means "still in effect".
//
// Two rules are enforced here rather than by convention, because a query cannot
// repair a contradictory history:
//
//   - A person has at most ONE manager at a time. The database also enforces this
//     with an exclusion constraint, so a race between two concurrent calls cannot
//     slip past the check.
//   - The management graph is acyclic. A cycle is not a hierarchy, and discovering
//     it later means an open-ended walk that never terminates.
//
// Matrix membership and cover are deliberately exempt from the one-manager rule:
// belonging to several teams, or covering for several colleagues, is the point of
// a matrix.
func (s *Store) CreateRelationship(ctx context.Context, org, actor, subject, kind, object string, from time.Time, to *time.Time) (Relationship, error) {
	var r Relationship
	if kind != "reports_to" && kind != "member_of" && kind != "covers_for" {
		return r, errors.New("unsupported relationship kind")
	}
	if to != nil && !to.After(from) {
		return r, errors.New("relationship would end before it starts")
	}
	var created time.Time
	var vf, vt *time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		if e := requireActivePrincipal(ctx, tx, org, subject); e != nil {
			return e
		}
		switch kind {
		case "member_of":
			var exists bool
			if e := tx.QueryRow(ctx, "select exists(select 1 from positions where org_id=$1 and id=$2)", org, object).Scan(&exists); e != nil || !exists {
				return errors.New("position not found")
			}
		default:
			if e := requireActivePrincipal(ctx, tx, org, object); e != nil {
				return e
			}
		}
		if kind == "reports_to" {
			if e := detectManagementCycle(ctx, tx, org, subject, object, from, to); e != nil {
				return e
			}
		}
		id := platform.NewID()
		return tx.QueryRow(ctx,
			`insert into org_relationships(id,org_id,subject_id,kind,object_id,valid,created_by)
			 values($1,$2,$3,$4,$5,tstzrange($6::timestamptz,$7::timestamptz,'[)'),$8)
			 returning id,subject_id,kind,object_id,lower(valid),upper(valid),created_by,created_at`,
			id, org, subject, kind, object, from, to, actor,
		).Scan(&r.ID, &r.SubjectID, &r.Kind, &r.ObjectID, &vf, &vt, &r.CreatedBy, &created)
	})
	r.ValidFrom = scanTime(vf)
	r.ValidTo = scanTime(vt)
	r.CreatedAt = scanTime(&created)
	return r, e
}

// detectManagementCycle refuses a reports_to edge that would close a loop.
//
// It walks UP the management chain from the prospective manager and fails if it
// reaches the subject. The walk is bounded by the validity window under
// construction: a cycle only matters if the intervals would actually overlap in
// time, so a historical non-overlapping arrangement is still allowed.
func detectManagementCycle(ctx context.Context, tx pgx.Tx, org, subject, manager string, from time.Time, to *time.Time) error {
	var cycles bool
	e := tx.QueryRow(ctx, `
		WITH RECURSIVE chain AS (
			SELECT $1::text AS id, 0 AS depth
			UNION ALL
			SELECT r.object_id, c.depth + 1
			  FROM org_relationships r
			  JOIN chain c ON r.subject_id = c.id
			 WHERE r.org_id = $2
			   AND r.kind = 'reports_to'
			   AND r.valid && tstzrange($3::timestamptz, $4::timestamptz, '[)')
			   AND c.depth < 64
		)
		SELECT EXISTS (SELECT 1 FROM chain WHERE id = $5 AND depth > 0)`,
		manager, org, from, to, subject,
	).Scan(&cycles)
	if e != nil {
		return e
	}
	if cycles {
		return errors.New("relationship would create a management cycle")
	}
	return nil
}

// EndRelationship closes a relationship's interval at `at` instead of deleting
// the row, so the period it was true remains answerable.
func (s *Store) EndRelationship(ctx context.Context, org, actor, id string, at time.Time) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var lower *time.Time
		if e := tx.QueryRow(ctx, "select lower(valid) from org_relationships where org_id=$1 and id=$2 for update", org, id).Scan(&lower); e != nil {
			return e
		}
		if lower == nil || !at.After(*lower) {
			return errors.New("relationship must end after it began")
		}
		// Close the interval, and refuse to move `valid` backwards if it is
		// already closed earlier than `at` (that would resurrect a period).
		_, e := tx.Exec(ctx,
			`update org_relationships
			    set valid = tstzrange(lower(valid), $3::timestamptz, '[)')
			  where org_id=$1 and id=$2 and (upper_inf(valid) or upper(valid) > $3)`, org, id, at)
		return e
	})
}

// RelationshipsAsOf returns the relationships in effect at an instant. This is
// the query that makes an effective-dated model worth having: "who reported to
// whom in March" is answerable, and expired cover is simply absent.
func (s *Store) RelationshipsAsOf(ctx context.Context, org string, asOf time.Time) ([]Relationship, error) {
	out := []Relationship{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`select id,subject_id,kind,object_id,lower(valid),upper(valid),created_by,created_at
			   from org_relationships
			  where org_id=$1 and valid @> $2::timestamptz
			  order by kind, subject_id, lower(valid)`, org, asOf)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r Relationship
			var vf, vt *time.Time
			var created time.Time
			if e := rows.Scan(&r.ID, &r.SubjectID, &r.Kind, &r.ObjectID, &vf, &vt, &r.CreatedBy, &created); e != nil {
				return e
			}
			r.ValidFrom = scanTime(vf)
			r.ValidTo = scanTime(vt)
			r.CreatedAt = scanTime(&created)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, e
}

// RequireActivePrincipal refuses an unknown or deactivated principal. Authority
// is never inferred from structure: a relationship names participants, it does
// not grant them anything.
func requireActivePrincipal(ctx context.Context, tx pgx.Tx, org, id string) error {
	if id == "" {
		return errors.New("principal is required")
	}
	var active bool
	if e := tx.QueryRow(ctx, "select active from principals where org_id=$1 and id=$2", org, id).Scan(&active); e != nil {
		return errors.New("principal not found")
	}
	if !active {
		return errors.New("principal is not active")
	}
	return nil
}
