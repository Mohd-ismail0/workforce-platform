// Package testdb provisions an isolated throwaway PostgreSQL database for one
// Go package's test binary and runs the tests against it.
//
// The api tests start real River workers on the DEFAULT database and queue, so
// packages that share a database steal each other's jobs. The original
// workaround was `go test -p 1` (serialise every package), which is safe but
// removes parallelism and lets one slow package stall the suite.
//
// When an operator supplies TEST_MASTER_DATABASE_URL (a role with CREATEDB,
// e.g. the postgres superuser in CI), Main gives the calling package its OWN
// database, migrated and fresh, so the api package and store/runner can run in
// parallel without touching each other's state. When the variable is absent
// (the local shared LXC scopes every role to one database and cannot CREATE
// DATABASE), Main degrades to the legacy shared-database behaviour and the
// caller must keep `-p 1`.
package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"testing"

	"workforce.local/platform/internal/store"
)

// Package is the current package name (e.g. "api"); it names the throwaway
// database so two package binaries can never collide.
var Package string

// Main runs the package test binary. It provisions and migrates a per-package
// database when a master URL is available, sets DATABASE_URL /
// MIGRATION_DATABASE_URL for the process, runs the tests, drops the database,
// and returns the exit code the caller should pass to os.Exit.
func Main(m *testing.M) int {
	if Package == "" {
		fmt.Fprintln(os.Stderr, "testdb: Package not set")
		return 1
	}
	master := os.Getenv("TEST_MASTER_DATABASE_URL")
	if master == "" {
		return runShared(m)
	}
	name := dbName(Package)
	if e := provision(context.Background(), master, name); e != nil {
		// Provisioning can fail when the master role cannot CREATE DATABASE
		// (e.g. the local shared-LXC operator role). Degrade to the shared
		// database so local development keeps working; the caller keeps -p 1
		// for correctness in that mode. CI (superuser) never hits this.
		fmt.Fprintf(os.Stderr, "testdb: isolated provisioning for %s failed (%v); falling back to shared DATABASE_URL\n", Package, e)
		return runShared(m)
	}
	url := rewriteDatabase(master, name)
	os.Setenv("DATABASE_URL", url)
	os.Setenv("MIGRATION_DATABASE_URL", url)
	code := m.Run()
	if e := drop(context.Background(), master, name); e != nil {
		fmt.Fprintln(os.Stderr, "testdb: drop", Package, ":", e)
	}
	return code
}

// runShared runs the tests against the shared DATABASE_URL, the legacy local
// behaviour. The caller must serialise packages with -p 1 in this mode.
func runShared(m *testing.M) int {
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "testdb: neither TEST_MASTER_DATABASE_URL nor DATABASE_URL set")
		return 1
	}
	return m.Run()
}

// dbName derives a stable database name shorter than PostgreSQL's 63-byte
// identifier limit.
func dbName(pkg string) string {
	sum := sha256.Sum256([]byte(pkg))
	return "wf_" + hex.EncodeToString(sum[:4])
}

// provision creates (if absent) the package database and migrates it.
func provision(ctx context.Context, master, name string) error {
	admin := rewriteDatabase(master, "postgres")
	if e := createDB(ctx, admin, name); e != nil {
		return e
	}
	// `go test` runs with the working directory set to the PACKAGE directory
	// (internal/api/...), not the repo root, so a relative "migrations" path
	// would glob an empty directory and leave the fresh database unmigrated
	// (silently: Glob returns no files, Migrate returns nil, then every test
	// fails on missing relations). Walk up from the package dir to the repo's
	// migrations/ directory. This is exactly what the shared-local path never
	// exercised, which is why CI caught it.
	migDir, err := findMigrationsDir()
	if err != nil {
		return err
	}
	if e := store.Migrate(ctx, rewriteDatabase(master, name), migDir); e != nil {
		return fmt.Errorf("migrate %s: %w", name, e)
	}
	return nil
}

// findMigrationsDir walks up from the current (package) working directory until
// it finds the repository's migrations/ directory, so migrations apply against
// the real files regardless of the package that hosts the test.
func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		cand := filepath.Join(dir, "migrations")
		if fi, e := os.Stat(cand); e == nil && fi.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("migrations/ not found above %s", wd)
		}
		dir = parent
	}
}

func createDB(ctx context.Context, adminURL, name string) error {
	pool, e := store.Open(ctx, adminURL)
	if e != nil {
		return e
	}
	defer pool.Close()
	// CREATE DATABASE cannot run inside a transaction block; the direct pool
	// connection executes it standalone. Idempotent: a left-over database from
	// a crashed run is dropped and recreated so the suite starts fresh.
	if _, e = pool.Pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(name))); e != nil {
		return e
	}
	_, e = pool.Pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(name)))
	return e
}

func drop(ctx context.Context, master, name string) error {
	admin := rewriteDatabase(master, "postgres")
	pool, e := store.Open(ctx, admin)
	if e != nil {
		return e
	}
	defer pool.Close()
	_, e = pool.Pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(name)))
	return e
}

// rewriteDatabase swaps the database path of a postgres:// URL, preserving any
// trailing query (options) but stripping a fragment.
func rewriteDatabase(raw, db string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return raw
	}
	u.Path = path.Join("/", db)
	u.Fragment = ""
	return u.String()
}

func quoteIdent(s string) string { return `"` + s + `"` }