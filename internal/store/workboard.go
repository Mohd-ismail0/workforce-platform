package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// WorkBoardItem is one piece of work as it relates to a specific person.
//
// The relationship is the point. Work a person is ACCOUNTABLE for, work they are
// EXECUTING on someone else's behalf, and an offer they have not answered are
// three different obligations that happen to involve the same person; a list
// that merges them hides which one the person actually carries. The same task
// therefore appears exactly once, under the strongest relationship that applies.
type WorkBoardItem struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	// Relevance is why this is on the board: accountable, executing or
	// handoff_offered.
	Relevance string `json:"relevance"`
	// The unmet prerequisite holding this up, when there is one. A board that
	// says "blocked" without saying by what is only half a board: the person
	// needs to know whose work is in the way.
	BlockerTaskID  string `json:"blocker_task_id"`
	BlockerTitle   string `json:"blocker_title"`
	BlockerOwnerID string `json:"blocker_owner_id"`
}

// WorkBoard returns the outstanding work associated with one principal, each
// item labelled with why it is theirs.
//
// This is a server-side projection rather than a client filter over all tasks:
// the set a person is allowed to see, and the counts derived from it, must be
// decided by the server, and a client that filters its own copy of everything has
// already been given everything.
//
// Decisions are deliberately NOT included. "My decisions" and "my commitments"
// are different obligations, and a board that folds one into the other turns
// answering a question into doing the work.
func (s *Store) WorkBoard(ctx context.Context, org, actor string) ([]WorkBoardItem, error) {
	out := []WorkBoardItem{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `
			select t.id, t.title, t.status, t.version, t.created_at,
			       case
			         when t.owner_id = $2 then 'accountable'
			         when t.assignee_id = $2 then 'executing'
			         else 'handoff_offered'
			       end as relevance,
			       coalesce(b.id, ''), coalesce(b.title, ''), coalesce(b.owner_id, '')
			  from tasks t
			  left join lateral (
			        -- The unmet prerequisite. Only 'done' clears it: a CANCELLED
			        -- prerequisite still blocks, because the dependent work cannot
			        -- proceed until someone removes or replaces the dependency.
			        select p.id, p.title, p.owner_id
			          from task_dependencies d
			          join tasks p on p.org_id = d.org_id and p.id = d.parent_id
			         where d.org_id = t.org_id and d.task_id = t.id and p.status <> 'done'
			         order by p.created_at
			         limit 1
			  ) b on true
			 where t.org_id = $1
			   and t.status not in ('done','cancelled')
			   and (
			         t.owner_id = $2
			         or t.assignee_id = $2
			         or exists (
			              select 1 from handoffs h
			               where h.org_id = t.org_id and h.task_id = t.id
			                 and h.recipient_id = $2
			                 and h.state in ('offered','clarification_requested')
			                 and (h.expires_at is null or h.expires_at > clock_timestamp())
			            )
			       )
			 order by t.created_at, t.id`, org, actor)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var it WorkBoardItem
			var created time.Time
			if e := rows.Scan(&it.TaskID, &it.Title, &it.Status, &it.Version, &created,
				&it.Relevance, &it.BlockerTaskID, &it.BlockerTitle, &it.BlockerOwnerID); e != nil {
				return e
			}
			it.CreatedAt = scanTime(&created)
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, e
}
