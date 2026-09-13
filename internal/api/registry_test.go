package api

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// registryFixture adds an admin principal to staffed org-fixture-a and returns a
// server whose token map includes requester, approver, admin and a second org.
func registryFixture(t *testing.T) (*apiFixture, string) {
	t.Helper()
	f := newFixture(t)
	admin := platform.NewID()
	if err := f.st.WithOrg(context.Background(), "org-fixture-a", func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "INSERT INTO principals(id,org_id,name,role) VALUES($1,'org-fixture-a','Fixture Admin','admin') ON CONFLICT (id) DO NOTHING", admin)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{
		"req":   {ID: "org-fixture-a-requester", OrgID: "org-fixture-a", Role: "requester", Name: "Requester"},
		"app":   {ID: "org-fixture-a-approver", OrgID: "org-fixture-a", Role: "approver", Name: "Approver"},
		"admin": {ID: admin, OrgID: "org-fixture-a", Role: "admin", Name: "Admin"},
		"other": {ID: "org-fixture-b-requester", OrgID: "org-fixture-b", Role: "requester", Name: "Other"},
	}}
	f.h = New(cfg, f.st).Handler()
	return f, admin
}

func createRelease(f *apiFixture, tok, family string) (int, map[string]any) {
	// Unique family+version per run: the shared development database persists
	// between runs and a family/version pair may only be released once.
	unique := platform.NewID()
	family = family + "-" + unique[:8]
	return f.do(tok, "POST", "/api/v1/registry/releases", map[string]any{
		"family": family, "kind": "connector", "version": "1.0.0-" + unique[8:16],
		"digest": "sha256:fixture-digest-value-" + unique, "requested_capabilities": []string{"read:inventory"},
		"simulation": true, "provenance": map[string]any{"source": "fixture"},
		"license_info": map[string]any{"spdx": "MIT"},
	})
}

func TestRegistryLifecycleIsAdminOnlyAndTenantScoped(t *testing.T) {
	f, admin := registryFixture(t)
	code, rel := createRelease(f, "admin", "fixture-registry-family")
	if code != 201 {
		t.Fatalf("create %d %v", code, rel)
	}
	id := rel["id"].(string)
	if rel["state"] != "quarantined" {
		t.Fatalf("new release must start quarantined: %v", rel["state"])
	}

	transition := func(tok, target string, revision any, want int) {
		t.Helper()
		code, body := f.do(tok, "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{
			"expected_version": revision, "target_state": target, "reason": "test",
		})
		if code != want {
			t.Fatalf("transition %s -> %s as %s: got %d want %d (%v)", rel["state"], target, tok, code, want, body)
		}
	}

	// An author cannot verify or activate their own artefact.
	transition("req", "verified", rel["revision"], 409)
	transition("app", "verified", rel["revision"], 409)
	// Skipping stages is rejected even for admin.
	transition("admin", "active", rel["revision"], 409)
	// Legitimate admin path.
	for _, step := range []string{"verified", "approved", "installed", "active"} {
		code, body := f.do("admin", "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{
			"expected_version": rel["revision"], "target_state": step, "reason": "reviewed",
		})
		if code != 200 {
			t.Fatalf("admin step %s: %d %v", step, code, body)
		}
		rel = body
	}
	if rel["state"] != "active" {
		t.Fatalf("expected active, got %v", rel["state"])
	}

	// Other organization cannot read or transition this release.
	if code, _ := f.do("other", "GET", "/api/v1/registry/releases/"+id, nil); code == 200 {
		t.Fatal("cross-org registry read allowed")
	}
	if code, _ := f.do("other", "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{"expected_version": rel["revision"], "target_state": "revoked", "reason": "attack"}); code == 200 {
		t.Fatal("cross-org registry transition allowed")
	}

	// Stale revision is refused.
	if code, _ := f.do("admin", "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{"expected_version": 1, "target_state": "revoked", "reason": "stale"}); code != 409 {
		t.Fatalf("stale revision accepted: %d", code)
	}

	// Revocation is a real terminal state for the active release.
	code, body := f.do("admin", "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{"expected_version": rel["revision"], "target_state": "revoked", "reason": "compromised"})
	if code != 200 || body["state"] != "revoked" {
		t.Fatalf("revoke failed: %d %v", code, body)
	}
	if code, _ := f.do("admin", "POST", "/api/v1/registry/releases/"+id+"/transition", map[string]any{"expected_version": body["revision"], "target_state": "active", "reason": "unrevoke"}); code == 200 {
		t.Fatal("revoked release was un-revoked")
	}
	_ = admin
}

func TestRegistryRequesterMaySubmitButNotGrantCapabilities(t *testing.T) {
	f, _ := registryFixture(t)
	code, rel := createRelease(f, "req", "requester-submitted-family")
	if code != 201 {
		t.Fatalf("requester submission refused: %d %v", code, rel)
	}
	if granted, ok := rel["granted_capabilities"].([]any); !ok || len(granted) != 0 {
		t.Fatalf("requester submission carried granted capabilities: %v", rel["granted_capabilities"])
	}
	// A submitter cannot smuggle granted capabilities through the create body.
	code, smuggled := f.do("req", "POST", "/api/v1/registry/releases", map[string]any{
		"family": "self-grant-" + platform.NewID()[:8], "kind": "connector", "version": "1.0.0",
		"digest": "sha256:self-grant-" + platform.NewID(), "requested_capabilities": []string{"read:inventory"},
		"granted_capabilities": []string{"write:inventory", "network:any"}, "simulation": true,
	})
	if code != 201 {
		t.Fatalf("create with forged grants: %d %v", code, smuggled)
	}
	if granted, ok := smuggled["granted_capabilities"].([]any); !ok || len(granted) != 0 {
		t.Fatalf("submitter self-granted capabilities: %v", smuggled["granted_capabilities"])
	}
	if rel["state"] != "quarantined" {
		t.Fatalf("requester submission not quarantined: %v", rel["state"])
	}
}
