package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"log"
	"time"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/runner"
)

type ErrNoActiveHarness struct{}

func (ErrNoActiveHarness) Error() string {
	return "an active harness release is required in the registry"
}

type ErrHarnessUnsupported struct{}

func (ErrHarnessUnsupported) Error() string {
	return "the configured harness is unsupported by this binary"
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
	var n []byte
	var ca, sa, fa *time.Time
	e := row.Scan(&x.ID, &x.OrgID, &x.TaskID, &x.AgentID, &x.Harness, &x.HarnessReleaseID, &x.RunnerID, &x.Intent, &x.Status, &x.ProposalID, &x.ResultSummary, &x.FailureReason, &n, &x.CreatedBy, &x.Version, &ca, &sa, &fa)
	_ = json.Unmarshal(n, &x.Notes)
	if x.Notes == nil {
		x.Notes = []string{}
	}
	x.CreatedAt = scanTime(ca)
	x.StartedAt = scanTime(sa)
	x.FinishedAt = scanTime(fa)
	return x, e
}

const runCols = "id,org_id,task_id,agent_id,harness,coalesce(harness_release_id,''),runner_id,intent,status,coalesce(proposal_id,''),result_summary,failure_reason,notes,created_by,version,created_at,started_at,finished_at"

func (s *Store) CreateAgentRun(ctx context.Context, org, task, actor, agent, intent string) (AgentRun, error) {
	var x AgentRun
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var h string
		if e := tx.QueryRow(ctx, "select harness from agents where org_id=$1 and id=$2", org, agent).Scan(&h); e != nil {
			return e
		}
		var rel, runnerID string
		e := tx.QueryRow(ctx, "select id,coalesce(nullif(manifest->>'runner_id',''),family) from registry_releases where org_id=$1 and kind='harness' and state='active' and coalesce(nullif(manifest->>'runner_id',''),family)=$2 order by updated_at desc limit 1", org, h).Scan(&rel, &runnerID)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				if !runner.Supported(h) {
					return ErrHarnessUnsupported{}
				}
				return ErrNoActiveHarness{}
			}
			return e
		}
		if !runner.Supported(runnerID) {
			return ErrHarnessUnsupported{}
		}
		var owner, status string
		if e = tx.QueryRow(ctx, "select owner_id,status from tasks where org_id=$1 and id=$2", org, task).Scan(&owner, &status); e != nil {
			return e
		}
		if owner != actor {
			var role string
			var active bool
			if e = tx.QueryRow(ctx, "select role,active from principals where org_id=$1 and id=$2", org, actor).Scan(&role, &active); e != nil || !active || role != "admin" {
				return errors.New("only task owner or active admin may create agent run")
			}
		}
		if status == "done" || status == "cancelled" {
			return errors.New("terminal task")
		}
		id := platform.NewID()
		if e = tx.QueryRow(ctx, "insert into agent_runs(id,org_id,task_id,agent_id,harness,harness_release_id,runner_id,intent,created_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9) returning "+runCols, id, org, task, agent, h, rel, runnerID, intent, actor).Scan(&x.ID, &x.OrgID, &x.TaskID, &x.AgentID, &x.Harness, &x.HarnessReleaseID, &x.RunnerID, &x.Intent, &x.Status, &x.ProposalID, &x.ResultSummary, &x.FailureReason, new([]byte), &x.CreatedBy, &x.Version, new(*time.Time), new(*time.Time), new(*time.Time)); e != nil {
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
		var e error
		x, e = scanRun(tx.QueryRow(ctx, "select "+runCols+" from agent_runs where org_id=$1 and id=$2", org, id))
		return e
	})
	return x, e
}
func (s *Store) ExecuteAgentRun(ctx context.Context, org, id string) error {
	tok := platform.NewID()
	// initiator = who started this run. owner = who is accountable for the task.
	// They differ whenever an admin starts a run on someone else's task, so the two
	// must never share a variable: conflating them would question the wrong person
	// and check the wrong principal for revocation.
	var task, aid, h, intent, release, initiator, runnerID, owner string
	var recs []runner.RecordRef
	var inputs []runner.Input
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var attempt int
		// The lease must exceed the longest run this runner is allowed to take.
		// A fixed short lease would let a healthy-but-slow harness outlive its own
		// lease and be reclaimed mid-run, publishing the same work twice.
		var configured string
		_ = tx.QueryRow(ctx, "select runner_id from agent_runs where org_id=$1 and id=$2", org, id).Scan(&configured)
		leaseSeconds := int64(runner.RunBudget(configured).Seconds()) + 60
		if leaseSeconds < 120 {
			leaseSeconds = 120
		}
		// Claim is a LEASE, not a flag. A worker that dies after claiming would
		// otherwise strand the run forever, because a bare 'queued' claim can never
		// pick up a run already marked 'running'. Reclaiming an expired lease is
		// safe because every publication path re-checks claim_token: the previous
		// holder's token is now stale, so it cannot publish or record failure.
		return tx.QueryRow(ctx, "update agent_runs set status='running',attempt=attempt+1,claim_token=$3,claim_expires_at=clock_timestamp()+($4::bigint * interval '1 second'),started_at=coalesce(started_at,clock_timestamp()) where org_id=$1 and id=$2 and (status='queued' or (status='running' and (claim_expires_at is null or claim_expires_at < clock_timestamp()))) returning task_id,agent_id,harness,intent,created_by,coalesce(harness_release_id,''),runner_id,attempt", org, id, tok, leaseSeconds).Scan(&task, &aid, &h, &intent, &initiator, &release, &runnerID, &attempt)
	})
	if errors.Is(e, pgx.ErrNoRows) {
		// Nothing was claimable. Decide carefully whether this delivery is DONE or
		// merely EARLY: reporting success for a run that is still in flight marks the
		// job complete in the queue, consuming the only delivery that could recover
		// the run if the current holder dies. Real harnesses run for minutes, so that
		// window is wide.
		var st string
		if qe := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "select status from agent_runs where org_id=$1 and id=$2", org, id).Scan(&st)
		}); qe != nil {
			return qe
		}
		switch st {
		case "running":
			// Held by a live lease: ask to be retried rather than acknowledged.
			return errors.New("run is held by a live lease; retrying later")
		default:
			// Terminal (succeeded/failed/cancelled) or parked awaiting an answer
			// (which enqueues its own fresh continuation): nothing left to do here.
			return nil
		}
	}
	if e != nil {
		return e
	}
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
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
		if e = rows.Err(); e != nil {
			return e
		}
		rows, e = tx.Query(ctx, "select response from decision_gates where org_id=$1 and run_id=$2 and status='resolved' order by created_at", org, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			var m map[string]json.RawMessage
			if json.Unmarshal(b, &m) == nil {
				for k, v := range m {
					inputs = append(inputs, runner.Input{Name: k, Value: string(v)})
				}
			}
		}
		if e := rows.Err(); e != nil {
			return e
		}
		return tx.QueryRow(ctx, "select owner_id from tasks where org_id=$1 and id=$2", org, task).Scan(&owner)
	})
	if e != nil {
		log.Printf("agent run %s: gathering context failed: %v", id, e)
		return s.failRun(ctx, org, id, tok, "internal_error")
	}
	r, e := runner.New(runnerID)
	if e != nil {
		log.Printf("agent run %s: runner %q is not available to this process: %v", id, runnerID, e)
		return s.failRun(ctx, org, id, tok, "internal_error")
	}
	res, re := r.Run(ctx, runner.Request{OrgID: org, TaskID: task, RunID: id, AgentID: aid, Harness: h, Intent: intent, Records: recs, Inputs: inputs})
	if re != nil {
		log.Printf("agent run %s: runner %q errored: %v", id, runnerID, re)
		return s.failRun(ctx, org, id, tok, "internal_error")
	}
	if res.Status == runner.StatusWaiting && res.Gate != nil {
		return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
			var st string
			if e := tx.QueryRow(ctx, "select status from agent_runs where org_id=$1 and id=$2 and claim_token=$3 for update", org, id, tok).Scan(&st); e != nil || st != "running" {
				return nil
			}
			rid := res.Gate.RespondentID
			if rid == "" {
				rid = owner
			}
			gid := platform.NewID()
			tag := res.Gate.InputSchema
			if len(tag) == 0 {
				tag = []byte(`{}`)
			}
			// A question the platform cannot record an answer to must be refused HERE,
			// not persisted. Otherwise the run parks in 'waiting' while every answer
			// fails validation, which is a dead end rather than a pause.
			if e := ValidateGateSchema(tag); e != nil {
				return publishError{code: "invalid_gate_schema"}
			}
			q, e := tx.Exec(ctx, "insert into decision_gates(id,org_id,task_id,run_id,kind,prompt,input_schema,respondent_id,created_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9) on conflict do nothing", gid, org, task, id, res.Gate.Kind, res.Gate.Prompt, tag, rid, initiator)
			if e != nil {
				return e
			}
			if q.RowsAffected() == 0 {
				// Another attempt already created this run's question. Returning here
				// would leave the run in 'running' with no gate pointer, so it could
				// never be answered. Adopt the existing question instead.
				if e := tx.QueryRow(ctx, "select id from decision_gates where org_id=$1 and run_id=$2 and status='pending' limit 1", org, id).Scan(&gid); e != nil {
					return e
				}
			}
			_, e = tx.Exec(ctx, "update agent_runs set status='waiting',gate_id=$1,claim_token='',claim_expires_at=null where org_id=$2 and id=$3", gid, org, id)
			return e
		})
	}
	if res.Status != runner.StatusSucceeded || res.Draft == nil || len(res.Draft.Operations) == 0 {
		return s.failRun(ctx, org, id, tok, res.FailureReason)
	}
	summary := res.Draft.Summary
	if summary == "" {
		summary = "Prepared by agent run " + id
	}
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var st, ts, rs string
		if e := tx.QueryRow(ctx, "select status from agent_runs where org_id=$1 and id=$2 and claim_token=$3 for update", org, id, tok).Scan(&st); e != nil || st != "running" {
			return nil
		}
		if e := tx.QueryRow(ctx, "select status from tasks where org_id=$1 and id=$2", org, task).Scan(&ts); e != nil {
			return e
		}
		// A terminal task is a recorded outcome, not a retryable error. Returning a
		// bare error here rolls the transaction back, leaving the run stuck in
		// 'running' where the queued-only claim can never pick it up again.
		if ts == "done" || ts == "cancelled" {
			return s.updateFailureTx(ctx, tx, org, id, tok, "task_no_longer_active")
		}
		if e := tx.QueryRow(ctx, "select state from registry_releases where org_id=$1 and id=$2", org, release).Scan(&rs); e != nil || rs != "active" {
			return s.updateFailureTx(ctx, tx, org, id, tok, "internal_error")
		}
		// Both the accountable owner and the initiator must still be active:
		// ownership may have moved and the initiator may have been revoked.
		for _, who := range []string{owner, initiator} {
			var active bool
			var role string
			if e := tx.QueryRow(ctx, "select active,role from principals where org_id=$1 and id=$2", org, who).Scan(&active, &role); e != nil || !active {
				return s.updateFailureTx(ctx, tx, org, id, tok, "actor_revoked")
			}
		}
		p, e := s.createProposalTx(ctx, tx, org, task, owner, summary+" (agent run "+id+")", "agent_run", id, res.Draft.Operations)
		if e != nil {
			// Once this statement fails the transaction is aborted, so the failure
			// CANNOT be recorded in it (any further statement errors out and the run
			// would be left in 'running'). Signal the caller to record the failure in
			// a fresh, fence-checked transaction instead.
			return publishError{code: publicFailure(e)}
		}
		_, e = tx.Exec(ctx, "update agent_runs set status='succeeded',proposal_id=$1,result_summary=$2,finished_at=clock_timestamp(),claim_token='',claim_expires_at=null where org_id=$3 and id=$4 and claim_token=$5", p.ID, summary, org, id, tok)
		return e
	})
	var pe publishError
	if errors.As(e, &pe) {
		return s.failRun(ctx, org, id, tok, pe.code)
	}
	return e
}

// publishError carries a safe public code out of a transaction that has been
// aborted by the failing statement, so the failure can be recorded separately.
type publishError struct{ code string }

func (e publishError) Error() string { return "run publication failed: " + e.code }
func (s *Store) updateFailureTx(ctx context.Context, tx pgx.Tx, org, id, tok, code string) error {
	_, e := tx.Exec(ctx, "update agent_runs set status='failed',failure_reason=$1,finished_at=clock_timestamp(),claim_token='',claim_expires_at=null where org_id=$2 and id=$3 and claim_token=$4", code, org, id, tok)
	return e
}
func (s *Store) failRun(ctx context.Context, org, id, tok, code string) error {
	if code == "" {
		code = "internal_error"
	}
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error { return s.updateFailureTx(ctx, tx, org, id, tok, code) })
}
func publicFailure(e error) string {
	if errors.Is(e, connectors.ErrStale) {
		return "stale_record"
	}
	if errors.Is(e, connectors.ErrInvalid) {
		return "invalid_operation"
	}
	if errors.Is(e, connectors.ErrUnknown) {
		return "unknown_operation"
	}
	return "internal_error"
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
