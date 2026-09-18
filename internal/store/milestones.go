package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Milestone is a stated outcome on a project, with the evidence required to call
// it met. It is not a "Done" column and not a summary of its tasks.
type Milestone struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	Name               string `json:"name"`
	AcceptanceEvidence string `json:"acceptance_evidence"`
	// CommittedDate is a promise; ForecastDate is a guess that should be revised
	// freely. They are separate columns so a revised guess does not silently
	// rewrite what was promised.
	CommittedDate string `json:"committed_date"`
	ForecastDate  string `json:"forecast_date"`
	Status        string `json:"status"`
	MetAt         string `json:"met_at"`
	MetEvidence   string `json:"met_evidence"`
	Version       int64  `json:"version"`
	CreatedAt     string `json:"created_at"`
	// UnmetPrerequisites counts prerequisite milestones not yet met, so the
	// planning view can show why something is not yet ready to be met.
	UnmetPrerequisites int `json:"unmet_prerequisites"`
}

const milestoneCols = `id,project_id,name,acceptance_evidence,committed_date,forecast_date,status,
	met_at,met_evidence,version,created_at`

func scanMilestone(row interface{ Scan(...any) error }) (Milestone, error) {
	var m Milestone
	var committed, forecast, metAt *time.Time
	var created time.Time
	e := row.Scan(&m.ID, &m.ProjectID, &m.Name, &m.AcceptanceEvidence, &committed, &forecast,
		&m.Status, &metAt, &m.MetEvidence, &m.Version, &created)
	m.CommittedDate = scanDate(committed)
	m.ForecastDate = scanDate(forecast)
	m.MetAt = scanTime(metAt)
	m.CreatedAt = scanTime(&created)
	return m, e
}

// scanDate renders a DATE (not a timestamp) as YYYY-MM-DD. A milestone date is a
// calendar day, and rendering it as an instant would invite timezone off-by-one
// errors in every consumer.
func scanDate(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.UTC().Format("2006-01-02")
}

// CreateMilestone records a stated outcome. Acceptance evidence is REQUIRED: a
// milestone with none can only ever be "completed" by assertion, which is the
// failure this table exists to prevent.
func (s *Store) CreateMilestone(ctx context.Context, org, actor, projectID, name, evidence string, committed, forecast *time.Time) (Milestone, error) {
	var m Milestone
	if strings.TrimSpace(name) == "" {
		return m, errors.New("a milestone name is required")
	}
	if strings.TrimSpace(evidence) == "" {
		return m, errors.New("acceptance evidence is required: without it, 'met' means nothing")
	}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var exists bool
		if e := tx.QueryRow(ctx, "select exists(select 1 from projects where org_id=$1 and id=$2)", org, projectID).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return errors.New("project not found")
		}
		var scanErr error
		m, scanErr = scanMilestone(tx.QueryRow(ctx,
			`insert into milestones(id,org_id,project_id,name,acceptance_evidence,committed_date,forecast_date)
			 values($1,$2,$3,$4,$5,$6,$7) returning `+milestoneCols,
			platform.NewID(), org, projectID, name, evidence, committed, forecast))
		return scanErr
	})
	return m, e
}

// GetMilestone reads one milestone. A missing one is ErrNotFound, so the HTTP
// layer answers 404 rather than a conflict.
func (s *Store) GetMilestone(ctx context.Context, org, id string) (Milestone, error) {
	var m Milestone
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var e error
		m, e = scanMilestone(tx.QueryRow(ctx, "select "+milestoneCols+" from milestones where org_id=$1 and id=$2", org, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return e
	})
	return m, e
}

// ListMilestones returns a project's milestones, each with its count of unmet
// prerequisites so a planning view can explain why something is not ready.
func (s *Store) ListMilestones(ctx context.Context, org, projectID string) ([]Milestone, error) {
	out := []Milestone{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `
			select m.id,m.project_id,m.name,m.acceptance_evidence,m.committed_date,m.forecast_date,
			       m.status,m.met_at,m.met_evidence,m.version,m.created_at,
			       (select count(*) from milestone_dependencies d
			          join milestones p on p.org_id=d.org_id and p.id=d.parent_id
			         where d.org_id=m.org_id and d.milestone_id=m.id and p.status <> 'met') as unmet
			  from milestones m
			 where m.org_id=$1 and ($2 = '' or m.project_id=$2)
			 order by coalesce(m.committed_date, m.forecast_date, m.created_at::date), m.name`, org, projectID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var m Milestone
			var committed, forecast, metAt *time.Time
			var created time.Time
			if e := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &m.AcceptanceEvidence, &committed,
				&forecast, &m.Status, &metAt, &m.MetEvidence, &m.Version, &created, &m.UnmetPrerequisites); e != nil {
				return e
			}
			m.CommittedDate = scanDate(committed)
			m.ForecastDate = scanDate(forecast)
			m.MetAt = scanTime(metAt)
			m.CreatedAt = scanTime(&created)
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, e
}

// SetMilestoneForecast revises the forecast ONLY. The commitment is untouched:
// a promise should not move because a guess was updated.
func (s *Store) SetMilestoneForecast(ctx context.Context, org, actor, id string, forecast *time.Time) (Milestone, error) {
	var m Milestone
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var status string
		if e := tx.QueryRow(ctx, "select status from milestones where org_id=$1 and id=$2 for update", org, id).Scan(&status); e != nil {
			return ErrNotFound
		}
		if status == "cancelled" {
			return errors.New("a cancelled milestone cannot be re-planned")
		}
		var e error
		m, e = scanMilestone(tx.QueryRow(ctx,
			`update milestones set forecast_date=$3, updated_at=clock_timestamp(), version=version+1
			  where org_id=$1 and id=$2 returning `+milestoneCols, org, id, forecast))
		return e
	})
	return m, e
}

// AddMilestoneDependency records that `milestoneID` cannot be met before
// `parentID`. Cycles are refused here rather than discovered later by a walk that
// never terminates.
func (s *Store) AddMilestoneDependency(ctx context.Context, org, milestoneID, parentID string) error {
	if milestoneID == parentID {
		return errors.New("a milestone cannot depend on itself")
	}
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var exists bool
		if e := tx.QueryRow(ctx,
			"select exists(select 1 from milestones where org_id=$1 and id=$2) and exists(select 1 from milestones where org_id=$1 and id=$3)",
			org, milestoneID, parentID).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return errors.New("milestone not found")
		}
		var cycle bool
		if e := tx.QueryRow(ctx, `
			with recursive reach(id) as (
			  select parent_id from milestone_dependencies where org_id=$1 and milestone_id=$2
			  union
			  select d.parent_id from milestone_dependencies d join reach r on d.milestone_id=r.id and d.org_id=$1
			)
			select exists(select 1 from reach where id=$3)`, org, parentID, milestoneID).Scan(&cycle); e != nil {
			return e
		}
		if cycle {
			return errors.New("dependency cycle")
		}
		_, e := tx.Exec(ctx,
			"insert into milestone_dependencies(org_id,milestone_id,parent_id) values($1,$2,$3) on conflict do nothing",
			org, milestoneID, parentID)
		return e
	})
}

// CompleteMilestone records that a milestone was met, and on what evidence.
//
// Two things must hold, and both are checked server-side rather than trusted
// from a client: the caller must be able to evidence the claim (non-empty), and
// every prerequisite milestone must already be met. "All its tasks happened to
// move" is deliberately not part of the test.
func (s *Store) CompleteMilestone(ctx context.Context, org, actor, id, evidence string, version int64) error {
	if strings.TrimSpace(evidence) == "" {
		return errors.New("completion evidence is required")
	}
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var status string
		var v int64
		if e := tx.QueryRow(ctx, "select status,version from milestones where org_id=$1 and id=$2 for update", org, id).Scan(&status, &v); e != nil {
			return ErrNotFound
		}
		if v != version {
			return errors.New("stale milestone revision")
		}
		if status == "met" {
			return errors.New("milestone is already met")
		}
		if status == "cancelled" {
			return errors.New("a cancelled milestone cannot be met")
		}
		var unmet int
		if e := tx.QueryRow(ctx, `
			select count(*) from milestone_dependencies d
			  join milestones p on p.org_id=d.org_id and p.id=d.parent_id
			 where d.org_id=$1 and d.milestone_id=$2 and p.status <> 'met'`, org, id).Scan(&unmet); e != nil {
			return e
		}
		if unmet > 0 {
			return errors.New("prerequisite milestones are not yet met")
		}
		_, e := tx.Exec(ctx,
			`update milestones
			    set status='met', met_at=clock_timestamp(), met_evidence=$3,
			        updated_at=clock_timestamp(), version=version+1
			  where org_id=$1 and id=$2`, org, id, evidence)
		return e
	})
}
