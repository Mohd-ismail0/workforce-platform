package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
	"reflect"
	"time"
	"workforce.local/platform/internal/platform"
)

// maxExactInteger is the largest integer JSON numbers can carry exactly as float64.
const maxExactInteger = 1 << 53

var (
	ErrGateNotFound        = errors.New("gate not found")
	ErrGateNotRespondent   = errors.New("gate respondent unavailable")
	ErrGateRevision        = errors.New("gate revision mismatch")
	ErrGateAlreadyResolved = errors.New("gate already resolved")
	ErrGateInvalidResponse = errors.New("invalid gate response")
)

type Gate struct {
	ID           string          `json:"id"`
	OrgID        string          `json:"org_id"`
	TaskID       string          `json:"task_id"`
	RunID        string          `json:"run_id"`
	Kind         string          `json:"kind"`
	Prompt       string          `json:"prompt"`
	InputSchema  json.RawMessage `json:"input_schema"`
	Revision     int64           `json:"revision"`
	Status       string          `json:"status"`
	RespondentID string          `json:"respondent_id"`
	Response     json.RawMessage `json:"response"`
	RespondedBy  string          `json:"responded_by"`
	RespondedAt  string          `json:"responded_at"`
	ExpiresAt    string          `json:"expires_at"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    string          `json:"created_at"`
	Version      int64           `json:"version"`
}

const gateCols = "id,org_id,task_id,coalesce(run_id,''),kind,prompt,input_schema,revision,status,respondent_id,response,coalesce(responded_by,''),responded_at,expires_at,created_by,created_at,version"

func scanGate(row interface{ Scan(...any) error }) (Gate, error) {
	var g Gate
	var resp, schema []byte
	var ra, ea, ca *time.Time
	e := row.Scan(&g.ID, &g.OrgID, &g.TaskID, &g.RunID, &g.Kind, &g.Prompt, &schema, &g.Revision, &g.Status, &g.RespondentID, &resp, &g.RespondedBy, &ra, &ea, &g.CreatedBy, &ca, &g.Version)
	g.InputSchema = json.RawMessage(schema)
	if resp != nil {
		g.Response = json.RawMessage(resp)
	}
	if ra != nil {
		g.RespondedAt = ra.UTC().Format(time.RFC3339Nano)
	}
	if ea != nil {
		g.ExpiresAt = ea.UTC().Format(time.RFC3339Nano)
	}
	if ca != nil {
		g.CreatedAt = ca.UTC().Format(time.RFC3339Nano)
	}
	return g, e
}
func (s *Store) ListGates(ctx context.Context, org, actor string) ([]Gate, error) {
	out := []Gate{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select "+gateCols+" from decision_gates where org_id=$1 and status='pending' and respondent_id=$2 order by created_at desc", org, actor)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			g, e := scanGate(rows)
			if e != nil {
				return e
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) GetGate(ctx context.Context, org, id string) (Gate, error) {
	var g Gate
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var e error
		g, e = scanGate(tx.QueryRow(ctx, "select "+gateCols+" from decision_gates where org_id=$1 and id=$2", org, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrGateNotFound
		}
		return e
	})
	return g, e
}

// ReadGate returns a gate only to someone entitled to see it: the named
// respondent, the accountable task owner, or an active admin. Tenant isolation
// alone would let any colleague read the question AND the recorded answer, which
// the respondent did not agree to share. Unauthorized readers get not-found rather
// than forbidden, so the gate's existence is not leaked either.
func (s *Store) ReadGate(ctx context.Context, org, id, actor string) (Gate, error) {
	var g Gate
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var err error
		g, err = scanGate(tx.QueryRow(ctx, "select "+gateCols+" from decision_gates where org_id=$1 and id=$2", org, id))
		if err != nil {
			return err
		}
		if g.RespondentID == actor {
			return nil
		}
		var role string
		var active bool
		if err := tx.QueryRow(ctx, "select role,active from principals where org_id=$1 and id=$2", org, actor).Scan(&role, &active); err != nil || !active {
			return ErrGateNotFound
		}
		var owner string
		if err := tx.QueryRow(ctx, "select owner_id from tasks where org_id=$1 and id=$2", org, g.TaskID).Scan(&owner); err != nil {
			return err
		}
		if role == "admin" || owner == actor {
			return nil
		}
		return ErrGateNotFound
	})
	if errors.Is(e, pgx.ErrNoRows) {
		return Gate{}, ErrGateNotFound
	}
	return g, e
}

// jsonEqual reports whether two JSON documents are equivalent as VALUES.
// PostgreSQL jsonb reorders object keys and normalises whitespace, so a stored
// response can never round-trip byte-identically to the request that produced it.
// Comparing raw bytes would reject a legitimate replay of the same answer.
func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func validateGateResponse(schema, response json.RawMessage) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(response, &obj) != nil || obj == nil {
		return ErrGateInvalidResponse
	}
	if len(schema) == 0 || bytes.Equal(bytes.TrimSpace(schema), []byte("{}")) {
		return nil
	}
	var spec struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Requested map[string]struct {
			Type string `json:"type"`
		} `json:"requested"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(schema, &spec) != nil {
		return ErrGateInvalidResponse
	}
	props := spec.Properties
	if props == nil {
		props = spec.Requested
	}
	for k := range obj {
		p, ok := props[k]
		if !ok {
			return ErrGateInvalidResponse
		}
		// Decode with UseNumber. Decoding straight into float64 silently rounds
		// large integers (9007199254740993 becomes ...992), so a human's stated
		// quantity could be recorded as a number they never chose. json.Number
		// preserves the exact literal so we can reject what we cannot represent.
		dec := json.NewDecoder(bytes.NewReader(obj[k]))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			return ErrGateInvalidResponse
		}
		switch p.Type {
		case "string":
			if _, ok := v.(string); !ok {
				return ErrGateInvalidResponse
			}
		case "integer":
			num, ok := v.(json.Number)
			if !ok {
				return ErrGateInvalidResponse
			}
			// Rejects fractions ("2.5"), exponents that are not whole ("1e300"
			// overflows int64), and non-numeric literals in one step.
			iv, err := num.Int64()
			if err != nil {
				return ErrGateInvalidResponse
			}
			// Beyond 2^53 an integer is not exactly representable as float64, so
			// downstream arithmetic could silently change it.
			if iv > maxExactInteger || iv < -maxExactInteger {
				return ErrGateInvalidResponse
			}
		case "number":
			num, ok := v.(json.Number)
			if !ok {
				return ErrGateInvalidResponse
			}
			if f, err := num.Float64(); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return ErrGateInvalidResponse
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return ErrGateInvalidResponse
			}
		default:
			return ErrGateInvalidResponse
		}
	}
	for _, k := range spec.Required {
		if _, ok := obj[k]; !ok {
			return ErrGateInvalidResponse
		}
	}
	return nil
}
func (s *Store) ResolveGate(ctx context.Context, org, id, actor string, rev int64, response json.RawMessage) (Gate, error) {
	var g Gate
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var e error
		g, e = scanGate(tx.QueryRow(ctx, "select "+gateCols+" from decision_gates where org_id=$1 and id=$2 for update", org, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrGateNotFound
		}
		if e != nil {
			return e
		}
		var active bool
		if e = tx.QueryRow(ctx, "select active from principals where org_id=$1 and id=$2", org, actor).Scan(&active); e != nil || !active || g.RespondentID != actor {
			return ErrGateNotRespondent
		}
		if g.Status == "resolved" {
			// Compare by value, never by bytes: PostgreSQL jsonb reorders object keys
			// and normalises whitespace, so a stored response can never round-trip
			// byte-identically to the request that produced it. A byte comparison
			// would reject a legitimate replay of the same answer.
			if g.RespondedBy == actor && jsonEqual(g.Response, response) {
				return nil
			}
			return ErrGateAlreadyResolved
		}
		if g.Status != "pending" {
			return ErrGateAlreadyResolved
		}
		if g.Revision != rev {
			return ErrGateRevision
		}
		if e = validateGateResponse(g.InputSchema, response); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "update decision_gates set status='resolved',response=$1,responded_by=$2,responded_at=clock_timestamp(),version=version+1 where org_id=$3 and id=$4", response, actor, org, id); e != nil {
			return e
		}
		if g.RunID != "" {
			tag, e := tx.Exec(ctx, "update agent_runs set status='queued',claim_token='',claim_expires_at=null where org_id=$1 and id=$2 and status='waiting' and gate_id=$3", org, g.RunID, id)
			if e != nil {
				return e
			}
			// Only a run that was genuinely waiting may be resumed. If it was
			// cancelled or already resumed elsewhere, the answer is still recorded
			// but no continuation is enqueued.
			if tag.RowsAffected() == 1 {
				if e = s.enqueueAgentRunTx(ctx, tx, AgentRunArgs{OrgID: org, RunID: g.RunID}); e != nil {
					return e
				}
			}
		}
		_, e = tx.Exec(ctx, "insert into domain_events(id,org_id,aggregate_id,aggregate_version,event_type,actor_id,payload) values($1,$2,$3,$4,'gate.resolved',$5,$6)", platform.NewID(), org, id, g.Version+1, actor, response)
		return e
	})
	if e == nil {
		g, e = s.GetGate(ctx, org, id)
	}
	return g, e
}
