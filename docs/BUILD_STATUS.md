# Implementation status — local development milestone

This is a partial working platform foundation, not completion of the full build specification.

## Implemented and exercised

- Go API and PostgreSQL persistence on the user's existing shared LXC; dedicated workforce database and restricted roles.
- Real River OSS library/migrations and transactionally enqueued proposal jobs, separate continuous worker.
- Projects, tasks, prerequisite cycle checks, agent-definition registration, three simulator connectors.
- Pure typed inventory/mail/document preparation, target versions, immutable proposal revisions, requester endorsement and distinct human approval.
- Current database identity resolution and active/role checks, tenant RLS plus forced policies, scoped transactions, coarse per-org transaction serialization.
- Simulator apply with readback and receipts; duplicate approval does not repeat the same proposal's execution.
- Handoff lifecycle: offer, accept, decline (with a reason), ask for clarification, creator withdrawal, and automatic expiry. Offers carry a bounded lifetime (`DefaultHandoffTTL`, 7 days) enforced in SQL against the database clock, so an unanswered offer cannot sit forever merely because nobody opened the page; reading an offer applies expiry first, so a stale offer is never shown as actionable. Accountability transfers already accepted for a task are counted and capped, because endless ping-pong launders responsibility; re-assigning the executor does not count, since it does not move accountability. Version-checked owner/assignee transfer on acceptance, authenticated incoming/outgoing handoff inbox, `GET /handoffs/{id}`, and API scope tests. Verified by store tests (party authorization for decline/clarify/cancel, expiry, hop limit, and a concurrent accept-vs-decline race where exactly one reply wins), API tests, and a live server probe of the full lifecycle.
- Effective-dated organization model: `positions` (structure nodes; a holderless position is a vacancy) and `org_relationships` (reporting, matrix membership, cover) with a half-open `tstzrange` validity period, both tenant-scoped with forced RLS. At most one manager at a time is enforced by a GiST EXCLUDE constraint, not an application check, so concurrent calls cannot both win; matrix membership and cover are deliberately exempt so concurrent teams are representable. Management cycles are refused at creation, and ending a relationship closes its interval rather than deleting history. `GET/POST /positions`, `GET /relationships?as_of=`, `POST /relationships`, `POST /relationships/{id}/end`. **Authority is not derived from structure** — a test makes a requester the manager of an approver and asserts both roles are unchanged. `GET /people` provides the directory a structure view needs (a reporting line between opaque ids tells nobody anything), with cross-org isolation asserted. The Team page shows reporting lines, team membership, cover and unstaffed positions with their effective periods. Not yet exposed in the UI: editing the structure from the page, and an as-of date picker even though the API supports it.
- React local-development panel, project/tasks, decision inbox, integration records/proposals, agent registry, activity and receipts.
- Person work board: `WorkBoard` (store) and `GET /work/board`, a server-side projection of outstanding work for the authenticated caller — identity comes from the principal, never a query parameter, so the client is no longer handed every task in the organisation. Each item carries its `relevance` (`accountable`, `executing`, `handoff_offered`) so the list is an account of obligations, not lifecycle states; the same task appears exactly once, and accepting an offer moves it rather than duplicating it. Blocked work names its unmet prerequisite and that prerequisite's owner; only `done` clears it (a cancelled prerequisite still blocks). Decisions are deliberately excluded — commitments and decisions are separate obligations — and My work groups by what the person owes instead of by state.
- Agent board: `AgentBoard` (store) and `GET /agents/{id}/board` — the agent definition, attempts split into `running`/`queued`/`parked`, open tasks, and blocking questions. `active` is true only when a run is `running`, because **a parked run is waiting on a person and is not an active reasoning process**; the Agent page renders "Working now", "Waiting on a person · not reasoning", "Admitted, not started" or "Idle" accordingly. An unknown or cross-org agent returns 404. **Agent identity is not a human approver**: an agent has no row in `principals`, so approving with one is refused, and declaring an `approve:work` capability does not create a principal (both asserted by test).
- Milestone planning: `milestones` (migration 015) with **required acceptance evidence**, separate `committed_date` and `forecast_date`, and `milestone_dependencies`. A milestone is not a "Done" column: completion requires recorded evidence AND met prerequisites, both checked from stored state, and the database carries a CHECK that a `met` milestone has evidence. Revising a forecast leaves the commitment untouched (asserted at store and API level). Dependency cycles and self-dependencies are refused. Endpoints: `GET/POST /projects/{id}/milestones`, `POST /milestones/{id}/{complete,forecast,dependencies}`.
- Capability ceilings: **activating a registry release grants what it requested** (the activating operator approves it), so `granted_capabilities` is real rather than permanently empty; widening beyond that needs a new release or the admin-only, version-checked `POST /registry/releases/{id}/grant`. An agent's declared capabilities are bounded by its harness's grants, enforced at **run admission** (`admissionMissing`) with a diagnostic naming exactly what is missing — the spec's "missing required capabilities fail admission", and admission is the run, not the agent definition (an agent may be described before its harness exists). `GET /agents/{id}/configuration` reports declared / permitted / denied / available headroom, and `POST /capabilities/check` answers a prospective request before anything launches. A GRANTED `approve:work` capability still does not let an agent approve.
- Tests cover real PostgreSQL HTTP workflows and River execution for three simulated integrations, cross-org denial, endorsement policy, revoked approver, unmet prerequisites, connector malformed input and local auth fail-closed checks.

- Dependency waits preserve approved proposals without occupying workers; parent completion transactionally enqueues eligible child work (real River regression test). Cancelled prerequisites remain blocked.
- Stale/invalid connector failures become needs_attention with blocked tasks instead of retry storms.
- Migrations serialize using an advisory session lock, record checksums, skip applied files and reject altered applied SQL. Legacy checksum adoption is explicit; migration-owner regression test passed.
- Cross-run business-operation identity: a stable `business_key` per integration/action reserves the official operation, so the same invoice, movement or send cannot be applied twice by a retry, a new proposal revision or a different task. Duplicate keys with different content, and reuse after dispatch, are rejected; rejected proposals release the key for a corrected revision. Effects link the business operation and it advances reserved→dispatching→verified.
- Plugin/integration registry: org-scoped release records with an operator-only lifecycle (quarantined→verified→approved→installed→active→draining/disabled, revoked terminal), optimistic revision checks and immutable content. Submit-time capability grants are ignored; verified by tests that a submitter cannot self-grant or self-verify.
- **PD-05 coherent supplier journey acceptance** (`scripts/vendor_journey.py`): a reproducible, committed journey exercising scoped intake → preparation → inventory preview → requester endorsement → authorized approval → park/resume and apply → independent readback → separate send response. 15/15 checks pass against the live API+worker and are idempotent across runs. It asserts the two invariants the vision depends on: (a) an inventory approval does NOT dispatch any email — a mail send stays `pending_endorsement` until it clears its own endorsement and a distinct approval; and (b) a request-changes rejection supersedes the review, a stale approve against the superseded revision is refused, and a fresh corrected revision releases the business key and proceeds normally. Duplicate-delivery dedupe and the stale-approval race are covered as separate checks.

## Known limitations — do not enable real business writes

- **Durable pause/resume is state-machine continuation, not memory serialization.** A parked run persists its question and the human's answer, then re-executes with those inputs. The harness process itself is not frozen and resumed, so in-memory harness state does not survive a stop.
- Claim recovery uses a two-minute lease. A worker that dies mid-run is recovered by the next attempt after the lease expires; a healthy worker is never preempted while its lease is valid.
- **Harness execution: two runners exist, and the distinction matters.** The default is an in-process, deterministic **simulator** that prepares a proposal and calls no network or model. Separately, `internal/runner` can launch any operator-configured command (`WORKFORCE_RUNNER_CLI_*`) as a real subprocess. Registering a harness release does not by itself make a real harness executable, and no harness binary or model credential is provisioned on this host by default.
- Agent runs are gated on an **active harness release** in the registry; without one a run is refused with `no_active_harness`. A quarantined release never unlocks runs, and only an operator may promote one.
- Local opaque-token development authentication only; OIDC/OpenFGA not implemented. Coarse organization visibility is not resource-level/matrix organization authorization.
- Agent-run execution seam: `agent_runs` lifecycle (queued → running → succeeded/failed) dispatched through River, where the runner may only *prepare* a proposal. A run never endorses, approves or executes; its output lands in the normal review path.
- Agent definitions are registry metadata; no real harness subprocess execution, hardened remote runner, executable-package SDK or plugin installation lifecycle yet.
- All external destinations are simulator records in the same PostgreSQL transaction. This establishes local transaction/readback behavior, NOT external provider idempotency, unknown outcomes, distributed partial failures or business correctness.
- Recovery is scheduled, not merely permitted: the worker sweeps for runs whose claim lease expired and atomically re-queues them with their job plus an `agent_run.reclaimed` audit event. Live leases are never stolen, terminal tasks are never resurrected, a batch limit never strands the remainder, and a displaced holder cannot publish twice. Verified per-organisation where it is hermetic.
- Verification boundary for recovery: the tests exercise targeted per-organisation reclamation. Wiring the sweeper to a timer and a passing general end-to-end run does **not** independently prove that *timed* crash recovery fires, and the displaced-holder test calls a fresh execution after completion rather than making an attempt that was paused mid-flight try to publish with a stale token. Those are open follow-up acceptance tests, not established behaviour.
- The sweeper must walk the organisation directory with a rotating cursor. A fixed prefix would examine the same few tenants forever: this database holds 1776 organisations, so "recovery" would have looked implemented while never firing. Cross-tenant scanning of tenant tables is impossible by design (forced RLS), which is why the candidate set comes from the `organizations` directory.
- No generalized durable gates, quorum, expiry, full audit immutability or effect broker credentials yet.
- Limited task authorization and same-org role semantics need tightening before shared-user rollout. Per-request identity lookup is not a complete atomic revocation protocol for all operations.
- Migration checksum adoption is an operator trust decision; automated cross-version rollback/restore certification remains outstanding.
- A `family`/`version` pair may only be released once per organization; retrying a failed release requires a new version. This is intentional but will need an explicit supersede path before real plugin updates.
- Real-model qualification (`scripts/qualify-real-harness.sh`): a pinned Claude Code 2.1.270 subprocess drives a real model on the operator's gateway over HTTPS. It returned `waiting` for a scope that genuinely lacked the fact, the gate persisted and routed to the accountable owner, an independent run was processed while it held no worker slot, the supplied answer was accepted, and a FRESH invocation resumed to a proposal awaiting endorsement; endorsement, self-approval refusal, a different approver, execution, a proposal-linked receipt and an exact one-version target advance all followed. 30/30.
- Boundaries of that result: the effects are still simulator records, the answer was supplied by a script standing in for a person (labelled as such), and there is no kernel containment on this host. It qualifies the platform's own loop, not production readiness.
- Gate questions support scalar arrays (`array` with scalar `items`), and a question whose schema cannot be recorded as an answer is REFUSED at creation with `invalid_gate_schema` — previously a model asking for `recipients` as an array parked the run on a question no answer could satisfy.
- No Gontext live adapter, object-store pipeline, org-wide assistant, Board Steward, notification delivery, correction/evaluation pipeline or production restore certification yet. Project milestones exist (stated outcomes with required acceptance evidence, separate committed/forecast dates and prerequisites); what is still absent is the fuller planning surface — timeline/dependency views, drag-as-planning-intent, and rollups.
- Simulator schemas/test data only. Live providers, external network and real write credentials remain disabled.
- **Real harness adapter: model-driven path VERIFIED for preparation, pause and resume.** `internal/runner` can launch any operator-configured command (`WORKFORCE_RUNNER_CLI_*`) as a separate process, hand it the run context on a documented JSON protocol, and turn its reply into a proposal. Verified through real subprocesses: process-group kill on timeout/cancel, output caps, environment isolation, missing-binary fail-closed, and rejection of run-scoped business keys. The full loop is proven end-to-end through a **real process boundary**: the harness process parked with no worker held, its question reached the respondent, the answer resumed it, and its output landed as a proposal awaiting endorsement (55/55 live checks in `scripts/e2e_harness_check.sh`). The harness used for that proof is a test double, **so no model-driven agent has run.** No harness binary (claude/codex/opencode) is installed and no model credential is configured on this host. Activation requires an operator to install a harness and supply a credential through `WORKFORCE_RUNNER_CLI_<ID>_ENV`.
- The adapter unwraps the output shapes real harnesses emit (Claude Code's single JSON envelope, Codex's JSONL event stream) up to a bounded depth, and treats a harness that exits 0 while reporting its own failure as failed (`harness_reported_error`) rather than mining it for a draft. No real harness binary or model credential exists on this host, so this is verified against documented shapes reproduced by a test double, not against the vendors' executables.
- Harness output is untrusted input: it is validated again by `CreateProposal`, business-key reservation, endorsement and distinct approval. A harness cannot approve, execute or hold business credentials, and an approval never releases credentials to it.
- The published protocol never sends `null` for an empty collection (empty inputs/records are `[]`). A nil Go slice marshals as `null`, and a harness doing `doc.get("inputs", [])` gets `None` rather than `[]` when the key exists with a null value — which crashed the first real subprocess run.
- Harnesses resolve their executable through a **configured PATH**, never the ambient one. `exec.LookPath` against a developer shell's PATH picked an unrelated virtualenv interpreter that could not start under the restricted child environment.
- Business keys accept the identifiers real work produces, including `@` and a leading `+` (supplier/customer email addresses, tagged addresses, phone numbers). Rejecting them forced harnesses to mangle keys, and a mangled key is a weaker duplicate guard than the real identifier.
- A successful harness run means *preparation completed*, not business completion. The run is labelled as preparing a proposal; the effect happens only after human approval through the existing executor.
- Recovery is eligibility, not scheduling: an expired claim becomes reclaimable, but no scheduler is guaranteed to retry it. The lease is now derived from the runner's own timeout budget (+60s, 2-minute floor) so a slow-but-healthy harness cannot outlive its lease.
- Locally, test suites share one database and one River queue; isolation is achieved by running packages serially (`-p 1`). Within `internal/api`, several tests start real workers on that shared queue, so a run may legitimately be claimed mid-test. Guarantees about *not being claimed* are therefore asserted in `internal/store`, which drives the store directly with no worker present; `internal/api` asserts only what holds regardless of scheduling (no publication bypasses human review).
- Per-package isolated databases ARE implemented (`internal/testdb`) and used in CI, where packages run in PARALLEL against their own freshly-migrated database. Locally the shared-LXC roles cannot CREATE DATABASE, so the helper degrades to the shared database and `-p 1` remains the correctness guarantee in that mode.

## Continuous integration

`.github/workflows/ci.yml` runs on every push to `main` and every pull request,
in three jobs: the Go race suite against a real PostgreSQL service container
(with migrations applied first), the frontend typecheck/build/tests, and spec
validation. None touch a live provider or public endpoint. **CI is green**
(commit `a1b3229`).

The backend job provisions a dedicated NON-SUPERUSER role (`workforce_app`,
NOSUPERUSER NOBYPASSRLS, CREATEDB) that owns the database; the superuser is used
only to provision. This is deliberate: a superuser or any `BYPASSRLS` role
silently bypasses row-level security (`FORCE ROW LEVEL SECURITY` binds the table
owner, not a superuser), so a superuser runtime role would make every
tenant-isolation policy inert. Per-package test databases (see
`internal/testdb`) are created and owned by the same ordinary role, so packages
run in parallel with RLS genuinely enforced.

Building CI caught three production-blocking defects that local testing
structurally could not, because the local shared-LXC roles cannot CREATE
DATABASE and its database is already migrated:

1. `internal/testdb` migrated per-package databases with a RELATIVE `migrations`
   path that `go test` resolves from the package directory, leaving the fresh
   database unmigrated and every test failing on missing relations.
2. **`002_river` aborted a fresh database**: PL/pgSQL does not short-circuit
   `a AND b` in an `IF`, so `EXISTS (SELECT 1 FROM river_job)` ran while
   `river_job` did not yet exist (River creates its tables after the numbered
   migrations), raising `42P01`. Fixed by nesting the checks. Already-migrated
   databases are unaffected in behaviour; correcting an applied migration
   requires the explicit, reported `WORKFORCE_REPAIR_MIGRATION_CHECKSUMS=1`.
3. The runtime role being a superuser left RLS inert, which the cross-org
   isolation test exposed as a real breach (`org-fixture-b` could read
   `org-fixture-a`'s task).

## Evidence

Parent-executed `python3 scripts/dev.py test` runs Go tests with race detection and real DATABASE_URL (fails rather than silently skips if missing). `go vet ./...` passes. Web tests/build/typecheck are recorded at integration completion. No quantitative productivity or production safety claim.

## Identity and authority (current boundary)

Verified by execution, not assertion:

- `go build ./...`, `go vet ./...` exit 0 (status captured directly, never through a pipe).
- Full DB-backed suite `python3 scripts/dev.py test` (race, serialized, real `DATABASE_URL`) exit 0.
- 9 new identity-linking tests **pass, not skip** — including one external account cannot
  map to two principals, the mapping is globally unique rather than per-org, a deactivated
  principal stops resolving immediately, unlink deactivates while keeping the audit row,
  and empty inputs are refused.
- OIDC verifier tests pass against a fixture that serves a real discovery document, so the
  code consumes `jwks_uri` instead of a guessed JWKS path.
- Web: 17 tests pass, typecheck exit 0, build exit 0. Migration 011 applies and is repeatable.
- `scripts/logto_provision.py` dry run exits 1 with the bootstrap steps and **no** secret
  echo and no traceback.

**Not established, and not to be implied:**

- **Identity onboarding is implemented and fixture-tested, but no human has completed a login.**
  - A local operator command (`-bootstrap-identity`) creates and links the FIRST administrator.
    It exists because the first administrator has no linked account and therefore nothing to
    authenticate with; it is a CLI command rather than an HTTP route so it cannot be an
    anonymous escalation path. It requires the exact `(issuer, subject)` of an
    already-authenticated identity, never links by email, and records the human operator as the
    actor instead of inventing an authenticated administrator.
  - Subsequent onboarding goes through authenticated administrator routes
    (`GET/POST /identity/links`, `POST /identity/links/unlink`), whose authority is read from
    the principal row on every request. A non-administrator is refused.
  - A session is invalidated when the identity link that authorised it is removed, even though
    the principal itself stays active — otherwise "remove access" would not remove access until
    the cookie expired. Unlinking also ends the identity's sessions immediately.
  - Unlinking the last external account of an active administrator is refused, because that
    would remove the organization's only authenticated path into onboarding.
  - No auto-provisioning on first sign-in and no email-based linking: email is reassignable,
    so linking on it would be an account-takeover primitive.
- Logto provisioning **has been applied and read back successfully**: resource
  `e810t1oshvoeefp00d7n5` with indicator `https://workforce.internal/api` and scopes
  `read:work`, `write:work`, `approve:work`; confidential BFF app
  `v33skrq1quvxkuvrqul8a` with the registered callback and logout URI.
- The BFF client secret was created additively as `workforce-bff-runtime` and stored in a
  mode-600 operator-protected file. Its value was never printed.
- The BFF browser foundation is implemented and tested: authorization-code + PKCE,
  single-use state/nonce transactions, hashed server-side sessions, revocation,
  offboarding checks, HttpOnly/Secure/SameSite cookies and CSRF protection. Migration 012
  applies repeatably.
- **A real human browser login HAS been exercised and passes.** Driven over CDP against the
  headed browser (`scripts/signin_acceptance.py`, `scripts/session_acceptance.py`): the
  round trip returns to the panel, `__Host-workforce_session` is stored with `secure=true`
  and `httpOnly=true`, `GET /auth/session` reports
  `{authenticated:true, identity:{id:mdil, org_id:xsama, role:admin}}`, and `GET /api/v1/me`
  succeeds using that cookie alone. A reload preserves the session.
- **The cookie transport is load-bearing and was the cause of a real sign-in failure.**
  `__Host-workforce_session` requires the `Secure` attribute; with
  `WORKFORCE_COOKIE_SECURE=false` the browser discarded it silently, so the callback
  succeeded, a session row was written, and the page still showed the sign-in screen. Chrome
  treats `http://127.0.0.1` as a trustworthy origin, so a Secure cookie is accepted there and
  `WORKFORCE_COOKIE_SECURE=true` is correct for the local topology — not a workaround.
- CSRF enforcement and logout were verified in the browser, not only in unit tests: a
  cookie-authenticated `POST` without `X-CSRF-Token` is refused `403 csrf_failed` while the
  same request with the header is routed (a non-existent path returns `404`), and after
  `POST /auth/logout` the session reports signed out and the API returns `401`. The logout leg
  of the acceptance script is opt-in (`--with-logout`) because it ends a real session.
- The UI now asks the BFF for session state first and shows "Sign in with SSO" when the browser
  flow is configured; the token form appears only when `/auth/session` refuses because the
  deployment is in local mode. The credential is an HttpOnly cookie the page cannot read; no
  access token, refresh token or client secret is ever placed in browser storage.
- Authentication is not authorization. Org, role, ownership, approval jurisdiction and
  separation of duties come from kernel rows, never from a token claim. A token proves
  *who*, never *what*.
- **The two-human approval experience is not proven by a single login.** Another real user must
  be onboarded before the requester/approver separation can be demonstrated with real people
  rather than fixtures.

Two earlier claims are corrected here. Sending mail is **not reversible** — it is narrow
and allowlistable, which is weaker. A dedicated unprivileged UID is **not** sufficient
sandboxing; it reduces exposure without containing a process. Both remain open workstreams;
clearing OIDC does not clear "all blockers".

See docs/build-spec for the complete destination. No PostgreSQL server was installed locally, no existing application databases modified, no public route created.
