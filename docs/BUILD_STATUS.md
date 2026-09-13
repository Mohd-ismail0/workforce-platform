# Implementation status — local development milestone

This is a partial working platform foundation, not completion of the full build specification.

## Implemented and exercised

- Go API and PostgreSQL persistence on the user's existing shared LXC; dedicated workforce database and restricted roles.
- Real River OSS library/migrations and transactionally enqueued proposal jobs, separate continuous worker.
- Projects, tasks, prerequisite cycle checks, agent-definition registration, three simulator connectors.
- Pure typed inventory/mail/document preparation, target versions, immutable proposal revisions, requester endorsement and distinct human approval.
- Current database identity resolution and active/role checks, tenant RLS plus forced policies, scoped transactions, coarse per-org transaction serialization.
- Simulator apply with readback and receipts; duplicate approval does not repeat the same proposal's execution.
- Handoff offer and version-checked owner/assignee acceptance, with authenticated incoming/outgoing handoff inbox and API scope tests.
- React local-development panel, project/tasks, decision inbox, integration records/proposals, agent registry, activity and receipts.
- Tests cover real PostgreSQL HTTP workflows and River execution for three simulated integrations, cross-org denial, endorsement policy, revoked approver, unmet prerequisites, connector malformed input and local auth fail-closed checks.

- Dependency waits preserve approved proposals without occupying workers; parent completion transactionally enqueues eligible child work (real River regression test). Cancelled prerequisites remain blocked.
- Stale/invalid connector failures become needs_attention with blocked tasks instead of retry storms.
- Migrations serialize using an advisory session lock, record checksums, skip applied files and reject altered applied SQL. Legacy checksum adoption is explicit; migration-owner regression test passed.

## Known limitations — do not enable real business writes

- Local opaque-token development authentication only; OIDC/OpenFGA not implemented. Coarse organization visibility is not resource-level/matrix organization authorization.
- Agent definitions are registry metadata; no harness subprocess execution, hardened remote runner, executable-package SDK or plugin installation lifecycle yet.
- All external destinations are simulator records in the same PostgreSQL transaction. This establishes local transaction/readback behavior, NOT external provider idempotency, unknown outcomes, distributed partial failures or business correctness.
- No stable cross-proposal business-operation dedupe, generalized durable gates, quorum, expiry, full audit immutability or effect broker credentials yet.
- Limited task authorization and same-org role semantics need tightening before shared-user rollout. Per-request identity lookup is not a complete atomic revocation protocol for all operations.
- Migration checksum adoption is an operator trust decision; automated cross-version rollback/restore certification remains outstanding.
- No Gontext live adapter, object-store pipeline, org-wide assistant, Board Steward, full PM milestones, notification delivery, correction/evaluation pipeline or production restore certification yet.
- Simulator schemas/test data only. Live providers, external network and real write credentials remain disabled.

## Evidence

Parent-executed `python3 scripts/dev.py test` runs Go tests with race detection and real DATABASE_URL (fails rather than silently skips if missing). `go vet ./...` passes. Web tests/build/typecheck are recorded at integration completion. No quantitative productivity or production safety claim.

See docs/build-spec for the complete destination. No PostgreSQL server was installed locally, no existing application databases modified, no public route created.
