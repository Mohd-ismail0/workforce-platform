package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrate serializes migration sessions and refuses changed previously applied SQL.
// The legacy baseline is adopted only with an explicit operator option.
func Migrate(ctx context.Context, url, dir string) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock(739015321)"); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(739015321)")
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS workforce_migrations(version text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT clock_timestamp()); ALTER TABLE workforce_migrations ADD COLUMN IF NOT EXISTS checksum text;`); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	for _, file := range files {
		body, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(body)
		sum := hex.EncodeToString(hash[:])
		version := strings.TrimSuffix(filepath.Base(file), ".sql")
		var stored *string
		e = conn.QueryRow(ctx, "SELECT checksum FROM workforce_migrations WHERE version=$1", version).Scan(&stored)
		if e == nil {
			if stored == nil {
				if os.Getenv("WORKFORCE_ADOPT_LEGACY_MIGRATIONS") != "1" {
					return fmt.Errorf("migration %s lacks checksum; explicit baseline adoption required", version)
				}
				if _, e = conn.Exec(ctx, "UPDATE workforce_migrations SET checksum=$2 WHERE version=$1 AND checksum IS NULL", version, sum); e != nil {
					return e
				}
				continue
			}
			if *stored != sum {
				// A corrected migration is a deliberate, reviewed act: the fresh-install
				// path may need a fix that an already-migrated database never re-runs.
				// Re-recording the checksum therefore requires an explicit operator flag
				// and is reported, never silent - otherwise the guard would be
				// meaningless. Read the reason before using it: only a migration whose
				// change is provably a no-op for already-migrated databases may be
				// repaired this way.
				if os.Getenv("WORKFORCE_REPAIR_MIGRATION_CHECKSUMS") != "1" {
					return fmt.Errorf("applied migration checksum mismatch: %s (a corrected migration requires WORKFORCE_REPAIR_MIGRATION_CHECKSUMS=1 and review)", version)
				}
				if _, e = conn.Exec(ctx, "UPDATE workforce_migrations SET checksum=$2 WHERE version=$1", version, sum); e != nil {
					return e
				}
				fmt.Fprintf(os.Stderr, "repaired migration checksum for %s after an explicit operator request\n", version)
				continue
			}
			continue
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, string(body)); e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO workforce_migrations(version,checksum) VALUES($1,$2) ON CONFLICT(version) DO UPDATE SET checksum=excluded.checksum WHERE workforce_migrations.checksum IS NULL`, version, sum)
		}
		if e != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", version, e)
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}
