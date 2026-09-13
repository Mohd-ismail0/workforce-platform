# Backend handoff

- Implemented the contract API surface under `/api/v1`: projects, tasks, dependencies, cancellation, proposals, proposal reads/lists, endorse/approve/reject, decisions, events, receipts, agents, integration manifests and simulator records, handoff offer/accept, `/me`, `/health`, and database-backed `/ready`.
- Added bounded JSON decoding, snake_case response tags, timestamp serialization, structured errors with SQL/provider detail suppression, tenant transaction context, RLS migration policy, task dependency cycle validation, task version cancellation, locked proposal revision allocation, superseding-safe status transitions, separate approver enforcement, decision idempotence, cancellation gating, effect creation, simulator execution, receipts, task/proposal completion, and a River-compatible transactional job table/worker pass.
- `-seed` now creates exactly two credential-free synthetic organizations (`org-fixture-a`, `org-fixture-b`), requester/approver fixture principals, and inventory/mail/documents simulator records. IDs are deterministic and documented here; production credentials remain external.
- Migration execution is repeatable and records `001_initial` in `workforce_migrations`.

Verification performed:

- `go test ./...` passes with the repository Go toolchain.
- Real `-migrate` against the dedicated PostgreSQL database passes repeatedly.
- Real `-seed` against the dedicated PostgreSQL database passes.

Remaining limitation: no committed PostgreSQL integration test suite was present in the starting repository, so the full HTTP end-to-end workflow and cross-tenant/RLS assertions still need to be added by the parent integration stream. The implementation is compile-tested and exercised through migration/seed against real PostgreSQL; no external provider writes are enabled.
