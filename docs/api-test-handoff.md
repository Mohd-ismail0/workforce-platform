# API integration-test handoff

- Tests in `internal/api/server_test.go` use the dedicated runtime `DATABASE_URL` from `/home/prod/.config/workforce-platform/database.env`; the configured database currently rejects migrations with `permission denied for schema public (SQLSTATE 42501)`. Tests therefore cannot execute against the real database until its role/schema privileges are corrected.
- The API currently inserts `river_job(args,kind)` directly. This is intentionally not changed here because the River runtime/migration agent owns queue compatibility; approval tests will validate it once migrations settle.
- No store files were modified. If database privileges are fixed and a store-level failure appears, investigate/report it here rather than changing `internal/store`.
