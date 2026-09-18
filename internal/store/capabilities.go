package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Capability configuration
//
// A capability is what an agent is permitted to DO, and the ceiling on it is an
// operator decision recorded on the harness release. Two things follow, and both
// are enforced rather than documented:
//
//   - An employee configuring an agent cannot raise that ceiling. Declaring more
//     than the operator granted is refused, not trimmed.
//   - Increasing what an agent may do requires a NEW grant. That is the approval
//     step for a privilege change, and it is recorded against the release.
//
// A capability is still not authority. Nothing here makes an agent able to
// approve an official change; that needs a principal row, which agents do not
// have. "May prepare a proposal" and "may authorise an effect" are different
// kinds of thing.

// CapabilityCheck is the answer to "what would happen if this was requested".
// It exists so a denied request is visible BEFORE anything is launched, rather
// than discovered when a run fails.
type CapabilityCheck struct {
	Permitted []string `json:"permitted"`
	Denied    []string `json:"denied"`
}

// AgentConfigurationView is the effective configuration of one agent: what it
// declares, the ceiling its harness permits, anything declared outside that
// ceiling, and the headroom left.
type AgentConfigurationView struct {
	AgentID   string   `json:"agent_id"`
	Harness   string   `json:"harness"`
	Declared  []string `json:"declared"`
	Permitted []string `json:"permitted"`
	// Denied is declared-but-not-permitted. For a valid agent this is empty,
	// which is exactly why it is reported: a non-empty value means the invariant
	// has been broken somewhere and should be loud.
	Denied []string `json:"denied"`
	// Available is permitted-but-not-declared: the unused headroom, which is what
	// a person would need a new grant to use.
	Available []string `json:"available"`
}

// permittedCapabilities returns the ceiling for a harness: the capabilities an
// operator granted its ACTIVE release. A harness with no active release permits
// nothing, which is the correct default rather than an accident.
func permittedCapabilities(ctx context.Context, tx pgx.Tx, org, harness string) ([]string, error) {
	var raw []byte
	e := tx.QueryRow(ctx, `
		select granted_capabilities from registry_releases
		 where org_id=$1 and kind='harness' and state='active'
		   and coalesce(nullif(manifest->>'runner_id',''), family) = $2
		 order by updated_at desc limit 1`, org, harness).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return []string{}, nil
	}
	if e != nil {
		return nil, e
	}
	var out []string
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	sort.Strings(out)
	return out, nil
}

func containsCap(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// GrantRegistryCapabilities records an OPERATOR decision to widen what a release
// may do. Grants accumulate; they are never replaced by a later partial grant.
//
// Only an admin may do this, and it is deliberately a separate act from
// submitting the release: the submitter cannot widen their own ceiling.
func (s *Store) GrantRegistryCapabilities(ctx context.Context, org, actor, role, releaseID string, caps []string, expected int64) (RegistryRelease, error) {
	var x RegistryRelease
	if role != "admin" {
		return x, errors.New("admin role required to grant capabilities")
	}
	if len(caps) == 0 {
		return x, errors.New("no capabilities supplied")
	}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var rev int64
		var raw []byte
		if e := tx.QueryRow(ctx, "select revision,granted_capabilities from registry_releases where org_id=$1 and id=$2 for update", org, releaseID).Scan(&rev, &raw); e != nil {
			return ErrNotFound
		}
		if rev != expected {
			return errors.New("registry version conflict")
		}
		var existing []string
		_ = json.Unmarshal(raw, &existing)
		merged := append([]string{}, existing...)
		for _, c := range caps {
			if !containsCap(merged, c) {
				merged = append(merged, c)
			}
		}
		sort.Strings(merged)
		b, _ := json.Marshal(merged)
		var e error
		x, e = scanRegistry(tx.QueryRow(ctx,
			`update registry_releases set granted_capabilities=$3, revision=revision+1, updated_at=clock_timestamp()
			  where org_id=$1 and id=$2 returning `+registryCols, org, releaseID, b))
		return e
	})
	return x, e
}

// CheckCapabilities answers a prospective request without creating anything.
func (s *Store) CheckCapabilities(ctx context.Context, org, harness string, requested []string) (CapabilityCheck, error) {
	out := CapabilityCheck{Permitted: []string{}, Denied: []string{}}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		permitted, e := permittedCapabilities(ctx, tx, org, harness)
		if e != nil {
			return e
		}
		for _, c := range requested {
			if containsCap(permitted, c) {
				out.Permitted = append(out.Permitted, c)
			} else {
				out.Denied = append(out.Denied, c)
			}
		}
		sort.Strings(out.Permitted)
		sort.Strings(out.Denied)
		return nil
	})
	return out, e
}

// AgentConfiguration reports an agent's effective configuration.
func (s *Store) AgentConfiguration(ctx context.Context, org, agentID string) (AgentConfigurationView, error) {
	out := AgentConfigurationView{Declared: []string{}, Permitted: []string{}, Denied: []string{}, Available: []string{}}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var harness string
		var raw []byte
		if e := tx.QueryRow(ctx, "select harness,capabilities from agents where org_id=$1 and id=$2", org, agentID).Scan(&harness, &raw); e != nil {
			return ErrNotFound
		}
		var declared []string
		_ = json.Unmarshal(raw, &declared)
		if declared == nil {
			declared = []string{}
		}
		sort.Strings(declared)
		out.AgentID = agentID
		out.Harness = harness
		out.Declared = declared

		permitted, e := permittedCapabilities(ctx, tx, org, harness)
		if e != nil {
			return e
		}
		out.Permitted = permitted
		for _, c := range declared {
			if !containsCap(permitted, c) {
				out.Denied = append(out.Denied, c)
			}
		}
		for _, c := range permitted {
			if !containsCap(declared, c) {
				out.Available = append(out.Available, c)
			}
		}
		return nil
	})
	return out, e
}

// admissionMissing returns the agent's declared capabilities that the ACTIVE
// release it would run under does not grant.
//
// This is the admission gate. It is enforced when a run is admitted rather than
// when the agent definition is written, because that is where the effective
// ceiling is actually known: an agent can be described before its harness is
// installed, and "missing required capabilities fail admission with a
// diagnostic" is the rule the platform commits to.
func admissionMissing(ctx context.Context, tx pgx.Tx, org, agentID, releaseID string) ([]string, error) {
	var agentCaps, grantedRaw []byte
	if e := tx.QueryRow(ctx, "select capabilities from agents where org_id=$1 and id=$2", org, agentID).Scan(&agentCaps); e != nil {
		return nil, e
	}
	if e := tx.QueryRow(ctx, "select granted_capabilities from registry_releases where org_id=$1 and id=$2", org, releaseID).Scan(&grantedRaw); e != nil {
		return nil, e
	}
	var declared, granted []string
	_ = json.Unmarshal(agentCaps, &declared)
	_ = json.Unmarshal(grantedRaw, &granted)
	missing := []string{}
	for _, c := range declared {
		if !containsCap(granted, c) {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
