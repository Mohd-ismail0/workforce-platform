# Build baseline and architecture decisions

Status: proposed implementation baseline for a new, separate OSS workforce product. No software has been built or deployed by this specification effort. Proposed contract identifiers and repository paths below are new designs, not claims about existing software.

## Destination

An employee delegates work to a chosen agent on organizational compute. Authorized reads and isolated preparation produce a tested automation and immutable reviewable changes. The employee endorses the proposal; a distinct eligible human authorizes official writes. A restricted executor applies the exact approved effects and independently obtains verification evidence. The central workspace hosts role-specific boards, project coordination, source-aware previews, decisions and receipts. Gontext supplies authorized organizational knowledge. Shared org agents and governed learning reduce repeated work without gaining independent authority.

Vlobel and Dhanda are independent products, not sources of task truth or mandatory code dependencies. Gontext remains independently deployed. The new product's name and repository are not chosen here. Specification files stay local until publication is authorized.

## Baseline decisions

| Concern | Implementation baseline | Qualification gate |
|---|---|---|
| Control plane | Go modular monolith with internal modules and API/background roles | Compile, transaction integration and auth tests before expanding |
| Web | React/TypeScript; typed generated API client; host-rendered domain previews | Accessible and revision-safe decision flow |
| Storage | PostgreSQL with composite tenant keys; explicit current-state authority plus transactional audit/outbox | Forced-RLS runtime role, cross-tenant and pooling tests |
| Queue | River OSS for dispatch/maintenance; product owns business tasks/gates/leases/admission | Lost launch response, duplicate job, parked wait and reservation tests |
| Execution | Separate organization runner pool; task-isolated workloads; privileged launch controller outside workloads | Runtime-specific read/write/network/process containment |
| Identity | Logto-backed verified human sessions and bounded workload identity; stable internal subject mapping | Real issuer/audience/tenant/service/user/delegation tests |
| Authorization | Kernel-enforced actions and current deny state; OpenFGA relationship checks | Explicit consistency, deny precedence and outage policy |
| Artifacts | Private S3-compatible storage; tenant-scoped references, versions, retention and checksums | No public previews or cross-tenant signed URL leakage |
| Context | Gontext adapter over supported retrieval/intake contracts | Actual grant/revocation/source behavior qualified; gaps stay blockers |
| Models | LiteLLM where harness supports it; explicit direct-provider exceptions | Metering, provider terms, residency and capability certification |
| Tracing | OTel operational telemetry; Langfuse for permitted model traces | Redaction before persistence/export; not an authority ledger |
| Broker | Not required initially; outbox can add JetStream fan-out later | Correctness survives broker absence |

Pinned dependency/runtime versions are selected during initial qualification, then committed in lockfiles/images and compatibility records. This is not a claim of currently verified package versions. River Pro is not a baseline dependency. Do not maintain two production queues; replace the candidate only if recorded test evidence requires it.

## Logical modules

| Module | Owns | Interface it exposes |
|---|---|---|
| Identity/authority | Principals, sessions, tenant mapping, mandates, grants/revocation | Authenticate, Authorize, ExplainDecision |
| Organization | Typed temporal relationships, accepted accountabilities and coverage | ResolveRoles, Offer/AcceptRole, ChangeRelationship |
| Work | Projects, milestones, task/graph revisions and valid transitions | Typed task/project commands and authorized queries |
| Review | Proposal freeze/supersession, gates, DecisionRequests, endorsement and human decisions | PrepareReview, RequestDecision, ResolveDecision |
| Handoff | Version-bound role transfer, sufficient evidence, acceptance and arbitration | Offer, Inspect, Accept/Decline, Apply |
| Execution | Admission, launch intents, domain attempts, leases, fencing, cleanup | Admit, Dispatch, Heartbeat, Park, Reconcile |
| Effects | Exact prepared operations, business uniqueness, restricted dispatch and verification | PrepareEffect, AuthorizeDispatch, Execute, Inspect/Reconcile |
| Integrations | Typed read/prepare/render/apply/verify contracts and capability certification | Operation catalog and checked connector execution |
| Registry | Agent/automation/plugin versions, signed artifact inventory and policy ceilings | Register, Certify, Install, Activate, Revoke |
| Knowledge | Gontext-authorized retrieval and bounded observation publication | FetchContext, PublishObservation, CatchUp |
| Improvement | Structured corrections, candidates, evals, shadow and promotion evidence | RecordCorrection, EvaluateCandidate, Promote |
| Presentation | Role-filtered boards, domain review projections and revision drafts | Authorized query models; commands still hit owning module |
| Operations | Outbox/inbox delivery, audit retention, incidents, quotas and repair | Health, diagnostics, deterministic reconcile and controlled recovery |

A logical module does not require its own network service. Trusted kernel modules share transactional infrastructure through controlled interfaces. Untrusted harnesses/connectors/renderers do not receive kernel database/signing keys. Platform operators and host administrators are explicitly trusted; threat model does not claim defense against malicious root.

## Proposed repository layout

```text
cmd/workforce/              API/background/maintenance role entrypoints
cmd/runner/                 constrained runner controller
internal/identity/
internal/organization/
internal/work/
internal/review/
internal/handoff/
internal/execution/
internal/effects/
internal/registry/
internal/knowledge/
internal/improvement/
internal/platform/          transaction plumbing, errors, clock, IDs, audit/outbox
web/                        React app, host preview catalog, generated client
contracts/                  OpenAPI, JSON Schemas, event vocab, error codes
sdk/                        supported automation/connector/driver contracts
integrations/               reviewed adapters, fixtures, version manifests
migrations/                 ordered forward migrations and verification
fixtures/                   synthetic two-company/matrix/authority cases
integration-tests/          PG/auth/runner/connector tests
conformance/                malicious/faulty plugin and provider simulators
infra/                      local setup and external-backend deployment examples
docs/                       vocabulary, architecture, operator/run/recovery guides
```

Public APIs are versioned language-neutral contracts, not exported internal Go types. Generated Python/TypeScript authoring SDKs may be added when an actual package/harness needs them; backend language does not restrict executable package language. Exact command names and scripts must be authored and exercised before documentation claims they run.

## Core distinctions that govern every chapter

- Employee or agent ID identifies a subject; authentication proves it. Clients cannot select privileged actors by supplying IDs.
- Source read, sensitive export and official mutation are semantic operations, not HTTP verbs.
- Requester endorsement is not effect approval. Technical tests are not human authorization. Service credentials do not inherit user scope automatically.
- Automation Package is reusable software. Review Package is one immutable effect proposal. DecisionRequest is an actionable reference to a canonical gate/handoff, not a new authority store. Execution Receipt is actual evidence.
- Queue delivery completion, run terminal success, external acceptance and task done are separate. A task is `done` only when required effects and verification are satisfied; do not add a second `verified_done` task status.
- A gate may be durably pending while a stopping run is still being cleaned up. Mark the run fully parked and release occupied capacity only after authoritative cleanup evidence or explicitly safe accounting recovery. Unknown cleanup is visible, not 'stopped'.
- No unbounded human wait holds task-dedicated execution. Shared controllers and pending database rows are normal.
- One source of truth per entity. Operational state/decisions in workforce DB; files/native snapshots in governed storage; organizational knowledge in Gontext; original records remain in official systems.
- Persistent org service identity is compatible with bounded per-caller/per-task execution. Service mandates and human-delegated authority are separate modes.
- Every run may generate evidence; code, verifier, policy and authority do not self-promote.

## Decision/invariant precedence

02-domain-security.md owns domain state vocabulary, table constraints and authority transitions. 03-execution-integrations.md owns runtime protocols and connector behavior, using domain terms. 04-product-delivery.md owns UI and milestone acceptance, with no independent approval semantics. 05-operations-assurance.md owns assurance scenarios, operational targets and recovery. README is the index, not a duplicate specification.

If chapters conflict, record the discrepancy and reconcile before publishing a build contract; precedence is not permission to ignore tests or invent a convenient hybrid. Older research files are evidence/history, not instructions to restore superseded designs.

## Frozen versus still qualified

Frozen as proposed product direction: human authority, scoped preparation, exact effect approval, immutable proposals, accepted handoffs, isolated runners, canonical board views, separate Gontext, no production mutation from preview or generated code credentials.

Qualified before first writes: exact runtime/image versions; River/job/lease mapping; first external inventory/email provider support; Gontext deployed behavior; sandbox isolation and platform fit; model/harness automation licensing; human permission matrix; authority freshness; recovery targets; retention/data residency requirements.

Scope limits are rollout constraints, not deletion of multi-company, hierarchy/matrix, global assistants, BYO harnesses or continuous improvement from the target model.
