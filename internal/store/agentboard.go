package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// AgentBoardView is what one agent is actually doing.
//
// The distinction this type exists to preserve: a PARKED run is waiting for a
// person. It is not reasoning, it is not holding a worker, and it must not be
// painted the same colour as active work. "Waiting for a human" is not an active
// reasoning process, and a board that blurred the two would report an idle agent
// as busy — which is exactly the kind of comfortable lie an operations view must
// not tell.
type AgentBoardView struct {
	Agent Agent `json:"agent"`
	// Running is the only state that counts as the agent working.
	Running []AgentRun `json:"running"`
	// Queued has been admitted but not started.
	Queued []AgentRun `json:"queued"`
	// Parked is waiting on a person. Deliberately separate, and never active.
	Parked []AgentRun `json:"parked"`
	// Tasks this agent has worked on that are still open.
	Tasks []Task `json:"tasks"`
	// Questions the agent is blocked on, whoever must answer them.
	OpenGates []Gate `json:"open_gates"`
	// Active is true only when the agent is reasoning right now.
	Active bool `json:"active"`
}

// AgentBoard projects one agent's current state. Terminal runs are omitted: this
// is a board of what is happening, not an audit log.
func (s *Store) AgentBoard(ctx context.Context, org, agentID string) (AgentBoardView, error) {
	v := AgentBoardView{
		Running: []AgentRun{}, Queued: []AgentRun{}, Parked: []AgentRun{},
		Tasks: []Task{}, OpenGates: []Gate{},
	}
	// The agent must be one of this organization's; ListAgents is already
	// tenant-scoped, so a cross-org id simply is not found.
	agents, e := s.ListAgents(ctx, org)
	if e != nil {
		return v, e
	}
	found := false
	for _, a := range agents {
		if a.ID == agentID {
			v.Agent, found = a, true
			break
		}
	}
	if !found {
		// ErrNotFound, not a plain error: a GET of a missing resource is a 404.
		// Answering 409 here would tell a client its request conflicted rather
		// than that the agent does not exist.
		return v, ErrNotFound
	}

	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			"select "+runCols+" from agent_runs where org_id=$1 and agent_id=$2 and status in ('queued','running','waiting') order by created_at",
			org, agentID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			r, e := scanRun(rows)
			if e != nil {
				return e
			}
			switch r.Status {
			case "running":
				v.Running = append(v.Running, r)
			case "queued":
				v.Queued = append(v.Queued, r)
			case "waiting":
				v.Parked = append(v.Parked, r)
			}
		}
		if e := rows.Err(); e != nil {
			return e
		}
		v.Active = len(v.Running) > 0

		// Work this agent has been involved in, still open.
		trows, e := tx.Query(ctx, `
			select t.id,t.org_id,t.title,t.description,t.status,t.owner_id,
			       coalesce(t.assignee_id,''),coalesce(t.project_id,''),t.version,t.created_at
			  from tasks t
			 where t.org_id=$1 and t.status not in ('done','cancelled')
			   and exists (select 1 from agent_runs ar
			                where ar.org_id=t.org_id and ar.task_id=t.id and ar.agent_id=$2)
			 order by t.created_at`, org, agentID)
		if e != nil {
			return e
		}
		defer trows.Close()
		for trows.Next() {
			var t Task
			var ca *time.Time
			if e := trows.Scan(&t.ID, &t.OrgID, &t.Title, &t.Description, &t.Status,
				&t.OwnerID, &t.AssigneeID, &t.ProjectID, &t.Version, &ca); e != nil {
				return e
			}
			t.CreatedAt = scanTime(ca)
			v.Tasks = append(v.Tasks, t)
		}
		if e := trows.Err(); e != nil {
			return e
		}

		// Questions blocking this agent's runs.
		// gateCols is unqualified, and this query joins two tables that both
		// have `id`, so the columns are named explicitly here rather than
		// reusing the constant.
		grows, e := tx.Query(ctx,
			`select g.id,g.org_id,g.task_id,coalesce(g.run_id,''),g.kind,g.prompt,g.input_schema,
			        g.revision,g.status,g.respondent_id,g.response,coalesce(g.responded_by,''),
			        g.responded_at,g.expires_at,g.created_by,g.created_at,g.version
			   from decision_gates g
			   join agent_runs ar on ar.org_id=g.org_id and ar.id=g.run_id
			  where g.org_id=$1 and ar.agent_id=$2 and g.status='pending'
			  order by g.created_at`, org, agentID)
		if e != nil {
			return e
		}
		defer grows.Close()
		for grows.Next() {
			g, e := scanGate(grows)
			if e != nil {
				return e
			}
			v.OpenGates = append(v.OpenGates, g)
		}
		return grows.Err()
	})
	return v, e
}
