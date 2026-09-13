package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/registry"
)

type RegistryRelease struct {
	ID                    string          `json:"id"`
	OrgID                 string          `json:"org_id"`
	Family                string          `json:"family"`
	Kind                  string          `json:"kind"`
	Version               string          `json:"version"`
	Digest                string          `json:"digest"`
	State                 string          `json:"state"`
	Manifest              json.RawMessage `json:"manifest"`
	RequestedCapabilities []string        `json:"requested_capabilities"`
	GrantedCapabilities   []string        `json:"granted_capabilities"`
	CompatibilityRange    string          `json:"compatibility_range"`
	Simulation            bool            `json:"simulation"`
	Provenance            map[string]any  `json:"provenance"`
	LicenseInfo           map[string]any  `json:"license_info"`
	CreatedBy             string          `json:"created_by"`
	Revision              int64           `json:"revision"`
	CreatedAt             string          `json:"created_at"`
	UpdatedAt             string          `json:"updated_at"`
}

type RegistryCreate struct {
	Family                string          `json:"family"`
	Kind                  string          `json:"kind"`
	Version               string          `json:"version"`
	Digest                string          `json:"digest"`
	Manifest              json.RawMessage `json:"manifest"`
	RequestedCapabilities []string        `json:"requested_capabilities"`
	GrantedCapabilities   []string        `json:"granted_capabilities"`
	CompatibilityRange    string          `json:"compatibility_range"`
	Simulation            bool            `json:"simulation"`
	Provenance            map[string]any  `json:"provenance"`
	LicenseInfo           map[string]any  `json:"license_info"`
}

func capList(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func scanRegistry(row interface{ Scan(...any) error }) (RegistryRelease, error) {
	var x RegistryRelease
	var req, grant, prov, lic []byte
	var ca, ua time.Time
	e := row.Scan(&x.ID, &x.OrgID, &x.Family, &x.Kind, &x.Version, &x.Digest, &x.State, &x.Manifest, &req, &grant, &x.CompatibilityRange, &x.Simulation, &prov, &lic, &x.CreatedBy, &x.Revision, &ca, &ua)
	if e != nil {
		return x, e
	}
	_ = json.Unmarshal(req, &x.RequestedCapabilities)
	_ = json.Unmarshal(grant, &x.GrantedCapabilities)
	if x.RequestedCapabilities == nil {
		x.RequestedCapabilities = []string{}
	}
	if x.GrantedCapabilities == nil {
		x.GrantedCapabilities = []string{}
	}
	_ = json.Unmarshal(prov, &x.Provenance)
	_ = json.Unmarshal(lic, &x.LicenseInfo)
	x.CreatedAt = ca.UTC().Format(time.RFC3339Nano)
	x.UpdatedAt = ua.UTC().Format(time.RFC3339Nano)
	return x, nil
}

const registryCols = "id,org_id,family,kind,version,digest,state,manifest,requested_capabilities,granted_capabilities,compatibility_range,simulation,provenance,license_info,created_by,revision,created_at,updated_at"

func (s *Store) CreateRegistryRelease(ctx context.Context, org, actor string, q RegistryCreate) (RegistryRelease, error) {
	var x RegistryRelease
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		id := platform.NewID()
		man := q.Manifest
		if len(man) == 0 {
			man = []byte(`{}`)
		}
		req, _ := json.Marshal(capList(q.RequestedCapabilities))
		// Granted capabilities are an operator decision, never supplied by the submitter.
		grant, _ := json.Marshal([]string{})
		prov, _ := json.Marshal(q.Provenance)
		lic, _ := json.Marshal(q.LicenseInfo)
		var err error
		x, err = scanRegistry(tx.QueryRow(ctx, "insert into registry_releases(id,org_id,family,kind,version,digest,manifest,requested_capabilities,granted_capabilities,compatibility_range,simulation,provenance,license_info,created_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) returning "+registryCols, id, org, q.Family, q.Kind, q.Version, q.Digest, man, req, grant, q.CompatibilityRange, q.Simulation, prov, lic, actor))
		return err
	})
	return x, e
}
func (s *Store) ListRegistryReleases(ctx context.Context, org string) ([]RegistryRelease, error) {
	out := []RegistryRelease{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select "+registryCols+" from registry_releases order by created_at,id")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			x, e := scanRegistry(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}
func (s *Store) GetRegistryRelease(ctx context.Context, org, id string) (RegistryRelease, error) {
	var x RegistryRelease
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var err error
		x, err = scanRegistry(tx.QueryRow(ctx, "select "+registryCols+" from registry_releases where org_id=$1 and id=$2", org, id))
		return err
	})
	return x, e
}
func (s *Store) TransitionRegistryRelease(ctx context.Context, org, id, actor, role, target, reason string, expected int64) (RegistryRelease, error) {
	var x RegistryRelease
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var from string
		var rev int64
		e := tx.QueryRow(ctx, "select state,revision from registry_releases where org_id=$1 and id=$2 for update", org, id).Scan(&from, &rev)
		if e != nil {
			return e
		}
		if !registry.CanTransition(from, target) {
			return errors.New("invalid registry lifecycle transition")
		}
		if !registry.CanTransitionAs(role, target) {
			return errors.New("admin role required for this transition")
		}
		if rev != expected {
			return errors.New("registry version conflict")
		}
		_, e = tx.Exec(ctx, "update registry_releases set state=$1,revision=revision+1,updated_at=clock_timestamp() where org_id=$2 and id=$3 and revision=$4", target, org, id, expected)
		if e != nil {
			return fmt.Errorf("registry transition: %w", e)
		}
		x, e = scanRegistry(tx.QueryRow(ctx, "select "+registryCols+" from registry_releases where org_id=$1 and id=$2", org, id))
		return e
	})
	return x, e
}
