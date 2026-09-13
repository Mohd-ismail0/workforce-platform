package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
)

type Store struct {
	Pool  *pgxpool.Pool
	river *river.Client[pgx.Tx]
}

var ErrNotFound = pgx.ErrNoRows

func Open(ctx context.Context, url string) (*Store, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return &Store{Pool: p}, nil
}
func (s *Store) Close() {
	if s != nil && s.Pool != nil {
		s.Pool.Close()
	}
}
func (s *Store) WithOrg(ctx context.Context, org string, fn func(pgx.Tx) error) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "select set_config('app.org_id',$1,true)", org); e != nil {
		return e
	}
	if org != "" {
		if _, e = tx.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1,0))", org); e != nil {
			return e
		}
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Project struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
	CreatedAt   string `json:"created_at,omitempty"`
}
type Task struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	OwnerID     string `json:"owner_id"`
	AssigneeID  string `json:"assignee_id"`
	ProjectID   string `json:"project_id"`
	Version     int64  `json:"version"`
	CreatedAt   string `json:"created_at"`
}
type Agent struct {
	ID           string   `json:"id"`
	OrgID        string   `json:"org_id"`
	Name         string   `json:"name"`
	Harness      string   `json:"harness"`
	Status       string   `json:"status"`
	OwnerID      string   `json:"owner_id"`
	Capabilities []string `json:"capabilities"`
}
type Proposal struct {
	ID            string                 `json:"id"`
	OrgID         string                 `json:"org_id"`
	TaskID        string                 `json:"task_id"`
	Revision      int64                  `json:"revision"`
	Digest        string                 `json:"digest"`
	Status        string                 `json:"status"`
	Summary       string                 `json:"summary"`
	Operations    []connectors.Operation `json:"operations"`
	CreatedAt     string                 `json:"created_at"`
	FailureReason string                 `json:"failure_reason,omitempty"`
}
type Decision struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	ProposalID string `json:"proposal_id"`
	Revision   int64  `json:"revision"`
	Digest     string `json:"digest"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}
type Event struct {
	ID               string          `json:"id"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	EventType        string          `json:"event_type"`
	ActorID          string          `json:"actor_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        string          `json:"created_at"`
}
type Receipt struct {
	ID          string         `json:"id"`
	EffectID    string         `json:"effect_id"`
	Kind        string         `json:"kind"`
	OutcomeCode string         `json:"outcome_code"`
	Result      map[string]any `json:"result"`
	CreatedAt   string         `json:"created_at"`
}
type Handoff struct {
	CreatedBy   string `json:"created_by"`
	ID          string `json:"id"`
	TaskID      string `json:"task_id"`
	RecipientID string `json:"recipient_id"`
	Role        string `json:"role"`
	Summary     string `json:"summary"`
	State       string `json:"state"`
	CreatedAt   string `json:"created_at"`
}
type Record struct {
	ID          string         `json:"id"`
	Integration string         `json:"integration"`
	Version     int64          `json:"version"`
	Data        map[string]any `json:"data"`
}

func scanProject(r interface{ Scan(...any) error }) (Project, error) {
	var x Project
	e := r.Scan(&x.ID, &x.OrgID, &x.Name, &x.Description, &x.Version)
	return x, e
}
func scanTime(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func (s *Store) CreateProject(ctx context.Context, org, name, desc string) (Project, error) {
	var x Project
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		id := platform.NewID()
		return tx.QueryRow(ctx, "insert into projects(id,org_id,name,description) values($1,$2,$3,$4) returning id,org_id,name,description,version", id, org, name, desc).Scan(&x.ID, &x.OrgID, &x.Name, &x.Description, &x.Version)
	})
	return x, e
}
func (s *Store) ListProjects(ctx context.Context, org string) ([]Project, error) {
	out := []Project{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,org_id,name,description,version from projects order by created_at")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			x, e := scanProject(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) CreateTask(ctx context.Context, org, title, desc, owner, assignee, project string) (Task, error) {
	var x Task
	var t time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		id := platform.NewID()
		e := tx.QueryRow(ctx, "insert into tasks(id,org_id,title,description,owner_id,assignee_id,project_id) values($1,$2,$3,$4,$5,nullif($6,''),nullif($7,'')) returning id,org_id,title,description,status,owner_id,coalesce(assignee_id,''),coalesce(project_id,''),version,created_at", id, org, title, desc, owner, assignee, project).Scan(&x.ID, &x.OrgID, &x.Title, &x.Description, &x.Status, &x.OwnerID, &x.AssigneeID, &x.ProjectID, &x.Version, &t)
		x.CreatedAt = scanTime(&t)
		return e
	})
	return x, e
}
func (s *Store) GetTask(ctx context.Context, org, id string) (Task, error) {
	var x Task
	var t time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select id,org_id,title,description,status,owner_id,coalesce(assignee_id,''),coalesce(project_id,''),version,created_at from tasks where id=$1", id).Scan(&x.ID, &x.OrgID, &x.Title, &x.Description, &x.Status, &x.OwnerID, &x.AssigneeID, &x.ProjectID, &x.Version, &t)
	})
	x.CreatedAt = scanTime(&t)
	return x, e
}
func (s *Store) ListTasks(ctx context.Context, org string) ([]Task, error) {
	out := []Task{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,org_id,title,description,status,owner_id,coalesce(assignee_id,''),coalesce(project_id,''),version,created_at from tasks order by created_at")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Task
			var t time.Time
			if e = rows.Scan(&x.ID, &x.OrgID, &x.Title, &x.Description, &x.Status, &x.OwnerID, &x.AssigneeID, &x.ProjectID, &x.Version, &t); e != nil {
				return e
			}
			x.CreatedAt = scanTime(&t)
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) enqueueEligibleChildren(ctx context.Context, tx pgx.Tx, org, parent string) error {
	rows, err := tx.Query(ctx, `select distinct p.id from task_dependencies d join proposals p on p.org_id=d.org_id and p.task_id=d.task_id join tasks t on t.org_id=d.org_id and t.id=d.task_id where d.org_id=$1 and d.parent_id=$2 and p.status='approved' and t.status not in ('done','cancelled') and not exists (select 1 from task_dependencies d2 join tasks parent2 on parent2.org_id=d2.org_id and parent2.id=d2.parent_id where d2.org_id=p.org_id and d2.task_id=p.task_id and parent2.status <> 'done')`, org, parent)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.riverInsertTx(ctx, tx, ProposalExecuteArgs{OrgID: org, ProposalID: id}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) AddDependency(ctx context.Context, org, task, parent string) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if task == parent {
			return errors.New("dependency cycle")
		}
		var exists bool
		if e := tx.QueryRow(ctx, "select exists(select 1 from tasks where org_id=$1 and id=$2) and exists(select 1 from tasks where org_id=$1 and id=$3)", org, task, parent).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return errors.New("task not found")
		}
		var cycle bool
		e := tx.QueryRow(ctx, `with recursive reach(id) as (select parent_id from task_dependencies where org_id=$1 and task_id=$2 union select d.parent_id from task_dependencies d join reach r on d.task_id=r.id and d.org_id=$1) select exists(select 1 from reach where id=$3)`, org, parent, task).Scan(&cycle)
		if e != nil {
			return e
		}
		if cycle {
			return errors.New("dependency cycle")
		}
		_, e = tx.Exec(ctx, "insert into task_dependencies(org_id,task_id,parent_id) values($1,$2,$3) on conflict do nothing", org, task, parent)
		return e
	})
}
func (s *Store) CancelTask(ctx context.Context, org, id string, version int64) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		r, e := tx.Exec(ctx, "update tasks set status='cancelled',version=version+1 where org_id=$1 and id=$2 and version=$3 and status <> 'done'", org, id, version)
		if e != nil {
			return e
		}
		if r.RowsAffected() != 1 {
			return errors.New("stale task version or task cannot be cancelled")
		}
		_, e = tx.Exec(ctx, "update proposals set status='rejected' where org_id=$1 and task_id=$2 and status in ('pending_endorsement','pending_approval','approved','executing')", org, id)
		return e
	})
}
func (s *Store) CreateAgent(ctx context.Context, org, name, harness, owner string, caps []string) (Agent, error) {
	var x Agent
	b, _ := json.Marshal(caps)
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		id := platform.NewID()
		return tx.QueryRow(ctx, "insert into agents(id,org_id,name,harness,owner_id,capabilities) values($1,$2,$3,$4,$5,$6) returning id,org_id,name,harness,status,owner_id,capabilities", id, org, name, harness, owner, b).Scan(&x.ID, &x.OrgID, &x.Name, &x.Harness, &x.Status, &x.OwnerID, &b)
	})
	_ = json.Unmarshal(b, &x.Capabilities)
	return x, e
}
func (s *Store) CreateProposal(ctx context.Context, org, task, actor, summary string, ops []connectors.Operation) (Proposal, error) {
	var x Proposal
	var b []byte
	var t time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var taskStatus, owner string
		if e := tx.QueryRow(ctx, "select status,owner_id from tasks where org_id=$1 and id=$2 for update", org, task).Scan(&taskStatus, &owner); e != nil {
			return e
		}
		if owner != actor {
			return errors.New("only task owner may create proposal")
		}
		if taskStatus == "done" || taskStatus == "cancelled" {
			return errors.New("terminal task")
		}
		reg := connectors.NewRegistry()
		var rev int64
		if e := tx.QueryRow(ctx, "select coalesce(max(revision),0)+1 from proposals where org_id=$1 and task_id=$2", org, task).Scan(&rev); e != nil {
			return e
		}
		for i := range ops {
			if ops[i].ID == "" {
				ops[i].ID = platform.NewID()
			}
			if ops[i].Integration == "" || ops[i].Action == "" || ops[i].TargetID == "" || ops[i].ExpectedVersion < 1 {
				return errors.New("invalid operation")
			}
			c, ok := reg.Get(ops[i].Integration)
			if !ok || c.Validate(ops[i]) != nil {
				return errors.New("invalid operation")
			}
			var ver int64
			var rb []byte
			if e := tx.QueryRow(ctx, "select version,data from simulator_records where org_id=$1 and id=$2", org, ops[i].TargetID).Scan(&ver, &rb); e != nil {
				return e
			}
			if int64(ops[i].ExpectedVersion) != ver {
				return errors.New("stale record")
			}
			var data map[string]any
			if json.Unmarshal(rb, &data) != nil {
				return errors.New("invalid record")
			}
			if _, e := c.Prepare(connectors.Record{ID: ops[i].TargetID, Version: int(ver), Data: data}, ops[i]); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(ctx, "update proposals set status='superseded' where org_id=$1 and task_id=$2 and status in ('pending_endorsement','pending_approval','approved')", org, task); e != nil {
			return e
		}
		b, _ = json.Marshal(ops)
		id := platform.NewID()
		d := digest(ops)
		if e := tx.QueryRow(ctx, "insert into proposals(id,org_id,task_id,revision,digest,status,summary,operations,created_by) values($1,$2,$3,$4,$5,'pending_endorsement',$6,$7,$8) returning id,org_id,task_id,revision,digest,status,summary,operations,created_at", id, org, task, rev, d, summary, b, actor).Scan(&x.ID, &x.OrgID, &x.TaskID, &x.Revision, &x.Digest, &x.Status, &x.Summary, &b, &t); e != nil {
			return e
		}
		return reserveBusinessOperations(ctx, tx, org, id, ops)
	})
	_ = json.Unmarshal(b, &x.Operations)
	x.CreatedAt = scanTime(&t)
	return x, e
}
func (s *Store) ListProposals(ctx context.Context, org string) ([]Proposal, error) {
	out := []Proposal{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,org_id,task_id,revision,digest,status,summary,operations,created_at from proposals order by created_at")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Proposal
			var b []byte
			var t time.Time
			if e = rows.Scan(&x.ID, &x.OrgID, &x.TaskID, &x.Revision, &x.Digest, &x.Status, &x.Summary, &b, &t); e != nil {
				return e
			}
			_ = json.Unmarshal(b, &x.Operations)
			x.CreatedAt = scanTime(&t)
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) GetProposal(ctx context.Context, org, id string) (Proposal, error) {
	xs, e := s.ListProposals(ctx, org)
	if e != nil {
		return Proposal{}, e
	}
	for _, x := range xs {
		if x.ID == id {
			return x, nil
		}
	}
	return Proposal{}, pgx.ErrNoRows
}
func (s *Store) Decide(ctx context.Context, org, proposal, actor, role, kind string, revision int64, digest, reason string) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, "SELECT role FROM principals WHERE org_id=$1 AND id=$2 AND active", org, actor).Scan(&role); e != nil {
			return errors.New("identity unavailable or revoked")
		}
		var task, status, d, requester string
		var rev int64
		if e := tx.QueryRow(ctx, "select task_id,status,digest,revision,created_by from proposals where org_id=$1 and id=$2 for update", org, proposal).Scan(&task, &status, &d, &rev, &requester); e != nil {
			return e
		}
		if rev != revision || d != digest {
			return errors.New("stale proposal revision or digest")
		}
		if kind == "endorse" && (actor != requester || status != "pending_endorsement") {
			return errors.New("only requester may endorse")
		}
		if (kind == "approve" || kind == "reject") && (actor == requester || (role != "approver" && role != "admin")) {
			return errors.New("separate approver required")
		}
		if kind != "endorse" && kind != "approve" && kind != "reject" {
			return errors.New("invalid decision")
		}
		var inserted bool
		id := platform.NewID()
		e := tx.QueryRow(ctx, "insert into proposal_decisions(id,org_id,proposal_id,actor_id,kind,revision,digest,reason) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict (org_id,proposal_id,actor_id,kind) do nothing returning id", id, org, proposal, actor, kind, revision, digest, reason).Scan(&id)
		if e == pgx.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
		inserted = true
		if !inserted {
			return nil
		}
		switch kind {
		case "endorse":
			_, e = tx.Exec(ctx, "update proposals set status='pending_approval' where org_id=$1 and id=$2 and status='pending_endorsement'", org, proposal)
		case "reject":
			_, e = tx.Exec(ctx, "update proposals set status='rejected' where org_id=$1 and id=$2 and status in ('pending_endorsement','pending_approval')", org, proposal)
			if e == nil {
				e = releaseBusinessOperations(ctx, tx, org, proposal)
			}
		case "approve":
			var taskStatus string
			if e = tx.QueryRow(ctx, "select status from tasks where org_id=$1 and id=$2", org, task).Scan(&taskStatus); e == nil && taskStatus == "cancelled" {
				return errors.New("cancelled task")
			}
			var ar int64
			if e = tx.QueryRow(ctx, "with u as (update proposals set status='approved' where org_id=$1 and id=$2 and status='pending_approval' returning 1) select count(*) from u", org, proposal).Scan(&ar); e == nil && ar != 1 {
				return errors.New("proposal is not pending approval")
			}
			if e == nil {
				_, e = tx.Exec(ctx, "insert into effects(id,org_id,proposal_id,operation_id,integration,action,target_id,expected_version,payload,state,business_operation_id) select gen_random_uuid()::text,p.org_id,p.id,(op->>'id'),(op->>'integration'),(op->>'action'),(op->>'target_id'),(op->>'expected_version')::bigint,op->'payload','authorized',bo.id from proposals p cross join lateral jsonb_array_elements(p.operations) op left join business_operations bo on bo.org_id=p.org_id and bo.integration=(op->>'integration') and bo.action=(op->>'action') and bo.business_key=(op->>'business_key') where p.org_id=$1 and p.id=$2 on conflict do nothing", org, proposal)
			}
			if e == nil {
				_, e = tx.Exec(ctx, "update business_operations set status='dispatching',updated_at=clock_timestamp() where org_id=$1 and proposal_id=$2 and status='reserved'", org, proposal)
			}
			if e == nil {
				e = s.riverInsertTx(ctx, tx, ProposalExecuteArgs{OrgID: org, ProposalID: proposal})
			}
		}
		return e
	})
}
func (s *Store) riverInsertTx(ctx context.Context, tx pgx.Tx, args ProposalExecuteArgs) error {
	client, err := river.NewClient(riverpgxv5.New(nil), &river.Config{})
	if err != nil {
		return err
	}
	_, err = client.InsertTx(ctx, tx, args, nil)
	return err
}

func (s *Store) ListDecisions(ctx context.Context, org, actor, role string) ([]Decision, error) {
	out := []Decision{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT p.id||':'||p.status,p.task_id,p.id,p.revision,p.digest,CASE WHEN p.status='pending_endorsement' THEN 'endorse' ELSE 'approve' END,t.title,p.status FROM proposals p JOIN tasks t ON t.id=p.task_id AND t.org_id=p.org_id WHERE p.org_id=$1 AND t.status NOT IN ('done','cancelled') AND ((p.status='pending_endorsement' AND p.created_by=$2) OR (p.status='pending_approval' AND p.created_by<>$2 AND $3 IN ('approver','admin'))) ORDER BY p.created_at`, org, actor, role)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Decision
			if e = rows.Scan(&x.ID, &x.TaskID, &x.ProposalID, &x.Revision, &x.Digest, &x.Kind, &x.Title, &x.Status); e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) ListEvents(ctx context.Context, org string) ([]Event, error) {
	out := []Event{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,aggregate_id,aggregate_version,event_type,coalesce(actor_id,''),payload,created_at from domain_events order by created_at")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Event
			var t time.Time
			if e = rows.Scan(&x.ID, &x.AggregateID, &x.AggregateVersion, &x.EventType, &x.ActorID, &x.Payload, &t); e != nil {
				return e
			}
			x.CreatedAt = scanTime(&t)
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) ListReceipts(ctx context.Context, org string) ([]Receipt, error) {
	out := []Receipt{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,effect_id,kind,outcome_code,result,created_at from receipts order by created_at")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Receipt
			var b []byte
			var t time.Time
			if e = rows.Scan(&x.ID, &x.EffectID, &x.Kind, &x.OutcomeCode, &b, &t); e != nil {
				return e
			}
			_ = json.Unmarshal(b, &x.Result)
			x.CreatedAt = scanTime(&t)
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) ListRecords(ctx context.Context, org, integration string) ([]Record, error) {
	out := []Record{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select id,integration,version,data from simulator_records where integration=$1 order by id", integration)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x Record
			var b []byte
			if e = rows.Scan(&x.ID, &x.Integration, &x.Version, &b); e != nil {
				return e
			}
			_ = json.Unmarshal(b, &x.Data)
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) CreateHandoff(ctx context.Context, org, task, actor, recipient, role, summary string) (Handoff, error) {
	var x Handoff
	var t time.Time
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if role != "owner" && role != "assignee" {
			return errors.New("unsupported handoff role")
		}
		var v int64
		var owner, assignee string
		if e := tx.QueryRow(ctx, "select version,owner_id,coalesce(assignee_id,'') from tasks where org_id=$1 and id=$2 for update", org, task).Scan(&v, &owner, &assignee); e != nil {
			return e
		}
		if actor != owner && actor != assignee {
			return errors.New("only owner or assignee may create handoff")
		}
		var active bool
		if e := tx.QueryRow(ctx, "select active from principals where org_id=$1 and id=$2", org, recipient).Scan(&active); e != nil || !active {
			return errors.New("recipient inactive")
		}
		id := platform.NewID()
		return tx.QueryRow(ctx, "insert into handoffs(id,org_id,task_id,offered_task_version,recipient_id,role,summary,created_by) values($1,$2,$3,$4,$5,$6,$7,$8) returning id,task_id,recipient_id,role,summary,state,created_at", id, org, task, v, recipient, role, summary, actor).Scan(&x.ID, &x.TaskID, &x.RecipientID, &x.Role, &x.Summary, &x.State, &t)
	})
	x.CreatedAt = scanTime(&t)
	return x, e
}
func (s *Store) AcceptHandoff(ctx context.Context, org, id, actor string) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var task, role, state, source string
		var offered int64
		if e := tx.QueryRow(ctx, "select task_id,role,state,created_by,offered_task_version from handoffs where org_id=$1 and id=$2 and recipient_id=$3 for update", org, id, actor).Scan(&task, &role, &state, &source, &offered); e != nil {
			return e
		}
		if state == "accepted" {
			return nil
		}
		if state != "offered" {
			return errors.New("handoff unavailable")
		}
		var owner, assignee string
		var version int64
		if e := tx.QueryRow(ctx, "select owner_id,coalesce(assignee_id,''),version from tasks where org_id=$1 and id=$2 for update", org, task).Scan(&owner, &assignee, &version); e != nil {
			return e
		}
		if source != owner || version != offered {
			return errors.New("stale handoff")
		}
		var q string
		if role == "owner" {
			q = "update tasks set owner_id=$1,version=version+1 where org_id=$2 and id=$3"
		} else {
			q = "update tasks set assignee_id=$1,version=version+1 where org_id=$2 and id=$3"
		}
		if _, e := tx.Exec(ctx, q, actor, org, task); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "update handoffs set state='accepted',accepted_at=clock_timestamp() where org_id=$1 and id=$2", org, id)
		return e
	})
}

type ProposalExecuteArgs struct {
	OrgID      string `json:"org_id"`
	ProposalID string `json:"proposal_id"`
}

func (ProposalExecuteArgs) Kind() string { return "proposal.execute" }

type proposalExecuteWorker struct {
	river.WorkerDefaults[ProposalExecuteArgs]
	store    *Store
	registry *connectors.Registry
}

func (w *proposalExecuteWorker) Work(ctx context.Context, job *river.Job[ProposalExecuteArgs]) error {
	if job.Args.OrgID == "" || job.Args.ProposalID == "" {
		return errors.New("invalid proposal job arguments")
	}
	return w.store.executeProposal(ctx, w.registry, job.Args.OrgID, job.Args.ProposalID)
}
func (s *Store) RunWorker(ctx context.Context, reg *connectors.Registry) error {
	if reg == nil {
		reg = connectors.NewRegistry()
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &proposalExecuteWorker{store: s, registry: reg})
	river.AddWorker(workers, &agentRunWorker{store: s})
	client, err := river.NewClient(riverpgxv5.New(s.Pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		Workers: workers,
	})
	if err != nil {
		return err
	}
	if err = client.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return client.Stop(context.Background())
}

func (s *Store) executeProposal(ctx context.Context, reg *connectors.Registry, org, pid string) error {
	err := s.applyProposal(ctx, reg, org, pid)
	if errors.Is(err, connectors.ErrStale) || errors.Is(err, connectors.ErrInvalid) || errors.Is(err, connectors.ErrUnknown) {
		return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
			var task string
			e := tx.QueryRow(ctx, "UPDATE proposals SET status='needs_attention',failure_reason='invalid_or_stale_operation' WHERE org_id=$1 AND id=$2 AND status IN ('approved','executing') RETURNING task_id", org, pid).Scan(&task)
			if errors.Is(e, pgx.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "UPDATE tasks SET status='blocked',version=version+1 WHERE org_id=$1 AND id=$2 AND status NOT IN ('done','cancelled')", org, task); e != nil {
				return e
			}
			_, e = tx.Exec(ctx, "UPDATE effects SET state='needs_attention' WHERE org_id=$1 AND proposal_id=$2 AND state='authorized'", org, pid)
			return e
		})
	}
	return err
}

func (s *Store) applyProposal(ctx context.Context, reg *connectors.Registry, org, pid string) error {
	return s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var task, status string
		if e := tx.QueryRow(ctx, "select task_id,status from proposals where org_id=$1 and id=$2 for update", org, pid).Scan(&task, &status); e != nil {
			return e
		}
		if status != "approved" && status != "executing" {
			return nil
		}
		var permitted bool
		var e error
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks t JOIN principals owner ON owner.id=t.owner_id AND owner.org_id=t.org_id WHERE t.id=$2 AND t.org_id=$1 AND t.status NOT IN ('cancelled','done') AND owner.active) AND EXISTS(SELECT 1 FROM proposal_decisions d JOIN principals p ON p.id=d.actor_id AND p.org_id=d.org_id WHERE d.org_id=$1 AND d.proposal_id=$3 AND d.kind='approve' AND p.active AND p.role IN ('approver','admin')) AND EXISTS(SELECT 1 FROM proposal_decisions d JOIN principals p ON p.id=d.actor_id AND p.org_id=d.org_id WHERE d.org_id=$1 AND d.proposal_id=$3 AND d.kind='endorse' AND p.active) AND NOT EXISTS(SELECT 1 FROM task_dependencies d JOIN tasks t ON t.id=d.parent_id AND t.org_id=d.org_id WHERE d.org_id=$1 AND d.task_id=$2 AND t.status<>'done')`, org, task, pid).Scan(&permitted); e != nil {
			return e
		}
		if !permitted {
			var blocked bool
			if e = tx.QueryRow(ctx, `select exists(select 1 from task_dependencies d join tasks p on p.org_id=d.org_id and p.id=d.parent_id where d.org_id=$1 and d.task_id=$2 and p.status <> 'done')`, org, task).Scan(&blocked); e != nil {
				return e
			}
			if blocked {
				_, e = tx.Exec(ctx, "update tasks set status='blocked' where org_id=$1 and id=$2 and status not in ('done','cancelled')", org, task)
				return e
			}
			_, e = tx.Exec(ctx, "UPDATE proposals SET status='needs_attention',failure_reason='authorization_or_dependency_unavailable' WHERE org_id=$1 AND id=$2", org, pid)
			return e
		}
		if _, e := tx.Exec(ctx, "update proposals set status='executing' where id=$1", pid); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, "select id,operation_id,integration,action,target_id,expected_version,payload from effects where org_id=$1 and proposal_id=$2 and state='authorized' for update", org, pid)
		if e != nil {
			return e
		}
		type pendingEffect struct {
			eid, oid, integ, action, target string
			ver                             int64
			payload                         []byte
		}
		var pending []pendingEffect
		for rows.Next() {
			var item pendingEffect
			if e = rows.Scan(&item.eid, &item.oid, &item.integ, &item.action, &item.target, &item.ver, &item.payload); e != nil {
				rows.Close()
				return e
			}
			pending = append(pending, item)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, item := range pending {
			eid, oid, integ, action, target, ver, b := item.eid, item.oid, item.integ, item.action, item.target, item.ver, item.payload
			var cur Record
			var cb []byte
			e = tx.QueryRow(ctx, "select id,integration,version,data from simulator_records where org_id=$1 and id=$2 for update", org, target).Scan(&cur.ID, &cur.Integration, &cur.Version, &cb)
			if e != nil {
				return e
			}
			_ = json.Unmarshal(cb, &cur.Data)
			op := connectors.Operation{ID: oid, Integration: integ, Action: action, TargetID: target, ExpectedVersion: int(ver), Payload: b}
			c, ok := reg.Get(integ)
			if !ok {
				return errors.New("unknown integration")
			}
			next, e := c.Prepare(connectors.Record{ID: cur.ID, Version: int(cur.Version), Data: cur.Data}, op)
			if e != nil {
				return e
			}
			nb, _ := json.Marshal(next.Data)
			if _, e = tx.Exec(ctx, "update simulator_records set version=$1,data=$2 where org_id=$3 and id=$4 and version=$5", next.Version, nb, org, target, cur.Version); e != nil {
				return e
			}
			var observed []byte
			var observedVersion int
			if e = tx.QueryRow(ctx, "select version,data from simulator_records where org_id=$1 and id=$2", org, target).Scan(&observedVersion, &observed); e != nil {
				return e
			}
			var observedData map[string]any
			if json.Unmarshal(observed, &observedData) != nil || observedVersion != next.Version || digest(observedData) != digest(next.Data) {
				return errors.New("simulator readback verification failed")
			}
			rid := platform.NewID()
			if _, e = tx.Exec(ctx, "update effects set state='verified',result=$1 where id=$2", observed, eid); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "insert into receipts(id,org_id,effect_id,kind,outcome_code,result) values($1,$2,$3,'simulator','applied',$4)", rid, org, eid, nb); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, "update business_operations set status='verified',updated_at=clock_timestamp() where org_id=$1 and proposal_id=$2 and status='dispatching'", org, pid)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "update proposals set status='done' where org_id=$1 and id=$2", org, pid)
		if e == nil {
			_, e = tx.Exec(ctx, "update tasks set status='done',version=version+1 where org_id=$1 and id=$2 and status <> 'cancelled'", org, task)
		}
		if e == nil {
			e = s.enqueueEligibleChildren(ctx, tx, org, task)
		}
		return e
	})
}

func Seed(ctx context.Context, s *Store) error {
	return s.WithOrg(ctx, "", func(tx pgx.Tx) error {
		for _, o := range []struct{ id, name string }{{"org-fixture-a", "Fixture Organization A"}, {"org-fixture-b", "Fixture Organization B"}} {
			if _, e := tx.Exec(ctx, "insert into organizations(id,name) values($1,$2) on conflict (id) do update set name=excluded.name", o.id, o.name); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "select set_config('app.org_id',$1,true)", o.id); e != nil {
				return e
			}
			for _, p := range []struct{ id, name, role string }{{o.id + "-requester", "Fixture Requester", "requester"}, {o.id + "-approver", "Fixture Approver", "approver"}, {o.id + "-admin", "Fixture Operator", "admin"}} {
				if _, e := tx.Exec(ctx, "insert into principals(id,org_id,name,role) values($1,$2,$3,$4) on conflict (id) do update set role=excluded.role", p.id, o.id, p.name, p.role); e != nil {
					return e
				}
			}
			if _, e := tx.Exec(ctx, "insert into simulator_records(id,org_id,integration,version,data) values($1,$2,'inventory',1,'{\"quantity\":10}') on conflict (id) do nothing", o.id+"-inventory", o.id); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "insert into simulator_records(id,org_id,integration,version,data) values($1,$2,'mail',1,'{}') on conflict (id) do nothing", o.id+"-mail", o.id); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "insert into simulator_records(id,org_id,integration,version,data) values($1,$2,'documents',1,'{\"title\":\"Fixture\",\"content\":\"Fixture\"}') on conflict (id) do nothing", o.id+"-documents", o.id); e != nil {
				return e
			}
		}
		return nil
	})
}

var _ = strings.TrimSpace
