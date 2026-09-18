package testdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteDatabaseSwapsPath(t *testing.T) {
	cases := []struct {
		in, db, want string
	}{
		// Plain password, no special chars: path swaps cleanly.
		{"postgres://user:pass@host:5432/old", "new", "postgres://user:pass@host:5432/new"},
		// Query (options) survives the swap.
		{"postgres://user:pass@host/db?sslmode=require", "fresh", "postgres://user:pass@host/fresh?sslmode=require"},
		// Fragment is dropped (never meaningful in a DSN).
		{"postgres://user:pass@host/db#frag", "x", "postgres://user:pass@host/x"},
	}
	for _, c := range cases {
		if got := rewriteDatabase(c.in, c.db); got != c.want {
			t.Errorf("rewriteDatabase(%q,%q)=%q want %q", c.in, c.db, got, c.want)
		}
	}
}

// A password with characters that url.Parse percent-encodes must still swap the
// path without corrupting the credential. Assert on the parsed components rather
// than a literal re-serialization, because net/url re-encodes delimiters.
func TestRewriteDatabasePreservesEncodedCredential(t *testing.T) {
	in := "postgres://user:abc%2Fdef@host:5432/old"
	got := rewriteDatabase(in, "new")
	if got == in {
		t.Fatalf("path not swapped: %q", got)
	}
	for _, wantToken := range []string{"@host:5432/new", "abc%2Fdef"} {
		var found bool
		for i := 0; i+len(wantToken) <= len(got); i++ {
			if got[i:i+len(wantToken)] == wantToken {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rewriteDatabase(%q) missing %q -> %q", in, wantToken, got)
		}
	}
}

func TestDbNameStableAndShort(t *testing.T) {
	a := dbName("api")
	b := dbName("api")
	if a != b {
		t.Fatalf("dbName not stable: %s vs %s", a, b)
	}
	if a == dbName("store") {
		t.Fatalf("dbName collision between packages")
	}
	if len(a) > 63 {
		t.Fatalf("dbName too long: %d (%s)", len(a), a)
	}
}

// The regression that broke CI: store.Migrate was called with a RELATIVE
// "migrations" path, which `go test` resolves from the package directory
// (internal/testdb/) where no migrations/ exists - globbing zero files and
// silently leaving the fresh per-package database unmigrated. findMigrationsDir
// must walk up to the repo root and find the real migrations directory, because
// the fresh-DB isolation path never ran before CI (local shared-LXC roles
// cannot CREATE DATABASE, so provision() was never reached).
func TestFindMigrationsDirReachesRepoRoot(t *testing.T) {
	dir, err := findMigrationsDir()
	if err != nil {
		t.Fatalf("findMigrationsDir: %v", err)
	}
	// The directory must exist and actually contain .sql migration files.
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatalf("readdir %s: %v", dir, e)
	}
	var sql int
	for _, en := range entries {
		if !en.IsDir() && strings.HasSuffix(en.Name(), ".sql") {
			sql++
		}
	}
	if sql == 0 {
		t.Fatalf("%s contains no .sql files; the migration dir resolved to the wrong place", dir)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "001_initial.sql")); statErr != nil {
		t.Fatalf("001_initial.sql missing in %s: %v", dir, statErr)
	}
}