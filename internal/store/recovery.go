package store

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// sweepInterval is how often stranded runs are looked for. The sweep is a bounded,
// indexed predicate, so it can run often without competing with real work.
const sweepInterval = 20 * time.Second

// sweepBatch bounds how many organisations one pass examines, so a single sweep is a
// bounded amount of work no matter how many tenants exist.
const sweepBatch = 50

// maxRecoverPerOrg bounds how many runs one organisation is reclaimed for per pass.
const maxRecoverPerOrg = 20

// sweepExpiredRuns periodically re-queues runs whose worker died mid-flight.
//
// An expired lease only makes a run *eligible* for recovery; nothing arrives to pick
// it up, because the job that could have has already been consumed. Real harnesses run
// for minutes, so the window in which a worker can die holding a claim is wide.
// Recovery therefore has to be scheduled, not merely permitted.
//
// Tenancy: agent_runs is under FORCED row-level security, so stale claims cannot be
// found by scanning tenant tables across organisations — a cross-tenant query returns
// nothing at all. Candidates therefore come from the `organizations` control-plane
// directory, one organisation at a time, each inside its own org-scoped transaction.
// Because that directory can be large, the sweep walks it with a rotating cursor
// instead of always examining the same leading pages: a fixed prefix would silently
// mean "the alphabetically-first few tenants are recovered, everyone else never".
// Worst-case latency for any single tenant is therefore (orgs/batch) × interval.
func (s *Store) sweepExpiredRuns(ctx context.Context) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	cursor := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			recovered, next, err := s.reclaimSweep(ctx, cursor)
			if err != nil {
				// Never fatal: a failed sweep must not take the worker down.
				log.Printf("recovery sweep failed: %v", err)
				continue
			}
			cursor = next
			if recovered > 0 {
				log.Printf("recovery sweep re-queued %d stranded run(s)", recovered)
			}
		}
	}
}

// reclaimSweep examines one page of organisations starting after cursor, recovering
// stale claims. It returns how many runs it recovered and the cursor to use next pass
// (empty once the walk wraps around).
func (s *Store) reclaimSweep(ctx context.Context, cursor string) (int, string, error) {
	orgs, wrapped, err := s.orgsAfter(ctx, cursor, sweepBatch)
	if err != nil {
		return 0, cursor, err
	}
	total := 0
	for _, org := range orgs {
		if ctx.Err() != nil {
			return total, cursor, ctx.Err()
		}
		n, err := s.reclaimInOrg(ctx, org, maxRecoverPerOrg)
		if err != nil {
			// Advance past this organisation so one bad tenant cannot wedge the walk.
			log.Printf("recovery sweep: organisation %s failed: %v", org, err)
			continue
		}
		total += n
	}
	if wrapped || len(orgs) == 0 {
		return total, "", nil
	}
	return total, orgs[len(orgs)-1], nil
}

// orgsAfter returns up to limit organisation ids greater than `after`, in id order,
// and whether the walk reached the end of the directory (so the caller should wrap).
func (s *Store) orgsAfter(ctx context.Context, after string, limit int) ([]string, bool, error) {
	if limit <= 0 {
		limit = sweepBatch
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id FROM organizations WHERE id > $1 ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var orgs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		orgs = append(orgs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return orgs, len(orgs) < limit, nil
}

// ReclaimExpiredRunsForOrg re-queues one organisation's runs whose claim lease has
// expired, returning how many it recovered. Exposed because targeted recovery is a
// real operator action ("recover this tenant now"), and because it lets a test assert
// recovery hermetically without depending on where a tenant sits in the directory.
//
// The run is moved back to queued and its continuation enqueued in the SAME
// transaction, so a reclaimed run never exists in a state where nothing will pick it
// up. Fencing, not this being the only path, is what makes it safe: every publication
// re-checks the claim token, so a displaced holder cannot publish, create a gate, or
// overwrite the new attempt's outcome.
func (s *Store) ReclaimExpiredRunsForOrg(ctx context.Context, org string, limit int) (int, error) {
	return s.reclaimInOrg(ctx, org, limit)
}

// reclaimInOrg reclaims stale claims for one organisation in a single transaction.
func (s *Store) reclaimInOrg(ctx context.Context, org string, limit int) (int, error) {
	if limit <= 0 {
		limit = maxRecoverPerOrg
	}
	var ids []string
	err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		// The candidate set is bounded in SQL BEFORE anything is mutated, and each row
		// is locked SKIP LOCKED so concurrent sweepers cannot fight over the same run.
		// Applying the limit after the UPDATE instead would leave the remaining rows
		// marked queued with no job to pick them up — stranded, not recovered.
		//
		// A NULL lease counts as expired: every claim taken by current code sets an
		// expiry, so NULL means the claim predates leasing and would otherwise strand
		// the run forever.
		//
		// Runs whose task is already terminal are excluded: re-queueing them would
		// resurrect work for business that is closed.
		rows, e := tx.Query(ctx, `
			WITH candidates AS (
				SELECT ar.id
				  FROM agent_runs ar
				 WHERE ar.org_id = $1
				   AND ar.status = 'running'
				   AND (ar.claim_expires_at IS NULL OR ar.claim_expires_at < clock_timestamp())
				   AND EXISTS (
				         SELECT 1 FROM tasks t
				          WHERE t.org_id = ar.org_id AND t.id = ar.task_id
				            AND t.status NOT IN ('done','cancelled'))
				 ORDER BY ar.claim_expires_at NULLS FIRST, ar.id
				 LIMIT $2
				 FOR UPDATE SKIP LOCKED
			)
			UPDATE agent_runs AS r
			   SET status = 'queued',
			       claim_token = '',
			       claim_expires_at = NULL,
			       attempt = attempt + 1
			  FROM candidates AS c
			 WHERE r.org_id = $1 AND r.id = c.id
			RETURNING r.id`, org, limit)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		// Fully drain and close BEFORE issuing further statements: a pgx connection
		// cannot run a new query while a result set on it is still open.
		if e := rows.Err(); e != nil {
			rows.Close()
			return e
		}
		rows.Close()

		for _, id := range ids {
			// Enqueue inside the same transaction, so a reclaimed run never exists in
			// a state where nothing will pick it up.
			if e := s.enqueueAgentRunTx(ctx, tx, AgentRunArgs{OrgID: org, RunID: id}); e != nil {
				return e
			}
			// Audit trail: a run changing hands must be visible to an investigator.
			if _, e := tx.Exec(ctx,
				`INSERT INTO domain_events(id,org_id,aggregate_id,aggregate_version,event_type,actor_id,payload)
				 VALUES($1,$2,$3,$4,'agent_run.reclaimed','system',$5)`,
				platform.NewID(), org, id, 0, []byte(`{"reason":"claim_lease_expired"}`)); e != nil {
				return e
			}
		}
		return nil
	})
	return len(ids), err
}
