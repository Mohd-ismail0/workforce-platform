package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/runner"
)

// AgentTemplate is a validated, versioned, DECLARATIVE starting point for an
// agent. It names a harness and the scope of capabilities an agent created from
// it may use. It is data: it cannot run anything and cannot widen a ceiling.
type AgentTemplate struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Harness      string   `json:"harness"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	Instructions string   `json:"instructions"`
	// Status is draft | published | retired. Only a published template may be used.
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"created_at"`
}

const templateCols = "id,name,version,harness,description,capabilities,instructions,status,created_by,revision,created_at"

func scanTemplate(row interface{ Scan(...any) error }) (AgentTemplate, error) {
	var x AgentTemplate
	var raw []byte
	var created time.Time
	e := row.Scan(&x.ID, &x.Name, &x.Version, &x.Harness, &x.Description, &raw,
		&x.Instructions, &x.Status, &x.CreatedBy, &x.Revision, &created)
	if e != nil {
		return x, e
	}
	if e := json.Unmarshal(raw, &x.Capabilities); e != nil {
		return x, e
	}
	if x.Capabilities == nil {
		x.Capabilities = []string{}
	}
	sort.Strings(x.Capabilities)
	x.CreatedAt = scanTime(&created)
	return x, nil
}

// CreateAgentTemplate writes a DRAFT. Declarative data only: it is never executed,
// and nothing about it grants authority.
func (s *Store) CreateAgentTemplate(ctx context.Context, org, actor, name, version, harness, description string, capabilities []string, instructions string) (AgentTemplate, error) {
	var x AgentTemplate
	if strings.TrimSpace(name) == "" {
		return x, errors.New("a template name is required")
	}
	if strings.TrimSpace(version) == "" {
		return x, errors.New("a template version is required")
	}
	if strings.TrimSpace(harness) == "" {
		return x, errors.New("a harness is required")
	}
	caps, _ := json.Marshal(capList(capabilities))
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var scanErr error
		x, scanErr = scanTemplate(tx.QueryRow(ctx,
			`insert into agent_templates(id,org_id,name,version,harness,description,capabilities,instructions,created_by)
			 values($1,$2,$3,$4,$5,$6,$7,$8,$9) returning `+templateCols,
			platform.NewID(), org, name, version, harness, description, caps, instructions, actor))
		return scanErr
	})
	return x, e
}

// PublishAgentTemplate makes a draft selectable. Validation is deliberate here
// rather than at draft time: a template naming a harness nothing can run is a
// trap for whoever picks it, and publishing is the moment it becomes pickable.
func (s *Store) PublishAgentTemplate(ctx context.Context, org, actor, id string, expected int64) (AgentTemplate, error) {
	var x AgentTemplate
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if e := requireActivePrincipal(ctx, tx, org, actor); e != nil {
			return e
		}
		var harness, status string
		var rev int64
		if e := tx.QueryRow(ctx, "select harness,status,revision from agent_templates where org_id=$1 and id=$2 for update", org, id).Scan(&harness, &status, &rev); e != nil {
			return ErrNotFound
		}
		if rev != expected {
			return errors.New("stale template revision")
		}
		if status != "draft" {
			return errors.New("only a draft template can be published")
		}
		if !runner.Supported(harness) {
			return errors.New("template names a harness no configured runner supports")
		}
		var e error
		x, e = scanTemplate(tx.QueryRow(ctx,
			`update agent_templates set status='published', revision=revision+1, updated_at=clock_timestamp()
			  where org_id=$1 and id=$2 returning `+templateCols, org, id))
		return e
	})
	return x, e
}

// ListAgentTemplates returns every template in the organization, drafts included:
// the directory is for choosing, and hiding drafts would make them unfindable to
// the person who wrote them.
func (s *Store) ListAgentTemplates(ctx context.Context, org string) ([]AgentTemplate, error) {
	out := []AgentTemplate{}
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "select "+templateCols+" from agent_templates where org_id=$1 order by name, version", org)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			x, e := scanTemplate(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, e
}

// GetAgentTemplate reads one template.
func (s *Store) GetAgentTemplate(ctx context.Context, org, id string) (AgentTemplate, error) {
	var x AgentTemplate
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var e error
		x, e = scanTemplate(tx.QueryRow(ctx, "select "+templateCols+" from agent_templates where org_id=$1 and id=$2", org, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return e
	})
	return x, e
}

// CreateAgentFromTemplate creates an agent pinned to a template VERSION.
//
// Customization is SCOPED: the requested capabilities must be a subset of the
// template's. Narrowing is the point — "start from a template, turn some of it
// off". Widening is refused, because otherwise starting from a template would be
// a way to declare anything at all.
func (s *Store) CreateAgentFromTemplate(ctx context.Context, org, actor, name, templateID string, capabilities []string, instructions string) (Agent, error) {
	var x Agent
	// Declared outside the closure so the inserted capability list can be read
	// back after it returns.
	var raw []byte
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		var version, harness, status string
		if e := tx.QueryRow(ctx, "select version,harness,status,capabilities from agent_templates where org_id=$1 and id=$2", org, templateID).Scan(&version, &harness, &status, &raw); e != nil {
			return ErrNotFound
		}
		if status != "published" {
			return errors.New("template is not published")
		}
		var scope []string
		_ = json.Unmarshal(raw, &scope)

		// Default to the template's scope; otherwise the caller must stay within it.
		caps := scope
		if capabilities != nil {
			var beyond []string
			for _, c := range capabilities {
				if !containsCap(scope, c) {
					beyond = append(beyond, c)
				}
			}
			if len(beyond) > 0 {
				sort.Strings(beyond)
				return errors.New("capabilities outside the template's scope: " + strings.Join(beyond, ", "))
			}
			caps = capabilities
		}
		// Instructions may be replaced, but an empty string means "use the
		// template's", not "no instructions".
		if strings.TrimSpace(instructions) == "" {
			if e := tx.QueryRow(ctx, "select instructions from agent_templates where org_id=$1 and id=$2", org, templateID).Scan(&instructions); e != nil {
				return e
			}
		}
		b, _ := json.Marshal(capList(caps))
		id := platform.NewID()
		return tx.QueryRow(ctx,
			`insert into agents(id,org_id,name,harness,owner_id,capabilities,template_id,template_version,instructions)
			 values($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 returning id,org_id,name,harness,status,owner_id,capabilities,template_id,template_version,instructions`,
			id, org, name, harness, actor, b, templateID, version, instructions,
		).Scan(&x.ID, &x.OrgID, &x.Name, &x.Harness, &x.Status, &x.OwnerID, &raw, &x.TemplateID, &x.TemplateVersion, &x.Instructions)
	})
	if e != nil {
		return x, e
	}
	_ = json.Unmarshal(raw, &x.Capabilities)
	if x.Capabilities == nil {
		x.Capabilities = []string{}
	}
	sort.Strings(x.Capabilities)
	return x, nil
}

// GetAgent reads one agent. Missing is ErrNotFound so the HTTP layer answers 404.
func (s *Store) GetAgent(ctx context.Context, org, id string) (Agent, error) {
	var a Agent
	var raw []byte
	e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		e := tx.QueryRow(ctx,
			`select id,org_id,name,harness,status,owner_id,capabilities,
			        coalesce(template_id,''),coalesce(template_version,''),instructions
			   from agents where org_id=$1 and id=$2`, org, id).
			Scan(&a.ID, &a.OrgID, &a.Name, &a.Harness, &a.Status, &a.OwnerID, &raw, &a.TemplateID, &a.TemplateVersion, &a.Instructions)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return e
	})
	if e != nil {
		return a, e
	}
	_ = json.Unmarshal(raw, &a.Capabilities)
	if a.Capabilities == nil {
		a.Capabilities = []string{}
	}
	return a, nil
}
