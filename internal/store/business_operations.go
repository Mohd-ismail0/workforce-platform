package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
)

var businessKeyRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$`)

func validateBusinessKey(k string) error {
	if !businessKeyRE.MatchString(k) {
		return errors.New("invalid business key")
	}
	return nil
}
func operationFingerprint(op connectors.Operation) string {
	return digest(struct {
		Integration, Action, Target string
		Version                     int
		Payload                     json.RawMessage
	}{op.Integration, op.Action, op.TargetID, op.ExpectedVersion, op.Payload})
}
func reserveBusinessOperations(ctx context.Context, tx pgx.Tx, org, proposal string, ops []connectors.Operation) error {
	for _, op := range ops {
		if err := validateBusinessKey(op.BusinessKey); err != nil {
			return err
		}
		fp := operationFingerprint(op)
		var id, oldFP, oldStatus, oldProposal string
		err := tx.QueryRow(ctx, `SELECT id,fingerprint,status,coalesce(proposal_id,'') FROM business_operations WHERE org_id=$1 AND integration=$2 AND action=$3 AND business_key=$4 FOR UPDATE`, org, op.Integration, op.Action, op.BusinessKey).Scan(&id, &oldFP, &oldStatus, &oldProposal)
		if err == pgx.ErrNoRows {
			id = platform.NewID()
			_, err = tx.Exec(ctx, `INSERT INTO business_operations(id,org_id,integration,action,business_key,fingerprint,proposal_id,status) VALUES($1,$2,$3,$4,$5,$6,$7,'reserved')`, id, org, op.Integration, op.Action, op.BusinessKey, fp, proposal)
		} else if err != nil {
			return err
		} else {
			if oldFP != fp {
				return fmt.Errorf("business operation already reserved for this key with different content: %s", id)
			}
			// A business key protects one official operation. It may move forward only
			// within the SAME task's revision lineage (a deliberate new revision supersedes
			// the previous one) or after the previous holder was rejected. Reuse across
			// tasks, or after an approval/dispatch, requires explicit human resolution.
			var newTask string
			if err := tx.QueryRow(ctx, `SELECT task_id FROM proposals WHERE org_id=$1 AND id=$2`, org, proposal).Scan(&newTask); err != nil {
				return err
			}
			rebindable := oldStatus == "released"
			if !rebindable && oldStatus == "reserved" && oldProposal != "" {
				var oldProposalStatus, oldProposalTask string
				if e := tx.QueryRow(ctx, `SELECT status,task_id FROM proposals WHERE org_id=$1 AND id=$2`, org, oldProposal).Scan(&oldProposalStatus, &oldProposalTask); e == nil {
					rebindable = oldProposalStatus == "rejected" || (oldProposalStatus == "superseded" && oldProposalTask == newTask)
				}
			}
			if !rebindable {
				return fmt.Errorf("business operation already claimed by another task or dispatched: %s", id)
			}
			_, err = tx.Exec(ctx, `UPDATE business_operations SET proposal_id=$1,status='reserved',fingerprint=$2,updated_at=clock_timestamp() WHERE org_id=$3 AND id=$4`, proposal, fp, org, id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func releaseBusinessOperations(ctx context.Context, tx pgx.Tx, org, proposal string) error {
	_, err := tx.Exec(ctx, `UPDATE business_operations SET status='released',updated_at=clock_timestamp() WHERE org_id=$1 AND proposal_id=$2 AND status='reserved'`, org, proposal)
	return err
}

var _ = strings.TrimSpace
