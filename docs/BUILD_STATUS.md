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
- Cross-run business-operation identity: a stable `business_key` per integration/action reserves the official operation, so the same invoice, movement or send cannot be applied twice by a retry, a new proposal revision or a different task. Duplicate keys with different content, and reuse after dispatch, are rejected; rejected proposals release the key for a corrected revision. Effects link the business operation and it advances reserved→dispatching→verified.
- Plugin/integration registry: org-scoped release records with an operator-only lifecycle (quarantined→verified→approved→installed→active→draining/disabled, revoked terminal), optimistic revision checks and immutable content. Submit-time capability grants are ignored; verified by tests that a submitter cannot self-grant or self-verify.

## Known limitations — do not enable real business writes

- **Durable pause/resume is state-machine continuation, not memory serialization.** A parked run persists its question and the human's answer, then re-executes with those inputs. The harness process itself is not frozen and resumed, so in-memory harness state does not survive a stop.
- Claim recovery uses a two-minute lease. A worker that dies mid-run is recovered by the next attempt after the lease expires; a healthy worker is never preempted while its lease is valid.
- **Harness execution is a simulator.** The only runner is an in-process, deterministic simulator that prepares a proposal. It does not launch a real coding harness, does not run external commands and does not call a network or model provider. Registering a harness release does not make that harness executable.
- Agent runs are gated on an **active harness release** in the registry; without one a run is refused with `no_active_harness`. A quarantined release never unlocks runs, and only an operator may promote one.
- Local opaque-token development authentication only; OIDC/OpenFGA not implemented. Coarse organization visibility is not resource-level/matrix organization authorization.
- Agent-run execution seam: `agent_runs` lifecycle (queued → running → succeeded/failed) dispatched through River, where the runner may only *prepare* a proposal. A run never endorses, approves or executes; its output lands in the normal review path.
- Agent definitions are registry metadata; no real harness subprocess execution, hardened remote runner, executable-package SDK or plugin installation lifecycle yet.
- All external destinations are simulator records in the same PostgreSQL transaction. This establishes local transaction/readback behavior, NOT external provider idempotency, unknown outcomes, distributed partial failures or business correctness.
- No generalized durable gates, quorum, expiry, full audit immutability or effect broker credentials yet.
- Limited task authorization and same-org role semantics need tightening before shared-user rollout. Per-request identity lookup is not a complete atomic revocation protocol for all operations.
- Migration checksum adoption is an operator trust decision; automated cross-version rollback/restore certification remains outstanding.
- A `family`/`version` pair may only be released once per organization; retrying a failed release requires a new version. This is intentional but will need an explicit supersede path before real plugin updates.
- No Gontext live adapter, object-store pipeline, org-wide assistant, Board Steward, full PM milestones, notification delivery, correction/evaluation pipeline or production restore certification yet.
- Simulator schemas/test data only. Live providers, external network and real write credentials remain disabled.
- **Real harness adapter: mechanism VERIFIED through a real subprocess; model-driven path still UNVERIFIED.** `internal/runner` can launch any operator-configured command (`WORKFORCE_RUNNER_CLI_*`) as a separate process, hand it the run context on a documented JSON protocol, and turn its reply into a proposal. Verified through real subprocesses: process-group kill on timeout/cancel, output caps, environment isolation, missing-binary fail-closed, and rejection of run-scoped business keys. The full loop is proven end-to-end through a **real process boundary**: the harness process parked with no worker held, its question reached the respondent, the answer resumed it, and its output landed as a proposal awaiting endorsement (55/55 live checks in `scripts/e2e_harness_check.sh`). The harness used for that proof is a test double, **so no model-driven agent has run.** No harness binary (claude/codex/opencode) is installed and no model credential is configured on this host. Activation requires an operator to install a harness and supply a credential through `WORKFORCE_RUNNER_CLI_<ID>_ENV`.
- Harness output is untrusted input: it is validated again by `CreateProposal`, business-key reservation, endorsement and distinct approval. A harness cannot approve, execute or hold business credentials, and an approval never releases credentials to it.
- The published protocol never sends `null` for an empty collection (empty inputs/records are `[]`). A nil Go slice marshals as `null`, and a harness doing `doc.get("inputs", [])` gets `None` rather than `[]` when the key exists with a null value — which crashed the first real subprocess run.
- Harnesses resolve their executable through a **configured PATH**, never the ambient one. `exec.LookPath` against a developer shell's PATH picked an unrelated virtualenv interpreter that could not start under the restricted child environment.
- Business keys accept the identifiers real work produces, including `@` and a leading `+` (supplier/customer email addresses, tagged addresses, phone numbers). Rejecting them forced harnesses to mangle keys, and a mangled key is a weaker duplicate guard than the real identifier.
- A successful harness run means *preparation completed*, not business completion. The run is labelled as preparing a proposal; the effect happens only after human approval through the existing executor.
- Recovery is eligibility, not scheduling: an expired claim becomes reclaimable, but no scheduler is guaranteed to retry it. The lease is now derived from the runner's own timeout budget (+60s, 2-minute floor) so a slow-but-healthy harness cannot outlive its lease.
- Test suites share one database and one River queue; isolation is achieved by running packages serially (`-p 1`). Within `internal/api`, several tests start real workers on that shared queue, so a run may legitimately be claimed mid-test. Guarantees about *not being claimed* are therefore asserted in `internal/store`, which drives the store directly with no worker present; `internal/api` asserts only what holds regardless of scheduling (no publication bypasses human review).
- Per-suite isolated databases/queues are the proper follow-up and are not yet implemented.

## Evidence

Parent-executed `python3 scripts/dev.py test` runs Go tests with race detection and real DATABASE_URL (fails rather than silently skips if missing). `go vet ./...` passes. Web tests/build/typecheck are recorded at integration completion. No quantitative productivity or production safety claim.

See docs/build-spec for the complete destination. No PostgreSQL server was installed locally, no existing application databases modified, no public route created.
