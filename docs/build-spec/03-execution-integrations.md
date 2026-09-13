# 03 — Execution runtime and integrations

**Status:** build specification, not implementation, deployment, benchmark or certification. Normative requirements below describe what must be built and demonstrated. Existing capabilities are attributed to the supplied source reviews; no provider or harness has been exercised for this chapter.

**Scope:** the separate organizational workforce product. This chapter owns executable-package runtime, dispatch/admission, runner lifecycle, governed effects, integration contracts, context boundaries and controlled improvement. Human/agent task accountability remains kernel domain state; neither a queue job nor a harness session replaces it.

## 1. Baseline and trust boundaries

Use **Go + PostgreSQL + River OSS** for the control plane and dispatch. API and background roles share a modular-monolith codebase and kernel command implementations. They may run as separate processes; that is not a microservice rewrite. Use a **React** frontend for boards, the decision inbox and execution history. Rich integration views remain subordinate to host-owned review controls.

| Component | Owns | Must not own or receive |
|---|---|---|
| Kernel API modules | Authenticated commands/queries, tasks, gates, decisions, immutable proposals, domain transitions, event/audit transactions | Agent-authored code execution; browser-supplied replacement effect payloads |
| Background role | Fair admission, River dispatch workers, lease recovery, reconciliation, outbox/inbox delivery, verifier orchestration | Sleeping queue handlers awaiting humans; discretion to grant authority |
| PostgreSQL | Canonical domain state, launch intents, attempts, reservations, effect ledger, current permissions/revocation epochs, durable audit/outbox/inbox | Native session blobs as task truth |
| Constrained runner | Organization-owned sandbox lifecycle, isolated package/harness execution, independently observable process inventory | Kernel DB/signing keys, host sockets inside sandboxes, production write credentials |
| Read/preparation gateway | Typed reads and approved paid processing; scope, export and provider checks | Arbitrary URL/SQL forwarding; official mutations disguised as reads |
| Separate effect executor | Stored approved operations through certified connector releases; actual upstream principal and receipts | Arbitrary generated programs holding write tokens; permission to approve its own request |
| Credential broker | Account-bound credentials, short-lived capabilities and lane-specific upstream sessions | Secrets returned to packages, renderers, prompts or transcripts |
| Governed artifact store | Immutable inputs/builds, evidence, checkpoints, restricted native snapshots | A substitute for gate/attempt transactions |
| Gontext | Authorized knowledge retrieval, cited observations, context provenance and changes | Scheduler state, effect truth, artifact scratch space or approval authority |

The read gateway and effect executor are logical capability boundaries even if some reviewed connector libraries are shared. The effect executor is a separate restricted process from untrusted execution. Each trust tier gets least-privilege service credentials. Host administrators, credential-broker operators and database superusers are explicit trusted parties; this design does not claim protection against malicious root.

River supplies durable at-least-once job transport/retries. Product code supplies human gates, tenant fairness, run identities, admission, fencing and external-effect recovery. **River Pro is not required or assumed.** Transactional insertion must use the selected River OSS PostgreSQL driver in the same transaction as domain state. Pin and prove that interface during implementation; the pseudocode here is conceptual, not a claim about exact library signatures. Do not add a second production queue. Reconsider a workflow engine only if measured branching/compensation needs exceed the staged-checkpoint model.

### 1.1 Non-negotiable invariants

1. Every action binds authenticated organization, actor/workload, purpose, resource boundary and current authority. User-provided IDs are assertions, not authentication.
2. Programs may author complete workflows, but only supported checkpoint/connector IO receives durability and authority guarantees.
3. A human wait retains durable state, not a task-dedicated sandbox, model stream or execution slot indefinitely. Unobserved cleanup remains a visible exception and retains capacity charges.
4. Queue retries reuse a launch intent and attempt; only the domain controller creates another domain attempt.
5. An intent is persisted before an external effect can be submitted. Ambiguous submission is **unknown**, never automatically safe to retry.
6. Approval binds the exact stored immutable Review Package revision and selected operation set. It never hands an agent general write power.
7. Current revocation/fence checks block new dispatches; they cannot undo a request already accepted by an external provider.
8. A task becomes `done` only with independent evidence of execution integrity and domain acceptance; there is no separate `verified_done` task state. Exit code zero, successful queue completion, a provider HTTP success and agent self-tests are insufficient individually. Domain state vocabulary is owned by `02-domain-security.md`; runtime/receipt labels below are not additional task statuses.

## 2. Durable records and identities

Use organization-scoped foreign keys and uniqueness throughout. IDs below are opaque identifiers; hashes bind bytes, not authority or business meaning.

**Domain-schema mapping:** `02-domain-security.md` uses `run` for one admitted domain attempt, with `attempt_id` and `attempt_number`; it does not define an additional durable parent-run aggregate. In this chapter, “workflow execution” is the immutable package/input/checkpoint lineage linked through the task and historical attempts. The workflow execution binding row below describes that logical binding, not a request for another authoritative table; `DomainAttempt` maps to the domain `run` row. Wire `run_id` identifies that row. A continuation creates a fresh row and explicitly references validated predecessor checkpoints; within-attempt step uniqueness is `(org_id, run_id, step_key, checkpoint_revision)`, while business dedupe persists across attempts. Do not add a competing run state machine.

| Record | Required identity and immutable bindings | Meaning |
|---|---|---|
| AutomationPackageRelease | Package ID, release ID, source/build/dependency/config digests, SDK/checkpoint schema versions, entrypoints, maintainer, certification evidence | Reusable executable behavior, not approval of a particular business effect |
| InputSnapshot | Snapshot ID, capture manifest, source/account/object IDs, observed revisions/timestamps, artifact digests, classification/provenance | Material used by a particular run; an observation, not a continuing access grant |
| Workflow execution binding (not a second aggregate) | Task/version, package release, immutable input/config/acceptance manifest, predecessor checkpoint references | Immutable lineage reused across historical domain runs; package/input changes require an explicitly linked successor or authorized migration |
| StepInstance | Run ID + stable step key + item/branch identity, input digest, dependencies, checkpoint version, result/status | Durable logical step independent of individual worker deliveries |
| DomainAttempt | Attempt ID, run/phase, ordinal, fence epoch, execution provider/adapter/build, budget/capacity reservations | One admitted execution incarnation; may cover several bounded local steps |
| StepAttempt | Step instance + domain attempt + retry ordinal | Diagnostic/charged invocation; cannot reset business dedupe |
| LaunchIntent | Launch intent ID, attempt ID, immutable launch-spec digest, reservation, expiry, runner placement | Exactly one logical start request, inspected/retried under the same identity |
| Checkpoint | Checkpoint ID/schema, step outputs, next continuation, artifact manifests, pending intent references | Host-committed continuation boundary, not arbitrary process memory |
| ReviewPackage | Proposal ID, revision, digest, immutable operation set, source/target dependencies, renderer/connector pins | The concrete business changes being reviewed |
| DecisionRequest / Gate | Canonical gate ID, review revision/digest, decision kind, current eligibility/quorum, expiry | Requester endorsement, technical review, human effect authorization and clarification remain distinct |
| BusinessOperation | Organization/account/environment + domain operation kind + stable business identity | Cross-delivery/run/release duplicate-prevention identity |
| EffectIntent | Intent ID, business operation ID, exact revision/payload digest, provider key, dependencies, current state | One authorized proposed external effect and its durable recovery anchor |
| ExecutionReceipt | Effect intent, actual provider/account/principal, request/response references, exact target IDs, observations | Evidence of accepted/applied/verified/settled state, not an agent assertion |

### 2.1 Business identity versus revision

A business-operation key must survive duplicate emails, reruns, new package versions, different agents and regenerated proposals. Define it in the reviewed domain connector, not by hashing the entire new proposal. For supplier inventory reconciliation, use the organization, official account, supplier/legal entity, document type and business document identity, line or movement identity, and action kind. Document numbering reuse, fiscal scope and legitimate corrections need explicit domain rules; uncertain identity becomes a review exception.

Keep three different things separate:

- **Business identity:** the event/intention that must not be applied twice.
- **Observed revision:** the source document or target state version used to prepare the change.
- **Proposal revision:** the exact normalized payload being reviewed now.

A corrected document can require a new proposal for the same unresolved operation. It cannot evade a confirmed or unknown original operation by receiving a fresh key. A legitimate second movement, reversal or adjustment has a separately justified business identity linked to the first. A provider idempotency key is an additional request-deduplication mechanism with provider-specific scope/retention, not permanent business uniqueness.

Enforce uniqueness at the business-operation ledger before preparing a new submission. Confirmed identities return existing evidence; unknown identities route to reconciliation; conflicting payloads become `conflict_requires_review`. Define retention/tombstone policy long enough for business duplicate risk, independently of queue and provider key retention.

## 3. Executable package and SDK contract

### 3.1 Full programs, constrained IO

Agents may write full authored programs: branches, loops, parsing, normalization, matching, local SQL, tests and reusable domain libraries. The platform is not restricted to a fixed no-code DAG. However, arbitrary scripts do not acquire instruction-level replay by being packaged. Production authors target a versioned, language-neutral SDK/wire contract; the host decides which entrypoints and runtime images are supported.

Programs must expose named bounded steps and explicit checkpoint boundaries. All network, source access, paid model/OCR calls, artifact publication and business effects go through certified connector/SDK operations. Local pure computation and ephemeral workspace IO are allowed within quotas. Raw outbound sockets, provider SDKs with embedded tokens and hidden subprocess network calls are not alternative IO paths.

A package includes source, immutable build, dependency lock/SBOM/provenance, entrypoint/schema definitions, explicit capabilities, supported checkpoint versions, pure/IO step classifications, retry policy, timeout/cancel policy, budgets, data classifications, tests, preview contract, acceptance evidence and named owner/support lifecycle. Build dependencies execute in a separate restricted build sandbox; build success does not confer production trust.

### 3.2 Normative step interface

```text
StepRequest {
  organization_id, run_id, attempt_id, fence_epoch,
  package_release_digest, runtime_digest, sdk_version,
  step_key, item_or_branch_key, step_input_digest,
  input_artifact_refs[], prior_output_refs[],
  checkpoint_ref?, resume_mode,
  invocation_context_ref, capability_handle,
  deadline, resource_budget_ref, deterministic_seed?
}

StepResult =
  Completed { output_manifest, provenance, next_steps[] }
  | ParkRequested { reason, continuation_manifest, proposal_ref?, question_ref? }
  | Retryable { error_class, retry_after, completed_io_refs[] }
  | NeedsReconciliation { intent_refs[], last_known_observations }
  | Failed { error_class, bounded_diagnostics, partial_output_refs[] }

HostSDK:
  read(operation_id, arguments, read_request_key) -> CapturedRead
  process(operation_id, input_refs, processing_request_key) -> ProcessingReceipt
  stage_artifact(bytes, classification) -> ImmutableArtifactRef
  submit_proposal(normalized_changeset, evidence_refs) -> PendingProposalReceipt
  inspect_effect(intent_ref) -> AuthorizedReceiptProjection
  commit_checkpoint(expected_step_version, manifest) -> CheckpointReceipt
  request_park(checkpoint_ref, pending_reason) -> ParkRequestReceipt
```

The SDK is not a capability grant. Handles are short-lived, audience/org/run/attempt/epoch-bound, validated by the receiving gateway and constrained by current policy. Official apply is deliberately absent from the untrusted SDK: packages propose and later consume effect receipts; the trusted executor runs certified operation handlers. A package's logical `apply` stage means request/observe the governed apply protocol, not execute arbitrary code with production tokens.

The host validates output schema, artifact existence/digests/classification, dependency legality, bounded fan-out and expected step version before checkpoint commit. Package output `next_steps` is a proposal within the certified graph/branch contract, not authority to execute arbitrary steps or alter policy. Reject inconsistent repeated outputs for an already committed step input. Identical committed output is reused without re-running that step.

Step keys/item keys are stable across retries and independent of scheduling order. Persist expansion of dynamic worklists before dispatching children. Enforce bounded pagination, cursor progression and complete input accounting. All items end in a named state: accepted, rejected, quarantined, deferred, duplicate or unresolved; skipping difficult items cannot improve success metrics.

### 3.3 Immutable inputs and reproducibility boundary

Freeze build, SDK, harness adapter, connector/schema, effective configuration and input snapshots per run. Capture attachment bytes, read results and probabilistic extraction outputs with provider request/version information when supplied. Record incomplete pagination and mutable-source capture windows rather than pretending a sequence of reads was an atomic snapshot.

Pure transformations replay from pinned inputs. IO results are journaled and reused where valid. Fresh reads required for authorization or target preconditions are new observations, not silent modifications to approved input. Time/randomness affecting decisions must be explicit inputs or recorded SDK values. Model/OCR determinism is not assumed, even at the same endpoint or reported version; pin captured results and retain uncertainty.

Object storage and PostgreSQL do not share a transaction. Stage immutable objects first, verify availability/digest, then commit authorized references in PostgreSQL; orphan staged objects may be collected after a safety interval. Never mark a step complete referencing an upload merely promised by the runner. A failed or unavailable artifact prevents checkpoint completeness.

An in-place package/dependency/config mutation is forbidden for an admitted run. Revocation can disable a pinned release despite immutability. A changed material input/build/operation creates a new review lineage and invalidates applicable approvals; pinning is not permission to run known-vulnerable code.

## 4. Dispatch, admission and resource accounting

### 4.1 Queue delivery is not domain execution

A domain transaction creates the attempt and launch intent, reserves bounded capacity and budget, and inserts a River dispatch job containing only organization/launch intent references. The launch payload is retrieved from canonical records, not reconstructed from mutable queue arguments.

River can retry a dispatch delivery after worker loss. That retry uses the **same launch intent ID, attempt ID, launch-spec digest and fence epoch**. It must inspect/adopt the existing runner execution rather than invent a new ID. A transport retry is not a new domain attempt or new effect. Queue-job completion means launch handed off or terminally reconciled, not task completed. Queue attempts are operational telemetry, never the business retry counter.

Only the domain controller may close a failed/expired attempt and allocate a successor after checking cleanup, effect uncertainty, remaining budgets, retry policy and current readiness. Retry limits span the run and its attempt lineage so a chain of new IDs cannot reset caps. A dispatch permanently dead-lettered by River leaves a visible domain incident; reconciliation inspects launch state before deciding whether a successor is permissible.

### 4.2 Atomic domain admission

Readiness is derived from accepted accountability, valid dependencies, resolved gates, current access, trusted compatible releases, no blocking unknown effect, available budget and host capacity. Fair selection proposes eligible work; a PostgreSQL transaction rechecks and admits it.

Follow the global lock order in `02-domain-security.md` §10.1: command receipt, authority guards, graph guard, capacity/budget guards, then task/run and subsequent domain rows. Capacity guards use organization, agent, pool/host, connector order and stable IDs. Admission and cleanup/release transactions both obey this order; discovering an earlier required lock after taking a later lock requires transaction restart. The baseline uses READ COMMITTED with explicit guards and conditional version updates; SERIALIZABLE is a separately reviewed optimization, not a competing default. A stale preliminary selection cannot overbook capacity. Revalidate dependency/gate readiness under these guards. No database lock remains held while contacting a runner or provider.

Reserve independently:

- Per organization: active runs/sandboxes, queued admitted launches, daily/period spend, source/export limits, prepared-review WIP.
- Per agent definition and invocation identity: concurrent attempts, model streams, child delegation/fan-out, token/time/spend allowance. A service invoked by many users cannot bypass its ceiling.
- Per execution pool and host: CPU, memory, process/sandbox slots, ephemeral disk, specialized devices and quarantined capacity.
- Per connector/account/provider: request rate, concurrent calls, export bytes, effect throughput and provider quota.

The organization/agent/host reservation must commit atomically with the attempt and launch intent. Host supervisors also enforce real OS limits and reject mismatched capacity claims. Fairness is domain-owned weighted round-robin/deficit scheduling with aging and explicit priority ceilings; specify weights as operator policy and measure starvation. River queue order alone is not fairness. Keep launch reservation TTLs bounded, but expiry after a possible start triggers inspection, not automatic capacity release.

### 4.3 Budget and metering semantics

Reserve estimates before model/OCR/connector calls, then record actual usage with provider request identity and reconcile invoices/receipts without double-counting. Track execution, preparation, review-assistant and improvement spend separately. Unknown usage is `unknown`, not zero. Record price-table version/currency, estimates versus provider-reported usage, streaming usage gaps, cached-token semantics and delayed billing corrections.

A hard monetary cap is only certified where all billable traffic is mediated and an enforceable worst-case reservation or provider limit exists. Otherwise advertise a **soft spend budget** plus enforceable request/token/time/concurrency ceilings, a bounded uncertainty reserve, and escalation. Cancelled model streams may still be billed. Do not free uncertain spend because a process disappeared. Failed paid requests and duplicated OCR submissions may cost money even though they are not official business mutations.

### 4.4 Launch provider contract

```text
EnsureLaunch(launch_intent_id, launch_spec_digest, fence_epoch, spec)
  -> accepted | existing_identical | rejected_conflict | unknown
InspectLaunch(launch_intent_id)
  -> not_started_authoritatively | starting | running | exited | unknown
RequestStop(launch_intent_id, stop_generation)
  -> request_accepted | already_exited | unknown
ObserveCleanup(launch_intent_id)
  -> process_tree_absent + mounts_closed + capabilities_revoked + resources_released
```

The runner persists its launch registry before starting, rejects the same ID with a different spec, and tags the complete execution tree with launch/attempt identity. An inspect result from a different or reset registry cannot establish absence. An unreachable host yields `unknown`; fence/quarantine it and retain accounting until trusted termination/fencing evidence exists. The provider contract must state whether it can prove descendants and mounts are gone, rather than equating parent PID exit with cleanup.

## 5. Checkpoints, park/resume, callbacks and stop

### 5.1 Safe park protocol

`ParkRequested` does not mean `parked_resources_released`.

1. Runner stops issuing new step IO and asks the host to commit completed outputs, continuation, proposed Review Package/gate and all outstanding IO/effect references. The host accepts only current attempt/epoch and expected step versions.
2. In one kernel transaction, persist checkpoint and pending human decision, move the attempt to `quiescing`, disable its new IO admissions/increment the execution fence, and publish stop/reconciliation work. The gate may become visible immediately; cleanup is displayed separately.
3. Broker/gateways reject new requests for the old epoch. Drain or classify admitted in-flight calls; ambiguous processing/effects enter the recovery ledger. Never snapshot an unacknowledged pending tool call as though it completed.
4. The trusted runner quiesces, flushes/upload-verifies artifacts, releases provider sessions and terminates the sandbox/process tree. Forced kill is a stop mechanism, not a reliable checkpoint. A checkpoint produced before kill stays usable only to the extent its artifacts/IO ledger prove completeness.
5. An independent supervisor inspection supplies cleanup evidence. The controller closes the attempt and releases actual capacity once absence is observed. If unreachable, use `cleanup_unknown`, preserve reservations/quarantine host and reconcile. Human-wait state must not hide the remaining resource incident.
6. Once required decisions resolve, make the continuation eligible. A new domain attempt is admitted only after cleanup/effect preconditions pass. Early approval does not skip quiescence or permit overlapping attempts.

A shared controller/watch loop may remain live. The invariant concerns task-dedicated execution resources; no special promise of literally zero goroutines is made. External provider processing may remain pending after local cleanup and is tracked independently.

### 5.2 Native resume versus portable continuation

**Portable continuation is the baseline:** create a fresh sandbox and attempt using the exact package release, committed checkpoint, saved inputs, verified outputs, unresolved items and next named SDK step. Handoff packets additionally contain objective, acceptance criteria, rejected approaches, open questions, evidence permissions and actor context. This reconstructs business progress; it does not restore another harness's stack, pending RPC or instruction pointer.

**Versioned native resume is optional:** an adapter certificate must name the harness build, session/checkpoint schema, dependency/runtime versions, supported outstanding-tool states and workspace restoration semantics. Preserve native artifacts as restricted immutable objects. Restore only an explicitly compatible version or an independently reviewed migration with old/new digests, fixtures and provenance. Reissue current handles; never resurrect credential material, permissions or revoked tool grants from a session dump.

If native resume fails compatibility or integrity checks, fall back to portable continuation when sufficient committed state exists; otherwise raise `continuation_unavailable` for recovery. Cross-harness transfer uses portable packets. A transcript alone is not a durable checkpoint. A resume that changes effects requires a new concrete review revision and applicable approval.

### 5.3 Stale callbacks and fencing

Authenticate callback origin; bind organization, run, attempt, launch intent, fence epoch, event ID and monotonic sequence where supported. Durable inbox uniqueness absorbs duplicates. Conditional domain updates require the current epoch and expected state/version. Old heartbeats cannot renew a new attempt, complete a task, release its reservations or restore revoked authority.

Preserve stale callback evidence for audit/reconciliation without granting it state-transition authority. In particular, a late provider success may prove that an old in-flight effect happened: route it into effect reconciliation rather than discarding it because its originating attempt is closed. Execution-lease validity and external-effect observation validity are different questions.

Provider webhooks require signature/replay/account validation, durable receipt and semantic reconciliation. Out-of-order notifications cannot move a confirmed effect backwards; contradictory evidence raises an incident/current-state lookup. Acknowledgement means durably received, not verified business success.

### 5.4 Stop requested versus observed cleanup

Stop is a kernel command with current authority and idempotency. Atomically mark `stop_requested`, invalidate launch/IO admissions and publish stop work. Dispatch rechecks that state before start; runner/gateway admission still checks the active launch capability to close the cancellation-before-start race.

Show separately: stop requested, runner acknowledgement, local cleanup observed, remote operations still pending/unknown, and final cancelled/partial outcome. Send graceful stop then bounded forced termination according to provider policy. Cancel the undispatched remainder of a changeset, not history. An already accepted remote write can finish after stop; never claim global instantaneous cancellation. Compensation/reversal is a new governed operation and may itself fail.

## 6. Governed read and write lanes

### 6.1 Authority decisions and default policy

Separate package certification, bounded run admission, requester endorsement, technical review, concrete effect approval and independent verification. None implies another. The baseline grants authorized reads/preparation without repeated prompts and requires **eligible human approval of every official-write changeset**. The baseline requires distinct canonical requester and effect-approver humans; aliases or two accounts do not satisfy independence. Any risk-class exception must be an explicit organizational policy decision, not a runtime shortcut.

Standing policy may permit routine scoped reads, paid processing, preparation and recurring run launch within limits/expiry. It does not silently waive official-write approval. Any future standing **write** mandate is an explicit governance change, disabled by default, limited to named operations/resources/predicates/budgets/expiry with independent review, revocation and evidence. Historical approval rates, successful tests, agent feedback or package promotion cannot create it. High-risk/destructive/privilege actions remain explicitly human-governed under organizational policy.

React decision commands submit stored DecisionRequest/gate/revision references, expected version and decision input. The kernel resolves current eligibility/quorum and stores decision, gate transition, audit and follow-up work atomically. Endorsement or personal snooze/dismiss never executes an effect. Revised payloads create new immutable revisions; stale decisions fail visibly without automatic replay onto new content.

### 6.2 Read/preparation lane

Connector operations have reviewed semantic classes: source read, sensitive read/export, local simulation, paid external processing, official mutation, destructive/privilege mutation or unsupported. Do not infer safety from HTTP verbs, MCP labels or a program calling itself `dry_run`. A GET may mutate; a POST may search; mail reads may mark read; a GraphQL document can mix queries and mutations. Unclassified operations are denied.

Read authorization intersects current caller/service authority, agent ceiling, task scope, purpose, resource classification, source/account binding, provider terms and export/rate/budget limits. Apply pagination/result/byte limits; preserve source provenance and incomplete-result warnings. Upstream read-only identities are preferred. When a provider read also mutates official state, expose an explicitly certified nonmutating option or classify it as a write; do not hide the side effect.

OCR/model calls are data exports and billable operations. Enforce approved provider/account/model, residency, retention/training terms, document minimization and classification before upload. Track request intent/receipt, timeouts and possible duplicate charges separately from business-effect approval. A parser's valid JSON is not a factual correctness check. Uncertain digits/units/identities remain exceptions with source citations.

### 6.3 Freeze, render and approve

The kernel canonicalizes a Review Package with organization/account/environment; task/run/package/connector/schema/renderer versions; operation identities/dependencies; exact normalized payload including material server defaults; before/after and related-resource preconditions; evidence/attachment digests; validations and omitted checks; known side effects and unknowns; expiry; cost; verification and compensation plan. Reject undeclared consequential fields or unresolvable defaults.

Host-owned typed React previews render record diffs, inventory deltas, ledger lines, email envelopes and evidence. The host owns account/environment banners, full operation inventory, fidelity warnings and approval controls. Renderers cannot change payloads, hide BCC/sharing/attachments or turn a filtered table into approval of unseen rows. Rich extensions use separate-origin isolation, default-deny network and minimal authorized data, no credentials. Unknown consequential fields block approval until a faithful host rendering exists.

Partial approval binds explicit operation IDs and their prerequisite closure. If shared invariants require whole-batch approval/application, independent-item selection is unavailable. Grouped approval is not a distributed transaction. Changed payload, material input/precondition, connector behavior or invalidated evidence triggers supersession and applicable renewed endorsement/approval. A security revocation can block even an otherwise pinned approved revision.

### 6.4 Effect state and reconciliation

Use the canonical per-operation effect states from `02-domain-security.md`, with batch state derived from them:

```text
prepared -> authorized -> dispatching -> accepted -> applied -> verified
                               |            |          -> needs_attention
                               -> unknown <-+
                               -> failed_no_effect
Before dispatch: stale | rejected | cancelled_before_dispatch
Batch projection: unstarted | in_progress | partial | verified | needs_attention | cancelled
```

Display/protocol terms are not extra database enums: `awaiting_authorization` describes a prepared effect with pending gates; `dispatch_pending` describes queued authorized work; `submitting` means `dispatching` with a `send_started` dispatch receipt; `pending_confirmation` displays `accepted`; `verification_failed` maps to `needs_attention`. Business settlement is separate criterion evidence, not a new effect state. Likewise `quiescing` maps to run `parking`, `cleanup_unknown` to run `unknown` plus cleanup reason, and `stop_requested` to run `cancel_requested` (or the task stop intent).

`dispatch_pending` means no completed submission is yet known; `submitting` conservatively means the remote boundary may have been crossed. `accepted/pending_confirmation` means the provider acknowledged a known asynchronous operation, but final application has not been established. `unknown` means whether/how much applied is unresolved. Neither pending nor unknown is failure-with-no-effect. No new attempt may blindly resubmit an unresolved business identity.

Before any remote write, persist intent, exact payload digest, business uniqueness reservation, provider idempotency key/scope/retention horizon, target conditions and authorization binding. The executor claims with an effect-specific lease/generation, rechecks current stop/revocation/approval/integration/account state, then marks submission risk durably before calling the provider. Effect leases are distinct from preparer attempts: releasing a preparer cannot invalidate evidence that its proposed effect later applied.

No database transaction spans the network call. A crash between `submitting` and request transmission may produce a false uncertainty; this conservative recovery cost is preferable to an unrecorded write. Expired executor lease fences new local dispatch, but cannot prove an earlier process did not send; replacement execution starts in inspect/reconcile mode. The gateway is the last mediated dispatch boundary. Once it admits/transmits a request, revocation is prospective and remote completion must be reconciled.

Recovery order:

1. Read the ledger and authenticated provider receipt/webhook observations for the exact account and operation.
2. Query by provider request/operation ID or certified idempotency lookup; independently read exact target state and relevant business invariants.
3. If provider semantics prove no application, allow policy-bounded retry using the same intent/key while its guarantee is valid. An eventually consistent empty lookup is not proof of absence.
4. If applied, persist evidence and continue verification without resubmitting. If partially applied, record each item and block dependent irreversible operations.
5. If unknowable, keep `unknown`, retain business uniqueness, reserve uncertainty budget and escalate to a named human/operator. Key expiration is a reason for stricter reconciliation, not for a fresh key.

Use provider conditional writes/ETags/versions where certified. Validate related dependencies as well as the primary record: unchanged invoice state does not protect changed vendor bank details or a newly closed accounting period. A local lock serializes only controlled writers; native UI/other clients remain a race. If the provider cannot enforce a material predicate, classify the operation/risk honestly and block unsupported high-risk automation rather than claiming read-before-write is safe.

Compensation is an explicit new operation with original-effect linkage, current preconditions and required human authorization. Code rollback affects future runs only; it cannot roll back inventory, money, sent messages or published documents.

### 6.5 Independent verification and task completion

The host verifier obtains authorized connector readback independently of the package's success output and checks exact organization/account/target IDs, approved payload effects, completeness, invariants and acceptance criteria. Reuse of a certified connector library is permitted, but package-authored checks alone cannot be the independent authority; critical invariants and fixtures belong to the domain owner/verifier release.

Distinguish execution integrity (what was applied) from semantic correctness (whether the matching/business decision was right) and settlement (a later provider/business finality condition). A provider success/readback proves neither the latter two automatically. Unknown, partial, skipped or unverifiable required assertions prevent the kernel from transitioning the task to `done`. Show truthful progress and named outstanding decisions instead. Publish verified receipts with observation timestamps and link later disputes/reversals without rewriting history.

## 7. Integration and agent-adapter certification

### 7.1 Integration manifest

A manifest is an assertion to test, never certification by itself. Pin publisher, license, release digest, protocol/schema ranges, operator/maintainer, dependency lock, requested capabilities, data classification/residency/retention, endpoint/egress inventory, storage/telemetry behavior, quotas and revocation policy. Requested capabilities are not granted capabilities.

Declare each **operation**, not just each connected vendor:

| Interface | Required contract |
|---|---|
| `read` | Typed inputs/results, semantic side effects, account binding, pagination completeness, freshness/version/provenance, export limits |
| `prepare` | Side-effect-free with respect to official systems; normalized operation proposal, exact identity/defaults, evidence and uncertainty |
| `render` | Host preview schema/version, full consequential field coverage, fidelity limits, safe fallback and accessibility |
| `validate` | Local versus provider checks, precondition coverage including related objects, freshness, omitted checks and validation side effects |
| `execute` | Supported typed mutation, credential mapping, exact payload normalization, condition/idempotency semantics, timeouts, triggers and async acceptance |
| `verify` | Independent exact-target readback, eventual consistency window, semantic invariants, settlement, reconciliation/compensation limitations |

Maturity labels are **unsupported**, **read-only**, **prepare-preview**, **governed-apply**, and **verified-apply**. Store detailed interface evidence even when using these summary labels. Read support does not imply preview fidelity; prepare/render support does not imply safe execution; governed application can still lack adequate verification. Required task effects need verified-apply capability for the required acceptance scope, or an explicit human/native recovery path that supplies evidence. Do not label a whole provider verified because one endpoint passed.

Certification key: connector/build + operation/schema + provider API/version/region/account class + environment + credential model + tested options/preconditions + evidence/date. Declare untested combinations unsupported. Sandbox tests establish only sandbox behavior; production can differ in hidden workflows, permissions, idempotency retention or side effects. Production activation requires bounded provider-specific qualification and human-approved test scope, not unverifiable global guarantees.

Provider conformance must exercise wrong account, permission denial, field/default fidelity, pagination, conditional-write conflicts including native writers, duplicate keys, expired keys, timeout after commit, async acceptance, partial batches, exact readback, webhook authenticity/order, rate limits, credential rotation and stop races. Capture sanitized request/receipt evidence and resulting official state. Record unsupported races and explicit go/no-go per operation. API drift/sentinel failures quarantine the affected capability; current tasks retain their evidence and move to safe recovery.

### 7.2 Agent/harness adapter contract

Employee-selected harnesses run only as approved adapters on organizational compute. Candidate adapters include Hermes, DeepSeek Harness, OpenCode, Codex and Claude Code; **none is certified by being named here**. ACP/MCP/headless support is an interface claim, not tenant isolation, durable resume or safe cancellation.

```text
DescribeCapabilities(pinned_release) -> evidence-linked capability manifest
PrepareInvocation(task_packet, effective_config) -> immutable launch spec
StartOrInspect(launch_intent_id) -> runner handle and normalized events
RequestStop(handle, generation) -> acknowledgement, not cleanup proof
ExportPortableCheckpoint(handle) -> host-validated artifact manifest
ExportNativeState(handle) -> versioned restricted snapshot | unsupported
ResumeNative(snapshot, certified_version_tuple) -> new attempt | unsupported
ReadUsage(handle) -> measured | estimated | unavailable + provenance
```

The trusted runner mediates actual process starts; adapters cannot launch peers outside its inventory. Capability evidence covers headless operation, tool injection/allowlisting, subprocess inheritance, network containment, stable event identities, cancellation/process-tree cleanup, checkpoint/export behavior, native pending-tool resume, portable restart, workspace retention, metering, model routing, secret/log redaction and licensing/automation terms. Some features may be supported while others are explicitly unavailable.

Certification tiers are capability combinations, not a marketing ranking: preparation-only with portable restart can be useful without native resume. A harness with an unmediated browser/network/credential path cannot enter the official-write workflow. Unknown monetary usage precludes hard-dollar guarantees but can permit policy-approved bounded preparation. A native approval prompt is advisory; the effect gateway remains enforcement authority.

Install lifecycle: quarantined -> verified -> human approved -> installed -> ready -> active -> draining -> disabled/removed. Mandatory dependency/readiness failure blocks admission with diagnostics. Version upgrades drain old attempts; compatible pinned runs may finish unless revoked. Permission expansion, runtime self-installation or new egress cannot inherit old approval. No employee executable configuration loads inside kernel processes.

## 8. Credentials, egress and shared-agent context

### 8.1 Broker and network isolation

Human sessions use the architecture's verified Logto-backed identity mapping; relationship decisions use kernel policy/OpenFGA with current deny/revocation handling. Runtime identity is a short-lived authenticated workload identity, not an employee ID in an environment variable. Separate provider credential custody from package code.

The broker binds credentials to organization, official account, production/test environment, operation lane and authorized mandate. Validate upstream account identity at connection setup and before applicable actions; a valid token for the wrong company is invalid for the request. Use separate upstream read and write identities where supported. When a broad provider token is unavoidable, keep it inside the trusted broker/executor and enforce semantic restrictions there; explicitly account for increased compromise blast radius.

Sandbox egress is default deny, with only authenticated typed gateways reachable. Block metadata services, private control networks, production databases/SSH, Docker/container sockets, host mounts, raw cloud credentials, arbitrary DNS/HTTP proxies and user-supplied forwarding headers. Check destination resolution and redirects at each hop, prevent DNS rebinding/SSRF, pin allowed account endpoints, and make signed-URL downloads scoped/minimized. Artifact upload/download handles bind exact objects/bytes, method, audience, size and expiry; possession of another tenant's URL is not authorization.

Do not mount authenticated personal browser profiles into preparation sandboxes. Browser-only official writes are **human-only initially**. Later certification requires an isolated mediated browser executor with exact-action review, credential isolation, egress controls, observed effects and recovery; an HTTP gateway beside an unrestricted logged-in browser is bypassable. Even read browser sessions may mark records read or trigger downloads/tracking; classify their actual effects. Renderer frames/assets cannot exfiltrate evidence through remote images or arbitrary navigation.

Model routing prefers LiteLLM where an adapter supports it, with explicitly reviewed direct-provider exceptions. Both paths must enforce egress/data/metering policy. OTel and permitted Langfuse traces are telemetry, not authority. Redact before logs/traces/export; raw prompts, documents, tokens and native session state require separate classification/retention approval. Provider/OCR egress grants do not grant general internet access.

### 8.2 Caller-delegated versus global service authority

A globally available org agent is discoverable, not omniscient. Each invocation explicitly chooses:

- **Caller-delegated:** effective permission = current caller rights intersected with agent ceiling, delegated grant, task/purpose/resource constraints and organization policy. Retain caller + workload attribution. Recheck on retrieval/use/dispatch; historic access is not evergreen.
- **Service mandate:** independent organizational sponsor, permitted observations/maintenance operations, resources, budget, expiry and revocation. No fabricated caller. A maintenance bot does not borrow the most privileged recent user's context.

An app-only effect executor acts as its actual upstream service principal. Kernel authority must explicitly bridge approved organizational decisions to that service capability; it does not automatically inherit the requester/approver's provider rights. Where the provider/business requires genuine user delegation, require a supported delegated token flow or refuse the operation. Record requester, preparer, reviewer, approver, mandate, gateway and actual downstream principal separately.

Partition invocation memories, caches, files, tool handles, model sessions and retrieval results by organization plus effective authority context, with resource/purpose/policy versions where relevant. Reauthorize before serving cached sensitive content; cache keys alone do not handle revocation. Reviewer-private evidence/findings cannot leak through requester projections or shared assistant history. Cross-company use is separately authenticated fan-out, never a broad tenant-selector parameter.

Persistent services retain stable identity/inbox and subscriptions; each reasoning invocation is bounded. Deduplicate causal events, cap fan-out/depth, apply cooldowns/no-progress breakers and separate budgets. Deterministic controllers process readiness and empty inboxes without waking an LLM. Board Steward is advisory initially, never scheduler correctness, independent verifier or its own privilege authority.

## 9. Gontext boundary and actual integration gaps

### 9.1 Required workforce adapter contract

Use separate APIs/databases and independently authenticated org-scoped calls. The workforce knowledge adapter exposes bounded `FetchContext`, `PublishObservation` and `CatchUp`, not direct access to Gontext tables or its internal NATS subjects.

- Retrieval uses supported REST/MCP search/get/brief/graph surfaces with explicit purpose. Preserve resource/revision/citation IDs, classification/redactions, policy/authz revision, audit ID, truncation and cursor metadata. A context reference is not an access grant. Recheck access for handoff recipient, reviewer and subsequent use.
- Publication consumes the workforce transactional outbox. Map reviewed events/observations to registered source-bound CloudEvents using a versioned MappingSpec, stable event ID/idempotency key, source HMAC and an appropriately scoped ingest bearer. Store publication receipt/retry state separately from task completion. Source ceilings limit trust, authority, record types and visibility; agent conclusions remain generated observations, not source-of-truth business records.
- Use contract-supported record kinds such as `event`, `case` and `observation`; an older suggestion to send `task` must not rely on a permissive implementation accepting a value absent from its published enum. Assignment graph facts do not automatically establish authorization tuples or workforce accountability.
- Consume metadata-only change feed/webhooks with authenticated org/account binding and durable dedupe/cursor state; hydrate through newly authorized retrieval. Reconcile gaps using the provider's supported replay/resync contract. Do not infer confidential content from an event or treat arbitrary change events as execution commands.
- No distributed transaction with Gontext is required. Idempotent outbox retries may lag. Context-required work waits on denied/unavailable/insufficient context; work with sufficient authorized pinned inputs may proceed under freshness policy. Knowledge publication failure alone does not undo confirmed official effects or globally halt unrelated work.

### 9.2 Source-reviewed gaps, not newly tested capabilities

The supplied `/home/prod/research-gontext-digest.md` is a prior source review, not a fresh deployed conformance result. Its strong statements about tests mean fixtures/source were found; this chapter does not infer that the deployed stack passed them. Re-pin and inspect the actual target release before qualification.

| Prior source finding | Consequence / required acceptance |
|---|---|
| Agent API-key scope is hard-wired to `context:search` and `context:read` | Do not promise agent-key ingest or even scoped access-request creation without an appropriate supported identity. Use an approved ingest-scoped service/source path for observations; qualify its ceilings. |
| Delegation table/schema/check logic exist, but HTTP/MCP do not populate retrieval delegation; credential delegation ID is not resolved end-to-end | Caller-delegated Gontext access is a blocker, not established by an owner ID or schema. Implement/qualify true delegation, use a genuinely caller-authenticated supported path, or restrict to separately authorized service-mandate context. A broad service token plus claimed caller metadata is insufficient. |
| No governed general member/reader/assignee tuple-admin API/CLI in the reviewed code; parent tuple sync is the implemented narrow path | Onboarding requires reviewed operator-managed tuples or a separately authorized Gontext addition. Workforce assignment must not be represented as if it automatically provisions visibility. |
| Access requests create pending rows; no list/approve/deny lifecycle or notification path was wired | Show `awaiting_access` and an explicit operator/native access process. Do not offer a fictional Gontext approve endpoint. A workforce approval does not itself grant Gontext access. |
| Search index populated title, not body/evidence; parser/embedding implementations absent | Do not promise full-document semantic search or OCR. Use scoped known-ID retrieval and separately authorized parser connectors; richer search requires its own qualified release. |
| Graph `next_cursor` signals truncation but resumable graph pagination was not implemented | Bound graph requests and display incompleteness; never loop a cursor as if it traversed all results. |
| Policy purpose list and MCP tool enums differed | Negotiate/test an actually accepted purpose on the pinned API; do not invent `task_execution` or silently substitute a different business purpose. |
| Observation retention/review-state enforcement and third-party plugin runtime were planned/not wired | Enforce workforce artifact/publication policy and qualify downstream retention; no claim that a schema field or plugin manifest enforces it. |
| Intake accepts source-bound events, read-only MCP and metadata change feed exist in source; operations/release CI evidence was incomplete | Qualify actual auth, account binding, replay, idempotency, deny, lag, restore and version contracts against the target instance. Existence in source is not release readiness. |

These are integration prerequisites or explicit reduced-function modes, not instructions to modify Gontext in this task. Keep source-of-truth records, authorization, evidence retention and knowledge observations distinguishable. Gontext's internal technology exclusions do not dictate the workforce queue/runtime choices.

## 10. Continuous improvement without self-authority

Production, learning and governance are separate loops:

1. **Production:** fixed approved release + pinned inputs -> prepare -> exact human approval -> apply -> independently verify/reconcile. Failures preserve evidence and isolate affected items where shared invariants permit.
2. **Learning:** collect structured outcomes/corrections -> deterministic clustering -> bounded candidate authoring -> independent evaluation -> read-only shadow. Routine successes need not invoke a model.
3. **Governance:** own acceptance criteria, protected verifier/evaluation corpus, capability ceilings, release approval and exception policy. Neither production nor learning can modify these to pass.

Each maintained package has a technical maintainer and accountable domain owner. Corrections record proposed correct value/action, reason, evidence, applicability scope (item/supplier/format/workflow/org), effective dates, uncertainty and permission to propose generalization. Default narrow. Labels distinguish human-approved, provider-confirmed, adjudicated-correct, disputed, reversed and unknown. Approval clicks are not ground truth.

Candidate lifecycle: `proposed -> built -> evaluated -> shadowed -> reviewed -> limited_rollout -> promoted | rejected`. A repair agent may produce a new code/config artifact but cannot alter protected verifier/policy/holdout in the same release. Authorized maintainers own publication; organizational release authority owns promotion. Permission expansion always requires a separate decision.

Evaluation fixes incumbent baseline, all-input denominator, risk-specific acceptance criteria and independent holdouts split by supplier/format/time where relevant. Measure false matches, omissions, quarantined/abandoned items, delayed disputes, review/rework burden and execution/learning cost—not only exception rate. Audit a sample of auto-accepted items so invisible errors enter the corpus. Protect holdout access and record contamination; test suites authored with the candidate are development evidence, not independent proof.

Shadow uses captured authorized inputs or certified read-only calls with no official write credentials. A consequential canary is a bounded, separately human-approved changeset, not permission to experiment on irreversible writes. Candidates cannot self-install, change budgets, weaken checks or increase source/export scopes. Promotion creates a new immutable release; parked runs remain pinned unless explicitly migrated/replanned.

Cluster and dedupe recurring failures, cap candidate fan-out/WIP, reserve a separate learning budget and apply cooldowns. Diagnose data/config/connector/parser/business-policy defects before prescribing code changes. Keeping the incumbent is a valid outcome. After promotion monitor delayed outcomes; rollback stops future use, maps affected runs/items and separately authorizes reconciliation/compensation. Report net benefit including specification, review, exception handling, maintenance and incident effort rather than promised labor savings per run.

## 11. End-to-end pseudocode

The following specifies transaction and authority boundaries, not runnable implementation or exact River SDK calls. All state labels resolve to the domain schema; helpers named `transaction` include tenant binding, conditional versions, audit and transactional outbox where applicable. External calls occur outside transactions. Authority checks incorporate current deny state and the architecture's consistency/outage policy; a stored policy snapshot alone is insufficient.

### 11.1 Create, admit and launch a bounded preparation attempt

```text
PrepareWorkflowBinding(human_session, task_ref, certified_release, input_manifest, request_key):
  actor = AuthenticateAndBindOrganization(human_session)
  artifacts = ValidateImmutableArtifacts(input_manifest)
  transaction:
    DedupeCommand(actor.org, request_key)
    AuthorizeCurrent(actor, "run.prepare", task_ref, purpose)
    AssertAcceptedAccountabilityAndTaskVersion(task_ref)
    AssertReleaseTrustedAndInputsPermitted(certified_release, artifacts)
    binding = AttachImmutableExecutionManifest(task_ref, release, artifacts, acceptance_version)
    AppendAuditAndOutbox("task.execution_eligible", binding)
  return binding

AdmitNext(fairness_candidate):                   # domain controller only
  placement = ProposeHealthyCertifiedHost(candidate.resource_class)
  transaction:
    LockQuotaRowsInCanonicalOrder(org, agent, placement.host)
    LockRunTaskReadinessState(candidate)
    RecheckReadyCurrentAuthorityTrustDependenciesAndStop(candidate)
    AssertNoLiveOrCleanupUnknownPredecessor(candidate)
    AssertNoBlockingUnknownBusinessEffect(candidate)
    ReserveOrgAgentHostAndBudgetAtomically(candidate, placement)
    attempt = CreateDomainRunAndAttempt(next_ordinal, new_fence_epoch, binding)
    launch = CreateLaunchIntent(attempt, immutable_spec_digest, expiry)
    RiverOSS.InsertTx(tx, Dispatch{org_id, launch_intent_id: launch.id})
    AppendAudit("attempt.admitted", attempt)
  return launch

Dispatch(job):                                 # River may redeliver
  launch = Kernel.LoadAndAuthorizeCurrentLaunch(job.org_id, job.launch_intent_id)
  if revoked_or_cancelled(launch):
    ScheduleLaunchInspectionAndStop(launch)     # could already have started
    return delivery_complete
  observation = Runner.InspectLaunch(launch.id)
  if observation is exited:
    Kernel.RecordObservedOutcomeAndScheduleCleanup(launch, observation)
    return delivery_complete                  # never restart this launch ID
  if observation is existing_matching_start_or_run:
    Kernel.RecordLaunchHandoffConditionally(launch, observation)
    return delivery_complete
  if observation is unknown:
    Kernel.RecordLaunchUnknown(launch)
    return bounded_transport_retry_same_job
  result = Runner.EnsureLaunch(launch.id, launch.spec_digest, launch.epoch, spec)
  # Runner checks current start admission, persists registry, then starts.
  if result is ambiguous:
    return bounded_transport_retry_same_job    # no fresh attempt or launch ID
  Kernel.RecordLaunchHandoffConditionally(launch, result)
  return delivery_complete                     # task is not done
```

A runner rejected-conflict result is an integrity incident, not a retriable start with new parameters. If transport retries exhaust, the domain recovery controller still inspects the same launch before closing/replacing it.

### 11.2 Prepare, checkpoint and park for humans

```text
InventoryProgram(step_request, sdk):
  email = sdk.read("mail.read_without_marking", bounded_scope, stable_read_key)
  attachment = sdk.stage_artifact(email.attachment_bytes, inherited_classification)
  extracted = sdk.process("document.extract", [attachment], stable_processing_key)
  sources = sdk.read("inventory.purchase_receipt_snapshot", exact_ids, next_read_key)
  normalized = PureNormalize(pinned_rules, extracted.saved_output, sources.saved_output)
  matches, exceptions = MatchWithDomainRules(normalized)   # uncertainty not guessed
  changeset = PrepareExactOperations(matches, dependencies, business_identities)
  proposal = sdk.submit_proposal(changeset, evidence_refs)
  checkpoint = sdk.commit_checkpoint(expected_version, {
    inputs, extraction_receipt, matches, exceptions, proposal,
    next_step: "observe_effects_then_prepare_response"
  })
  return sdk.request_park(checkpoint, "requester_endorsement_then_human_effect_approval")

Park(attempt, checkpoint, proposal):
  transaction:
    AssertCurrentEpochAndCompleteCheckpoint(attempt, checkpoint)
    FreezeReviewPackageAndCreateCanonicalGateDecisionRequests(proposal)
    MarkAttemptQuiescingAndFenceNewIO(attempt)
    PersistContinuationAndOutstandingIO(attempt, checkpoint)
    AppendOutbox("runner.stop", attempt.launch_id)
  # Gate is pending now, even if runner cleanup is not complete.
  RequestStopAndReconcileInFlightIO(attempt)
  evidence = Runner.ObserveCleanup(attempt.launch_id)
  transaction:
    if not AuthoritativeCessationEvidence(evidence):
      MarkCleanupUnknownAndRetainReservations(attempt)
    else:
      CloseAttemptAndMarkFullyParked(attempt, evidence)
      ReleaseOccupiedCapacityExactlyOnce(attempt.reservations)
      ReconcileActualAndUncertainSpend(attempt)
```

These illustrative operation names belong to the future catalog and must be implemented/certified; they are not existing provider endpoint claims. An actual unsupported nonmarking mail read cannot be substituted silently.

### 11.3 Resolve decisions and execute exact operations

```text
ResolveDecision(human_session, decision_ref, reviewed_revision, selection, key):
  human = AuthenticateCanonicalHuman(human_session)
  transaction:
    DedupeCommand(human.org, key)
    LockDecisionGateProposalAndTaskVersions(decision_ref)
    AssertPendingExactUnsupersededRevision(reviewed_revision)
    AssertCurrentEligibilityQuorumAndDistinctHumanRules(human, decision_ref)
    AssertExactSelectionAndPrerequisiteClosure(selection)
    RecordEndorsementOrApprovalAccordingToDecisionKind()
    if all_required_decisions_satisfied:
      MarkGateApprovedRequestsResolvedAndEffectsEligible()       # no general agent write token
      AppendOutbox("effects.admit_when_ready", proposal.id)
  return canonical_decision_receipt

AdmitEffect(proposal, operation):
  transaction:
    AssertCleanupAndEffectDependenciesSatisfied(proposal)
    LockBusinessOperationAndEffectReservation(operation.business_identity)
    if linked_effect.state == verified: return existing_receipt
    if linked_effect.submission_possible_or_unresolved: return enqueue_reconciliation
    AssertApprovedExactRevisionCurrentMandateTrustStopAndBudget(operation)
    intent = PersistOrLoadExactIntent(operation, stable_provider_key, retention)
    ReserveEffectCapacityBudgetAndEnqueueTx(intent)
  return pending_receipt

ExecuteEffect(intent_id):
  intent = LoadStoredIntentNotCallerPayload(intent_id)
  transaction:
    ClaimEffectLeaseAndCheckCurrentAuthorization(intent)
    if prior_submission_possible(intent):
      EnqueueReconciliationAndReturnWithoutSubmitting()
    AssertPayloadDigestAccountAndCurrentApprovedRevision(intent)
  validation = CertifiedConnector.ValidateCurrentMaterialPreconditions(intent)
  if unsupported_or_stale(validation): return Kernel.MarkStaleOrBlocked(intent)
  transaction:
    RecheckLeaseCurrentAuthorityStopApprovalTrustAndExpiry(intent)
    MarkSubmittingAndPersistSubmissionGeneration(intent)
  result = EffectGateway.ExecuteStoredOperation(
    intent, generation, conditional_predicates=validation.provider_enforced_conditions)
  # Gateway checks current dispatch admission and obtains scoped broker credentials.
  if timeout_disconnect_or_uncertain(result):
    Kernel.RecordUnknownAndEnqueueReconciliation(intent)
  else:
    Kernel.RecordProviderObservationAndScheduleVerification(intent, result)
```

Validation cannot make an unprotected provider write atomic. If current material predicates cannot be enforced upstream, apply the operation-specific risk gate rather than execute under a claimed guarantee. The final dispatch boundary defines whether cancellation occurred before admission or while a remote operation might already be in flight.

### 11.4 Reconcile, verify, continue and improve

```text
Reconcile(intent):
  observations = CertifiedConnector.InspectExactOperationAndTargets(intent)
  if observations.prove_applied:
    PersistAppliedEvidenceAndScheduleIndependentVerification(intent, observations)
  elif observations.prove_no_effect_under_certified_provider_semantics:
    MarkSafeRetryEligibilityWithoutChangingBusinessIdentity(intent)
    # Re-admission still checks current approval, key horizon, policy and budget.
  else:
    PreserveUnknownAndEscalate(intent, observations)

VerifyAndAdvance(run):
  evidence = IndependentVerifier.CheckExactReceiptsAndDomainCriteria(run)
  transaction:
    RecordVerifierEvidence(evidence)
    if missing_partial_unknown_or_failed_required_checks(evidence):
      SetCanonicalExceptionOrVerificationWait(run)
      return
    if remaining_program_steps(run):
      MarkContinuationEligible(run)              # fresh domain-admitted attempt
    elif all_task_requirements_satisfied(run.task):
      AssertNoLiveExecutionOrUnverifiedCleanup(run.task)
      TransitionTaskToDone(expected_task_version, evidence)
      AppendOutbox("knowledge.publish_observation", source_cited_outcome)
  # A supplier success email is a separate exact send proposal/gate.
  # It cannot claim inventory success until inventory verification passes.

ProcessCorrections(bounded_window):
  clusters = DeterministicallyDedupeAndClassifyAuthorizedCorrections(bounded_window)
  for cluster within learning_budget_and_WIP:
    candidate = AuthorSandboxedCandidateAgainstImmutableIncumbent(cluster)
    report = IndependentEvaluation(candidate, protected_holdout, fixed_acceptance)
    if report.meets_requirements:
      ReadOnlyShadowAndRequestAuthorizedReleaseReview(candidate, report)
    # Only a separate authorized Promote command publishes the approved release.
```

## 12. Failure scenarios and acceptance evidence

These are required tests, **not executed results**. Chapter `05-operations-assurance.md` owns the broader assurance program; this matrix identifies runtime-specific observable outcomes.

| Scenario / injected failure | Required state and evidence |
|---|---|
| Duplicate River delivery or launch response lost after start | Same launch ID/spec/attempt adopted; runner inventory shows no duplicate sandbox; domain attempt count unchanged |
| Concurrent admissions at org/agent/host limits | One atomic reservation outcome per admissible slot; no oversubscription; bounded fairness measured across organizations |
| Stop between enqueue and start | Current launch capability rejects start, or the raced accepted start is stopped/inspected; no fabricated cleanup |
| Host disappears while running or parking | Cleanup unknown, host quarantined, occupied capacity retained; no successor overlap until authoritative fencing/cessation evidence |
| Approval arrives before cleanup | Decision survives; fully parked/capacity release and successor admission wait for cleanup evidence |
| Process killed before/after checkpoint commit | Only committed complete artifacts/outputs reused; incomplete step reruns with IO reconciliation; no arbitrary stack-resume promise |
| Old worker heartbeat/completion after replacement | Inbox retains evidence; stale epoch cannot complete run, renew lease or release successor capacity |
| New release installed while native state is parked | Old compatible release remains pinned or explicit migration/portable fallback; revoked release blocks admission |
| Unknown remote result after commit before receipt write | Intent/business key remains reserved; exact provider inspection/readback; no blind new-key write |
| Provider accepts asynchronous request | Pending confirmation remains visible; task not done until required observed application/verification/settlement |
| Provider key expires before retry | Reconciliation required; durable business identity prevents duplicate proposal from bypassing uncertainty |
| Same document in two emails / supplier reuses number | Duplicate delivery dedupes; genuinely distinct business event uses reviewed scoped identity; ambiguity becomes exception |
| Native application changes record or related bank/unit/period state | Provider CAS/predicate failure stales approval; unsupported related-object race blocks high-risk certification |
| Batch partially applies then fails | Exact item states/receipts; dependent irreversible operations blocked; separately approved recovery/compensation |
| Wrong org token, account ID or fabricated caller metadata | Denied before data disclosure/submission, with safe audit; provider-valid credentials alone cannot authorize |
| Revocation after approval or while provider call is in flight | New dispatch blocked; possibly accepted operation reconciled; no promise of reversal by token revocation |
| Raw HTTP, DNS, SQL, browser profile, redirected URL or renderer image bypass | Sandbox/gateway deny evidence; no credential/data leakage; browser-only writes remain human-only |
| OCR upload violates classification/residency or times out after acceptance | Export denied before transfer, or processing receipt remains uncertain with reserved usage; no unmetered retry storm |
| Forged/duplicate/out-of-order callback | Signature/account/inbox checks; semantic current-state reconciliation; no backward transition or cross-tenant disclosure |
| Package reports success but omits lines or lies about totals | Independent all-input accounting/target readback fails; task cannot become done |
| Two caller invocations share global agent | Isolation tests across memory/cache/files/credentials and revocation; service mode never inherits last caller |
| Gontext denies/outages/truncates/unwired delegation | Explicit access/context blocker or qualified sufficient-input continuation; no broad-service fallback masquerading as caller delegation |
| Model usage missing after disconnect | Unknown cost retained; hard-cap claim disabled unless enforceable bounds exist; late usage reconciled once |
| Shadow candidate attempts official write or edits holdout/verifier | No credentials/capability; protected data/policy rejected; candidate cannot promote itself |
| Candidate lowers exceptions by dropping records | All-input denominator and omission checks reject misleading improvement; incumbent may remain |
| Rollback after bad applied changes | New admissions stopped; impacted receipts mapped; governed compensation separate from binary rollback |

Acceptance artifacts must include pinned software/provider configurations, named tester/approver, reproducible fixture/fault steps, sanitized logs and requests, canonical before/after state, runner inventory/cleanup observations, reservation/usage reconciliation and pass/fail/unsupported decisions. Static source inspection and simulated provider tests are valuable but labeled separately from live provider conformance.

## 13. Sources, precedence and remaining qualification

Read and incorporated:

- `/home/prod/workforce-build-spec/01-architecture.md` — baseline and cross-chapter invariants; `done` requires verified evidence and pending gates precede cleanup when necessary.
- `/home/prod/workforce-build-spec/02-domain-security.md` — canonical task/run/gate/effect states and mapping of a domain run to an admitted attempt.
- `/home/prod/research-org-workforce-plane-plan-v2.md` — modular kernel, River OSS, launch/park semantics, global services and Gontext boundaries.
- `/home/prod/research-workforce-executable-work-packages.md` — full authored packages, SDK checkpoints, immutable runs and independent verification.
- `/home/prod/research-workforce-governed-action-gateway.md` — semantic read/export/write lanes, broker enforcement and mandatory official-write human approval default.
- `/home/prod/research-workforce-automation-learning-stress-test.md` — business dedupe, unknown outcomes, provider limitations and independent learning loop.
- `/home/prod/research-workforce-unified-review-v2.md` — latest matching unified-review-v2 file discovered; immutable Review Package, distinct decisions, exact effect selection, operation-level maturity and host-owned previews.
- `/home/prod/research-gontext-digest.md` — additional prior source evidence for actual API/delegation/access-request/search gaps; not a fresh runtime claim.

Remaining implementation qualification: exact River OSS transaction/driver behavior; runner fencing/cleanup and isolation; pinned harness capabilities; selected inventory/mail/OCR provider operation semantics; identity/delegation/account mapping; Gontext deployed API gaps; provider metering guarantees; migration, artifact retention and restore behavior. No runtime conformance, live provider effects, implementation or deployment was performed in producing this chapter.
