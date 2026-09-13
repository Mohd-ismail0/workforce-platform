package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationsAppliedOnceAndChecksumProtected(t *testing.T) {
	url := os.Getenv("MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("migration owner required")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "990_test_once.sql")
	// Disposable schema scoped to this test; does not touch other application data.
	sql := `CREATE TABLE workforce_migration_probe (id int PRIMARY KEY); INSERT INTO workforce_migration_probe VALUES(1);`
	if e := os.WriteFile(p, []byte(sql), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := Open(context.Background(), url)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	defer s.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS workforce_migration_probe; DELETE FROM workforce_migrations WHERE version='990_test_once'")
	if e = Migrate(context.Background(), url, dir); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(context.Background(), url, dir); e != nil {
		t.Fatalf("migration repeated: %v", e)
	}
	os.WriteFile(p, []byte(sql+" SELECT 1;"), 0600)
	if e = Migrate(context.Background(), url, dir); e == nil {
		t.Fatal("changed applied migration accepted")
	}
}
