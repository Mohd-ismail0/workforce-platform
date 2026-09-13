package api

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"workforce.local/platform/internal/platform"
)

func TestInactiveIdentityRejected(t *testing.T) {
	f := newFixture(t)
	uid := platform.NewID()
	err := f.st.WithOrg(context.Background(), "org-fixture-a", func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "INSERT INTO principals(id,org_id,name,role,active) VALUES($1,'org-fixture-a','revoked','requester',false)", uid)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{"revoked": {ID: uid, OrgID: "org-fixture-a", Role: "requester"}}}
	f.h = New(cfg, f.st).Handler()
	code, _ := f.do("revoked", "GET", "/api/v1/me", nil)
	if code != 401 {
		t.Fatalf("inactive identity accepted: %d", code)
	}
}
