package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"time"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/runner"
)

type ErrNoActiveHarness struct{}

func (ErrNoActiveHarness) Error() string {
	return "an active harness release is required in the registry"
}

type AgentRun struct {
	ID               string   `json:"id"`
	OrgID            string   `json:"org_id"`
	TaskID           string   `json:"task_id"`
	AgentID          string   `json:"agent_id"`
	Harness          string   `json:"harness"`
	HarnessReleaseID string   `json:"harness_release_id"`
	RunnerID         string   `json:"runner_id"`
	Intent           string   `json:"intent"`
	Status           string   `json:"status"`
	ProposalID       string   `json:"proposal_id"`
	ResultSummary    string   `json:"result_summary"`
	FailureReason    string   `json:"failure_reason"`
	Notes            []string `json:"notes"`
	CreatedBy        string   `json:"created_by"`
	Version          int64    `json:"version"`
	CreatedAt        string   `json:"created_at"`
	StartedAt        string   `json:"started_at,omitempty"`
	FinishedAt       string   `json:"finished_at,omitempty"`
}

func scanRun(row interface{ Scan(...any) error }) (AgentRun, error) {
	var x AgentRun
	var notes []byte
	var ca, sa, fa *time.Time
	e := row.Scan(&x.ID, &x.OrgID, &x.TaskID, &x.AgentID, &x.Harness, &x.HarnessReleaseID, &x.RunnerID, &x.Intent, &x.Status, &x.ProposalID, &x.ResultSummary, &x.FailureReason, &notes, &x.CreatedBy, &x.Version, &ca, &sa, &fa)
	if e != nil {
		return x, e
	}
	_ = json.Unmarshal(notes, &x.Notes)
	x.CreatedAt = scanTime(ca)
	x.StartedAt = scanTime(sa)
	x.FinishedAt = scanTime(fa)
	if x.Notes == nil {
		x.Notes = []string{}
	}
	return x, nil
}

const runCols = "id,org_id,task_id,agent_id,harness,coalesce(harness_release_id,''),runner_id,intent,status,coalesce(proposal_id,''),result_summary,failure_reason,notes,created_by,version,created_at,started_at,finished_at"

func (s *Store) CreateAgentRun(ctx context.Context, org, taskID, actorID, agentID, intent string) (AgentRun, error) {
	var x AgentRun
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var harness, aid string
		var rel string
		if e := tx.QueryRow(ctx, "select name,harness from agents where org_id=$1 and id=$2", org, agentID).Scan(&aid, &harness); e != nil {
			return e
		}
		_ = aid
		if e := tx.QueryRow(ctx, "select id from registry_releases where org_id=$1 and kind='harness' and state='active' order by updated_at desc limit 1", org).Scan(&rel); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrNoActiveHarness{}
			}
			return e
		}
		var owner, status string
		if e := tx.QueryRow(ctx, "select owner_id,status from tasks where org_id=$1 and id=$2", org, taskID).Scan(&owner, &status); e != nil {
			return e
		}
		if owner != actorID {
			var role string
			var active bool
			if e := tx.QueryRow(ctx, "select role,active from principals where org_id=$1 and id=$2", org, actorID).Scan(&role, &active); e != nil || !active || role != "admin" {
				return errors.New("only task owner or active admin may create agent run")
			}
		}
		if status == "done" || status == "cancelled" {
			return errors.New("terminal task")
		}
		id := platform.NewID()
		if e := tx.QueryRow(ctx, "insert into agent_runs(id,org_id,task_id,agent_id,harness,harness_release_id,runner_id,intent,created_by) values($1,$2,$3,$4,$5,$6,'simulator',$7,$8) returning "+runCols, id, org, taskID, agentID, harness, rel, intent, actorID).Scan(&x.ID, &x.OrgID, &x.TaskID, &x.AgentID, &x.Harness, &x.HarnessReleaseID, &x.RunnerID, &x.Intent, &x.Status, &x.ProposalID, &x.ResultSummary, &x.FailureReason, new([]byte), &x.CreatedBy, &x.Version, new(*time.Time), new(*time.Time), new(*time.Time)); e != nil {
			return e
		}
		return s.enqueueAgentRunTx(ctx, tx, AgentRunArgs{OrgID: org, RunID: id})
	})
	return x, e
}
func (s *Store) ListAgentRuns(ctx context.Context, org string) ([]AgentRun, error) {
	out := []AgentRun{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select "+runCols+" from agent_runs where org_id=$1 order by created_at", org)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			x, e := scanRun(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) GetAgentRun(ctx context.Context, org, id string) (AgentRun, error) {
	var x AgentRun
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var y AgentRun
		y, err := scanRun(tx.QueryRow(ctx, "select "+runCols+" from agent_runs where org_id=$1 and id=$2", org, id))
		x = y
		return err
	})
	return x, e
}
func (s *Store) ExecuteAgentRun(ctx context.Context, org, id string) error {
	var recs []runner.RecordRef
	var owner string
	done := false
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var st, existing string
		if e := tx.QueryRow(ctx, `SELECT ar.status,coalesce(ar.proposal_id,''),t.owner_id FROM agent_runs ar JOIN tasks t ON t.org_id=ar.org_id AND t.id=ar.task_id WHERE ar.org_id=$1 AND ar.id=$2 FOR UPDATE OF ar`, org, id).Scan(&st, &existing, &owner); e != nil {
			return e
		}
		// Idempotent: a run that already produced a proposal must never produce a second one.
		if existing != "" || (st != "queued" && st != "running") {
			done = true
			return nil
		}
		if _, e := tx.Exec(ctx, "update agent_runs set status='running',started_at=coalesce(started_at,clock_timestamp()) where org_id=$1 and id=$2", org, id); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, "select id,integration,version,data from simulator_records where org_id=$1 order by id limit 50", org)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var q runner.RecordRef
			var b []byte
			if e = rows.Scan(&q.ID, &q.Integration, &q.Version, &b); e != nil {
				return e
			}
			_ = json.Unmarshal(b, &q.Data)
			recs = append(recs, q)
		}
		return rows.Err()
	})
	if e != nil || done {
		return e
	}
	var task, aid, harness, intent string
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select task_id,agent_id,harness,intent from agent_runs where org_id=$1 and id=$2", org, id).Scan(&task, &aid, &harness, &intent)
	})
	if e != nil {
		return e
	}
	res, runErr := runner.Default().Run(ctx, runner.Request{OrgID: org, TaskID: task, RunID: id, AgentID: aid, Harness: harness, Intent: intent, Records: recs})

	// The proposal is always authored by the task owner: a run triggered by an admin
	// still prepares work on the owner's behalf and follows the same endorsement path.
	fail := func(reason string) error {
		return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
			_, z := tx.Exec(ctx, "update agent_runs set status='failed',failure_reason=$1,finished_at=clock_timestamp() where org_id=$2 and id=$3", reason, org, id)
			return z
		})
	}
	if runErr != nil {
		return fail("runner_error: " + runErr.Error())
	}
	if res.Status != "succeeded" || res.Draft == nil || len(res.Draft.Operations) == 0 {
		reason := res.FailureReason
		if reason == "" {
			reason = "runner_returned_no_usable_draft"
		}
		return fail(reason)
	}
	summary := res.Draft.Summary
	if summary == "" {
		summary = "Prepared by agent run " + id
	}
	p, e := s.CreateProposal(ctx, org, task, owner, summary+" (agent run "+id+")", res.Draft.Operations)
	if e != nil {
		return fail("proposal_rejected: " + e.Error())
	}
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var z error
		_, z = tx.Exec(ctx, "update agent_runs set status='succeeded',proposal_id=$1,result_summary=$2,finished_at=clock_timestamp() where org_id=$3 and id=$4", p.ID, summary, org, id)
		return z
	})
}

type AgentRunArgs struct {
	OrgID string `json:"org_id"`
	RunID string `json:"run_id"`
}

func (AgentRunArgs) Kind() string { return "agent.run" }

type agentRunWorker struct {
	river.WorkerDefaults[AgentRunArgs]
	store *Store
}

func (w *agentRunWorker) Work(ctx context.Context, j *river.Job[AgentRunArgs]) error {
	return w.store.ExecuteAgentRun(ctx, j.Args.OrgID, j.Args.RunID)
}
func (s *Store) enqueueAgentRunTx(ctx context.Context, tx pgx.Tx, a AgentRunArgs) error {
	c, e := river.NewClient(riverpgxv5.New(nil), &river.Config{})
	if e != nil {
		return e
	}
	_, e = c.InsertTx(ctx, tx, a, nil)
	return e
}
