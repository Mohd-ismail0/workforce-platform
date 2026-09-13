package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) ListHandoffs(ctx context.Context, org, actor string) ([]Handoff, error) {
	out := []Handoff{}
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select h.id,h.task_id,h.recipient_id,h.role,h.summary,h.state,h.created_at,h.created_by from handoffs h join tasks t on t.org_id=h.org_id and t.id=h.task_id where h.org_id=$1 and (h.created_by=$2 or h.recipient_id=$2) and t.status not in ('done','cancelled') and h.state in ('offered','accepted') order by h.created_at`, org, actor)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Handoff
			var tm time.Time
			if err = rows.Scan(&h.ID, &h.TaskID, &h.RecipientID, &h.Role, &h.Summary, &h.State, &tm, &h.CreatedBy); err != nil {
				return err
			}
			h.CreatedAt = scanTime(&tm)
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) PeekHandoff(ctx context.Context, org, id, actor string) (Handoff, error) {
	var h Handoff
	var tm time.Time
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select h.id,h.task_id,h.recipient_id,h.role,h.summary,h.state,h.created_at,h.created_by from handoffs h join tasks t on t.org_id=h.org_id and t.id=h.task_id where h.org_id=$1 and h.id=$2 and (h.created_by=$3 or h.recipient_id=$3) and t.status not in ('done','cancelled')`, org, id, actor).Scan(&h.ID, &h.TaskID, &h.RecipientID, &h.Role, &h.Summary, &h.State, &tm, &h.CreatedBy)
	})
	h.CreatedAt = scanTime(&tm)
	return h, err
}
