package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ListHandoffs returns the offers a principal is party to (creator or
// recipient) for work that is still live. Offers whose lifetime has passed are
// first expired, so a stale offer is never presented as actionable.
func (s *Store) ListHandoffs(ctx context.Context, org, actor string) ([]Handoff, error) {
	out := []Handoff{}
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		// Expire this organization's due offers before reading, so the list
		// reflects the lifecycle rather than freezing a stale state.
		if _, e := tx.Exec(ctx,
			`update handoffs set state='expired', resolved_at=clock_timestamp()
			 where org_id=$1 and state in ('offered','clarification_requested')
			   and expires_at is not null and expires_at <= clock_timestamp()`, org); e != nil {
			return e
		}
		rows, err := tx.Query(ctx, `select h.id,h.task_id,h.recipient_id,h.role,h.summary,h.state,h.created_at,h.created_by,h.expires_at,h.resolved_at,h.reason,h.hop_depth from handoffs h join tasks t on t.org_id=h.org_id and t.id=h.task_id where h.org_id=$1 and (h.created_by=$2 or h.recipient_id=$2) and t.status not in ('done','cancelled') and h.state in ('offered','accepted','clarification_requested') order by h.created_at`, org, actor)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Handoff
			var tm time.Time
			var exp, res *time.Time
			if err = rows.Scan(&h.ID, &h.TaskID, &h.RecipientID, &h.Role, &h.Summary, &h.State, &tm, &h.CreatedBy, &exp, &res, &h.Reason, &h.HopDepth); err != nil {
				return err
			}
			h.CreatedAt = scanTime(&tm)
			h.ExpiresAt = scanTime(exp)
			h.ResolvedAt = scanTime(res)
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// PeekHandoff reads one offer the actor is party to. Expiry is applied first, so
// an offer that has run out of time is reported as expired rather than offered.
func (s *Store) PeekHandoff(ctx context.Context, org, id, actor string) (Handoff, error) {
	var h Handoff
	var tm time.Time
	var exp, res *time.Time
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := expireDueHandoffs(ctx, tx, org, id); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `select h.id,h.task_id,h.recipient_id,h.role,h.summary,h.state,h.created_at,h.created_by,h.expires_at,h.resolved_at,h.reason,h.hop_depth from handoffs h join tasks t on t.org_id=h.org_id and t.id=h.task_id where h.org_id=$1 and h.id=$2 and (h.created_by=$3 or h.recipient_id=$3) and t.status not in ('done','cancelled')`, org, id, actor).Scan(&h.ID, &h.TaskID, &h.RecipientID, &h.Role, &h.Summary, &h.State, &tm, &h.CreatedBy, &exp, &res, &h.Reason, &h.HopDepth)
	})
	h.CreatedAt = scanTime(&tm)
	h.ExpiresAt = scanTime(exp)
	h.ResolvedAt = scanTime(res)
	return h, err
}
