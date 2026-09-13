# 04 — Product experience and delivery roadmap

## 1. Status, scope and implementation assumptions

This chapter specifies a proposed product and testable delivery sequence; it does not claim an implemented backend, tested harness, supplier integration or measured labor saving. Its source baseline is:

- [Workforce plane v2](../research-org-workforce-plane-plan-v2.md): trusted kernel, full organizational model, bounded execution and extension boundaries.
- [Unified review v2](../research-workforce-unified-review-v2.md): authoritative review semantics, including incorporated Agency amendments.
- [Agency UX strategies](../research-agency-ux-strategies.md): outcome-first interaction and attention continuity, adapted to authenticated organizational work.
- [Automation and learning stress test](../research-workforce-automation-learning-stress-test.md): executable packages, independent verification and governed improvements.

**Proposed default:** one modular Go backend, PostgreSQL operational persistence, River OSS for transactional job delivery, and a React/TypeScript frontend. HTTP command/query and event contracts remain language-neutral. Isolated execution workers, connector processes and optional view plugins are not additional business-authority backends. River is a candidate requiring the milestone proof below, not a demonstrated performance conclusion. Durable human gates belong to domain records, not sleeping River jobs.

This is a separate workforce product. Gontext remains its governed knowledge/context integration, not its task database; Vlobel and Dhanda are not presumed implementation dependencies. Agency interaction ideas will be implemented independently; copying its code or assets requires established permission.

### Assumptions to resolve rather than silently invent

| Assumption | Resolution owner and delivery consequence |
|---|---|
| A sponsoring organization can name a domain owner, approver policy and pilot workflow | Product/domain responsibility establishes these before any real write. No fictional employee names or implied manager authority. |
| Supplier email and inventory systems are not yet selected or demonstrated | Connector responsibility discovers actual provider/account/API/export availability. Start with clearly labeled synthetic fixtures and a purpose-built simulator. No invented supplier endpoint is a live integration. |
| Provider supports adequate account binding, idempotency, conditional updates and readback for a chosen operation | Record measured operation-level capabilities. Missing guarantees may permit preview only; block unsupported high-risk automated writes. |
| Organization-controlled compute and an identity provider will be available | Platform operations supplies and tests them. Local preparation means a task-isolated workspace on approved compute, not an employee laptop with production secrets. |
| Gontext adapter contracts and deployment must be inspected during implementation | Use a contract test double initially; declare unavailable features rather than assuming an existing endpoint or shared database. |
| Workload sizes, latency targets and rollout staffing are unknown | Benchmark representative pilot fixtures and agree budgets in PD-00. Dependencies below are commitments; calendar estimates await capacity discovery. |
| Approval, retention, employee consultation and analytics policies vary by company | Governance defines explicit policy before enrollment; organizational hierarchy alone grants no rights. |

### Full target, not just the first pilot

Retain multi-company separation, temporal human reporting hierarchy and matrix teams; a board for each person and agent definition; projects, milestones and dependency planning; canonical tasks and accepted handoffs; prepared domain decisions; requester endorsement followed by authorized human effect approval; full-code local preparation; package registry, templates and employee-selected/BYO harnesses; caller-scoped shared organization assistants; advisory Board Steward; Gontext knowledge; and continuous governed improvement. Early releases narrow activation and privilege, not this model. Later milestones below explicitly deliver the remaining target.

## 2. Product information architecture and screen projections

Three coordinated surfaces organize daily work: **shared commitments**, **my decisions**, and **execution evidence**. They reference one canonical task, not copied cards with independent status. Company selection is explicit and persistent. Cross-company portfolios are separately authorized aggregate queries; switching companies clears incompatible cached records and drafts from the visible session.

All projections are server-authorized before pagination, search facets, counts or aggregation. Proposed query names below are contract names, not assertions that APIs exist. Each result carries organization, projection schema version, observed-at timestamp, source watermark and cursor/completeness metadata. Commands use canonical IDs and expected versions, never a projection row as authority. Live events invalidate/requery views; event bodies receive the same access filtering. Reconnection gaps trigger a fresh snapshot. A stale projection may display information with a warning but cannot bypass command-time validation.

| Screen / proposed query projection | Required content | Commands and boundaries |
|---|---|---|
| **Company home / `OrganizationOverview`** | Active company, current memberships, authorized projects, actionable request counts, blocked work, recent verified outcomes; other companies only when independently authorized | Select company; open work. No implicit global-admin dashboard. Counts cannot reveal restricted teams. |
| **People, hierarchy and matrix / `OrganizationRelationships`** | Positions, reporting relationships, effective dates, vacancies, matrix teams, project roles and cover/leave delegation; organizational map plus accessible table | Authorized relationship changes have effective dates and audit. Display “reports to” separately from “can approve”; private work remains filtered. |
| **My board / `PersonWorkBoard`** | Accountable work, executing work, participation, awaited decisions and incoming handoff offers in distinct filters; canonical task IDs, deadlines, dependencies and actual blocker owner | Create scoped task, propose handoff, request clarification, edit permitted planning fields. My decision inbox remains distinct from my commitments. |
| **Agent board / `AgentWorkBoard`** | Stable agent definition, sponsor/ceiling, template and harness version, assigned tasks, active attempts, parked gates, queue eligibility, budget, last heartbeat and unconfirmed cleanup | Assign executor through an authorized command; pause/revoke under policy. “Waiting for human” is not an active reasoning process. Agent identity is not a human approver. |
| **Project planning / `ProjectPlan`** | Milestones with acceptance evidence, task dependencies, forecast versus committed dates, accepted accountable owner, executor, reviewer, approver and awaited actor; list, board and dependency/timeline views | Create dependency, propose milestone/date/owner change, accept handoff. Server rejects cycles and invalid scope. Drag is planning intent only, never approval, ownership acceptance or verified completion. |
| **Task detail / `TaskWorkspace`** | Goal, acceptance criteria, owner/participants, dependency versions, handoff history, artifacts, package/run lineage, decisions, effects and next permitted action | Scoped comments and revisions; explicit handoff offer/accept/decline/clarify. Block acceptance when recipient lacks necessary evidence; offer an access-request or authorized redacted derivative. |
| **My decisions / `ActionableDecisionInbox`** | Eligible requests by kind, required deadline/urgency, blocked downstream commitments, outcome-first summaries, revision, known risks and estimated review time with rationale | Open; personal snooze/dismiss; request changes; endorse; technical review; approve named effects; clarify; accept handoff. Only currently eligible actions are offered; server rechecks. |
| **Review workspace / `ReviewPackageView`** | Frozen revision, company/account/environment, host-owned full operation inventory, typed previews, performed and missing validations, material risks, exact remaining decision and side-by-side revision comparison | Explicit stored action reference and expected revision. No arbitrary prompt accepted as execution payload. Separate requester and privileged reviewer evidence projections. |
| **Execution history / `ExecutionHistory`** | Attempt and operation timeline, downstream principal, authorization lineage, accepted/applied/verified/settled distinctions, external receipt reference, independent readback and exceptions | Open source/native recovery links; request reconciliation; authorized compensation or cancellation through separate decisions. Queue completion is not business completion. |
| **Preparation workspace / `PreparationRunView`** | Captured inputs, extraction uncertainty, full-code artifact/diff, test results, resource/egress scope, package version and immutable outputs | Start bounded prepare/revision; inspect code; stop attempt. Domain reviewers see business diff first, technical reviewers can inspect implementation. Production secrets are absent. |
| **Registry and configuration / `CapabilityCatalog`** | Automation packages, templates, harness/connector/renderer versions, publisher/digest, certified operations, requested versus granted capabilities, effective composed configuration and readiness failures | Choose approved version, parameterize or submit candidate; privileged install/promotion separate. BYO requests do not self-install privileged code. |
| **Shared assistants / `AssistantDirectory` + `AssistantInvocation`** | Organization-discoverable assistant, purpose, current caller-scoped authority, accessible evidence and invocation history; autonomous service mandates distinctly labeled | Invoke within caller intersection; offer bounded task or proposal. Never expose another caller's memory/session. |
| **Steward suggestions / `BoardStewardSuggestions`** | Scoped draft intake, possible duplicates, missing acceptance criteria, stale-work explanations and proposed clarification | Accept/edit/reject suggestion through normal commands. Advisory by default; cannot become scheduler, approver or sole verifier. |
| **Knowledge / `KnowledgeReferences`** | Gontext citations, source and freshness, classification, provenance, supersession and availability; distinguish reusable knowledge from operational receipts | Authorized retrieval or reviewed publication request. Citation possession is not access permission. Unavailable required context blocks only dependent work. |
| **Improvement work / `ImprovementPortfolio`** | Recurring issues, scoped corrections, candidates, baseline/holdout/shadow evidence, review, rollout and delayed adverse outcomes | Propose change, adjudicate evidence, authorize release/rollback with distinct roles. Production can continue on a known safe version. |
| **Operations and governance / `OperationsHealth`** | Admission/backpressure, connector health, unknown effects, cleanup debt, audit, revocations, policy and release status, restricted analytics | Kill switch, reconcile, restore drill and authorized policy changes. Incidents do not flood personal decision inboxes unless a genuine human decision is needed. |

### Planning and board interaction rules

- A task distinguishes accountable human/position, executor, collaborators, awaited actor, reviewer and approver. A person may appear in several roles without those roles becoming interchangeable authority.
- A handoff offer is version-bound and has accept/decline/clarify/expiry. Accepted accountability transfer and executor reassignment are different commands. Preserve maximum-hop and dispute limits.
- Milestone completion requires its declared acceptance evidence and valid dependency state, not every child visually residing in a “Done” column. Forecast uncertainty and blocked dependencies are explicit; hidden dependencies get only an authorized “restricted dependency” indication where policy permits, not leaked titles.
- Drag-and-drop offers a preview of an allowed planning transition, then calls the same command as keyboard/menu alternatives. Protected transitions open explicit decision UI or reject. Never map dropping into “Approved” or “Done” to an authorization grant or verified receipt.
- Board filters, personal topics and saved views change navigation only. They do not change tenant, role, membership or gate eligibility.

## 3. Decision contract, revisions and visible state

### Stable `DecisionRequest` semantics

A durable request contains organization ID, canonical task ID, Review Package ID/revision/digest where applicable, canonical gate or handoff ID, decision kind, eligibility policy/required role, target person/group, deadline, creation event and expected request version. Kinds are `requester_endorsement`, `technical_review`, `effect_approval`, `clarification` and `handoff_acceptance`. Clarification/handoff requests without a material proposal bind their canonical target version rather than inventing a Review Package.

Lifecycle is **pending → resolved | superseded | expired | cancelled**. Each resolution records its typed response and actor. “Resolved” says this request was answered; a multi-person quorum gate may remain open. A new material proposal supersedes affected requests and creates new revision-bound requests without rewriting history. Canonical gate state is authoritative; the request routes attention to it, not a second approval engine.

Commands accept a server-owned action/request reference, expected request/target revision, idempotency key and typed decision input. Authentication establishes the actor; browser-supplied role or actor labels do not. Server checks current eligibility, canonical human separation of duties, expiry, proposal integrity, selected operation dependencies and quorum, then atomically records response, gate/task transition and any outbox/dispatch intent. It never accepts replacement official operations from the client. Duplicate delivery returns the committed result or a conflict, not another grant.

**Personal disposition is a separate record:** `(organization, human, request)` with unread/read, snoozed-until or dismissed. It does not change request lifecycle, gate deadline, task state or other people's inboxes. Required decisions resurface/escalate by policy. “Decline this proposal,” “cancel this gate” and “cancel shared task” are distinct authorized domain commands with reasons and effect-state checks. Removing a notification never invokes them.

### Outcome-first decision card

Always show outcome and why now; affected party; company, official account and test/production environment; exact material before/after; source freshness; validation performed and omitted; unresolved risks; and the specific decision still required. Keep recipients, BCC, amounts, currencies, units, attachments, sharing and permission changes visible in the host inventory even when evidence detail is collapsed. Show “preparation incomplete—clarification needed” when evidence is missing instead of inventing an approval-ready result.

Labels name the action: **Endorse revision and submit**, **Request changes**, **Approve inventory correction**, **Approve sending this email**, **Accept handoff**. Endorsement attests requester intent; technical review is evidence; eligible human approval authorizes an exact effect set; execution and verification happen afterward. Optional partial approval displays explicit operation IDs and prerequisite closure. Pagination or filters never define the approved subset implicitly.

### State labels are separate dimensions

These are proposed UI mappings; PD-00 must reconcile exact wire enums with the kernel contract before code generation. Do not collapse them into a universal status field.

| Dimension | User-facing labels / meaning |
|---|---|
| Work planning | Draft; Ready; Preparing; Blocked; Ready for decision; Running; Verifying; Done; Partial; Cancelled. Done requires task acceptance policy, not just an agent terminal string. |
| Dependency/access blocker | Waiting for dependency; Awaiting access; Needs clarification; Required context unavailable; Preconditions changed. Include responsible role and permitted recovery. |
| Decision request | Pending; Resolved; Superseded—review new revision; Expired; Cancelled. Quorum progress is a separate gate display. |
| Gate | Awaiting endorsement; Awaiting technical review; Awaiting authorized approval; Approved; Declined; Invalidated; Expired; Cancelled. Exact policy determines required stages. |
| Execution attempt | Eligible/queued; Starting; Running; Parked for decision; Stopping; Cleanup unconfirmed; Stopped; Failed. Show resource reservations/charges until cleanup is confirmed. |
| Effect | Prepared; Authorized; Submitted/accepted; Pending confirmation; Applied; Verified; Business-settled; Failed with no effect confirmed; Unknown; Compensated. Provider acceptance is not readback. |
| Aggregate effects | Not started; In progress; Verified; Partial; Unknown/reconciliation required. Show per-operation truth; a mixture cannot be painted uniformly green. |
| Connector operation maturity | Read-only; Prepare-preview; Governed-apply; Verified-apply; Unsupported. Certification is operation-specific and evidence-backed. |
| Local review draft | Saving; Saved to this revision; Save failed; Offline/unsaved; Conflict; Based on superseded revision. A draft never implies a submitted decision. |

Business correctness adjudication is separate from “Verified”: readback can establish that an incorrect inventory change happened exactly as requested. Disputed/reversed outcome observations remain attached to the original run.

### Attention continuity and revision-pinned drafts

1. Keep selected task/request identity fixed across sorting, new arrivals and late query responses. Announce new arrivals without navigating or stealing focus.
2. Pin the displayed proposal revision. A newer revision creates a banner, material diff and explicit **Review new revision** control. Old content stays inspectable but cannot resolve a superseded request.
3. Persist authorized drafts by organization, canonical human, request and base revision, with retention, encryption/access controls and optimistic concurrency. Multi-tab edits conflict visibly rather than last-write-wins. Do not store sensitive durable drafts in browser local storage by default.
4. Preserve focused control, scroll, selection and disclosure state using stable component/field IDs. Keep old feedback attached to its base revision. Deliberate rebasing copies text with provenance and requires checking changed facts; it is never automatic approval reuse.
5. A stale decision/conflict response preserves the draft and displays the mismatch. Never automatically retry a stale consequential command against the new revision. A network timeout after submission shows “checking decision result,” looks up the idempotent command outcome and avoids a new decision key.
6. On logout, company switch or revoked access, remove unauthorized material from visible caches; quarantine/restrict server drafts according to policy. Warn about unsaved local changes without redisclosing revoked content.

### Error, partial and degraded UI

- Distinguish empty authorized results, loading, unavailable projection, truncated/incomplete results and access denied. Never display unavailable data as zero or claim a complete operation inventory from a partial response.
- Pin visible content on transient network loss and label freshness. Disable consequential actions when required package/authority validation cannot be obtained. Offline actions cannot queue silent future approvals.
- Renderer failure falls back to host-readable typed fields. Unknown schema, missing material fields or preview/effect mismatch blocks approval; an attractive partial preview is not sufficient.
- Preserve successful independent operations when another fails, but block downstream dependencies. Unknown outcome exposes reconciliation owner, last observation and next safe action; “Retry” is unavailable until effect safety is determined.
- Access-restricted evidence is not automatically equivalent to missing evidence. The server must determine whether the current decision role has sufficient review visibility; otherwise route to an eligible reviewer or request access without leaking restricted facts.
- Show cancellation as requested/stopping until fenced execution and cleanup are confirmed. Already accepted remote operations may still settle; reconcile them. Do not show “nothing happened” merely because the user cancelled.

### Accessibility acceptance

Target WCAG 2.2 AA for the shipped screens, with automated checks plus manual keyboard and screen-reader verification. All board operations have menu/form alternatives; dialogs restore focus; status changes use restrained live regions; diff additions/deletions use text and symbols, not color alone. Label risk, environment and irreversible actions in text. Test logical heading/table structure, visible focus, zoom/reflow, 390px narrow layouts, reduced motion and long localized labels. Large operation lists need accessible pagination/virtualization and a complete host summary; no hidden approval through offscreen rows. Test revision banners and conflict recovery with assistive technology, not only the happy path.

## 4. Preview, preparation and configuration boundaries

**Automation Package** is reusable executable behavior; **Review Package** is this run's immutable concrete proposal; **Execution Receipt** is actual effect evidence. Certification of code does not approve every output.

Host-owned typed preview components initially include `EmailEnvelope`, `InventoryDelta`, `RecordDiff`, `EvidenceCitation`, `ValidationResult` and `UnknownEffects`; later add `LedgerLines`, `DocumentDiff` and `CodeDiff` as supported domains require. Version the schemas and renderer IDs. Server canonicalization binds normalized effect fields, account/environment, immutable attachments, input/resource versions, package/connector/renderer digests, expiry, operation dependencies and verification policy. Host checks every material operation has a faithful representation.

The host owns company/account/environment banners, full operation inventory, validation omissions, risk/fidelity warnings and decision controls. Typed previews express domain semantics rather than reproducing every ERP screen. Source and native-application links are exception/recovery paths, not an excuse to omit the proposed effect from review.

Optional rich plugins run on separate origins in restrictive sandboxes with minimal role-filtered data, no credentials, default-deny network, bounded host messaging and immutable version pins. They cannot overlay trusted controls, submit authorization independently or conceal required fields. A declarative adapter or MCP Apps-style interface may be evaluated, but protocol branding establishes neither implementation nor security. Unknown/disabled plugins fall back to host rendering; unsafe or lossy fallback blocks material approval.

Full-code preparation can acquire authorized inputs, run extraction and pure transformations, use local files/tests and generate reusable packages inside an isolated workspace. Reuse a certified package, then parameterize, extend, and only then generate new code. Code targets checkpoint/effect SDK interfaces; arbitrary script execution does not acquire replay safety. Production credentials stay in the governed connector/effect boundary. Preparation emits proposed operations, never directly applies them. Capture package/build/config/input versions and bounded resource budgets.

Registry entries distinguish requested capabilities from grants and install/configuration/execution/data scope. Templates are validated declarative data, not executable employee configuration. Show effective configuration and denied requests before launch. Harness selection is employee choice within organizational certification and ceilings; BYO submission enters quarantine, conformance and approval. Missing required capabilities fail admission with a diagnostic. Upgrades are staged/drained; parked runs remain pinned or undergo explicit migration/reprepare, not silent hot replacement. Native continuation is optional; portable task packets and fresh admitted attempts are the baseline.

## 5. First coherent slice: supplier mail to verified inventory and reply draft

The first integrated demonstration uses synthetic supplier mail, an explicit test inventory service and synthetic identities. The simulator is a test deliverable, not a representation of supplier infrastructure already available. Real integration selection and account consent occur only in PD-07.

| Step | Concrete product result | Required evidence / stopping condition |
|---|---|---|
| 1. Scoped intake | A mail event creates or links one canonical task, evidence attachments and a business-document identity | Durable inbox dedupe covers duplicate delivery; supplier/entity/document/revision identity handles reused document numbers. Source text is untrusted data, not instructions. |
| 2. Prepare locally | Approved harness/package extracts attachment data and obtains authorized inventory observations | Captured source bytes/digests and account binding; ambiguous units, missing pages or contradictory quantities become item exceptions. No official-write credentials in worker. |
| 3. Inventory preview | Immutable Review Package renders item, location, unit, observed/proposed quantity, reason, evidence and relevant target versions | Independent domain fixtures validate extraction/matching; all operation IDs and dependencies visible. Safe subsets only where invariants permit. |
| 4. Requester endorsement | Requester's inbox presents useful prepared work and named endorsement action | Attestation binds revision/digest; requester alone cannot dispatch a write. Changes request bounded reprepare and supersede affected requests. |
| 5. Authorized approval | Currently eligible approver sees the same effect revision and any permitted extra review evidence | Current role, separation-of-duties, expiry and revision tests pass atomically. Technical agent review remains labeled evidence. |
| 6. Park/resume and apply | Durable wait releases the dedicated worker; approval admits a fresh attempt; gateway applies only stored approved effects | Intent recorded before effect; current mandate/grants/fences, connector trust and target preconditions checked. True downstream principal recorded. |
| 7. Independent readback | Execution history distinguishes each applied, verified, pending or unknown item | Verifier reads exact external targets using connector-owned semantics; simulator supports timeout-after-commit, delayed visibility and partial failure. Never invent receipt data. |
| 8. Response draft | A host-rendered supplier reply cites confirmed results and explicitly identifies unresolved items | Success statements depend on verified inventory results. Sender, To/CC/BCC and attachment digests are explicit. Initial slice stops at a draft. |
| 9. Optional later send | A separate send Review Package and decision authorize final message content | Inventory approval does not authorize email. Separate send intent, receipt and readback/confirmation capability; provider limitations remain visible. |

**Integrated completion:** a reviewer can follow this chain across person, agent and project boards, inbox and history without duplicated tasks; restart the backend and worker at defined boundaries; see the same revision and decision survive; and obtain accurate per-operation outcomes. Include an ambiguity branch, request-changes branch, stale-approval race, duplicate-mail branch and unknown-effect branch. A screenshot of the happy path alone is insufficient.

## 6. Module responsibility and contract ownership

These are engineering/domain responsibilities to assign, not a staffing roster or fictional employees. A small team may combine implementation roles, while required independent domain/release review must remain genuinely independent. One named responsibility owns each contract/migration family; other modules consume it.

| Responsibility / module | Sole implementation ownership | Boundary and required collaborators |
|---|---|---|
| Product/domain acceptance | Workflow semantics, milestone evidence, correction labels, approved test corpus and pilot acceptance | Does not implement approval policy by UI convention. Reviews business correctness independently of package author. |
| Kernel contracts and authorization | Command/query/event schemas, tenant/actor binding, temporal eligibility, canonical task/handoff/gate/DecisionRequest transitions and grants | Owns contract version review and transactional boundaries; frontend and runtime cannot create parallel authorities. |
| Durable orchestration | River integration, outbox/inbox, admission/fairness, attempt identity, leases/fencing, park/resume and cleanup state | Uses kernel transitions; queue acknowledgement never sets verified task completion. |
| Effect gateway and connector SDK | Normalized operations, mandate-to-provider identity mapping, effect ledger, business uniqueness, preconditions and reconciliation contracts | Connector-specific code supplies verified capabilities; no raw provider credentials exported to preparation. |
| Connector implementation | Actual provider discovery, account binding, read/pagination fixtures, apply/readback behavior and operation certification evidence | Cannot label an unsupported operation verified-apply. Works with domain owner on semantics and gateway owner on recovery. |
| Preparation/runtime and registry | Sandbox execution, full-code SDK, harness drivers, manifests, package/template versions, conformance and portable handoffs | No policy editing or production installation from generated code; operations supplies compute isolation. |
| Frontend and accessibility | React shell, authorized projection clients, boards, review host, typed renderers, drafts and focus/error behavior | Generates clients from frozen contracts; host decision controls cannot delegate authority to plugins. |
| Knowledge and assistants | Gontext adapter, provenance publication, caller-scoped invocation, service mandates and advisory Steward | Uses kernel commands; separate caches/memories per caller/scope and bounded causal processing. |
| Improvement and release evaluation | Correction/outcome records, candidate clustering, protected holdouts, shadow evaluation and promotion evidence | Package author cannot alter independent verifier/holdouts/authority policy to pass the same release. |
| Quality/security and operations | Cross-module threat/race tests, browser accessibility checks, release evidence; secrets, deployment, restore, kill switches and incident procedures | Infrastructure access is an explicit trust assumption; production enablement requires domain, security and operational acceptance, not merely merged code. |

## 7. Dependency-ordered technical milestones

IDs below are stable planning references. Each milestone delivers executable evidence before downstream activation. Test fixtures, mock contracts and simulators are labeled as such; passing them is not provider certification. All coding work uses red → green → refactor: commit/review a failing acceptance or contract test first, implement the smallest real path, then refactor under passing tests. Start with real PostgreSQL integration tests for transactional claims; in-memory substitutes alone cannot prove races or durability.

### PD-00 — Contracts, threat model and domain fixtures

**Dependencies:** none. **Owner:** kernel contracts, with product/domain and quality/security review.

**Deliver:** versioned command/query/event schemas; request/gate/effect transition tables; Review Package and receipt schemas; canonical identifiers and revision/CAS rules; authorization matrix; data-retention classification; synthetic two-company hierarchy/matrix/cover-delegation fixtures; supplier document corpus and operation invariants; approved pilot SLO/measurement plan. Specify command idempotency and error envelope, query completeness, current authorization and event replay behavior before client generation.

**Accept:** independently reviewed executable schema/transition tests reject cross-company IDs, arbitrary effect payloads, endorsement-as-approval, cycles, invalid handoff acceptance and stale revisions. Human role fixtures include revoked membership, duplicate human accounts and private reviewer evidence. Architecture decision records label unresolved provider/Gontext/harness assumptions; no mock is presented as a live capability.

### PD-01 — Transactional kernel and River proof

**Dependencies:** PD-00. **Owner:** durable orchestration and kernel contracts.

**Deliver:** Go application skeleton, PostgreSQL migrations, authenticated command boundary, domain transaction plus outbox/transactional River enqueue, deterministic attempt launch, runner inspection protocol, durable gates and resource cleanup tracking. Add fair admission and bounded reservations, not one queue per feature.

**Accept:** kill/restart and concurrent integration tests prove atomic decision/gate/dispatch intent; duplicate delivery, lost launch response, cancellation-before-start and stale callback cannot create an unauthorized second attempt. Parked human waits retain no dedicated agent process/session/slot after confirmed cleanup. Disconnected cleanup stays visible/charged. Backpressure/fairness tests use agreed representative loads and report observations, not assumed benchmarks. Keep River unless this evidence demonstrates a material unmet requirement.

### PD-02 — Review packages and simulator effect boundary

**Dependencies:** PD-00; PD-01 for final integration. **Owner:** effect gateway/connector SDK, with domain acceptance.

**Deliver:** immutable canonical Review Package storage; effect intent/receipt ledger; test inventory/mail connector simulator; account/environment bindings; conditional updates, bounded idempotency retention and independent readback hooks; typed preview fixtures; per-item dependencies and explicit unknown-state handling.

**Accept:** tests fail first for wrong-account credentials, preview/payload disagreement, changed unit/attachment/recipient, remote target drift, timeout after commit, expired idempotency and partial dependent batches. Then prove expected denial/reconciliation with exact simulator readback. Business operation identity survives duplicate deliveries and package upgrades. No “success” terminal string can substitute for readback.

### PD-03 — Human review shell and canonical boards

**Dependencies:** PD-00; PD-01 and PD-02 for server-backed acceptance. **Owner:** frontend/accessibility, with kernel contracts.

**Deliver:** company selector; basic person/agent/project boards; task detail; actionable inbox; host-rendered EmailEnvelope/InventoryDelta/evidence; explicit endorsement/approval actions; history; revision-pinned drafts; error/partial states; accessible keyboard interaction. Synthetic data may accelerate component development but acceptance runs against Go/PostgreSQL.

**Accept:** browser tests prove one task across surfaces, revision races, old-draft preservation, multi-tab draft conflicts, no focus theft, no automatic stale replay, per-user dismissal, authorized cancellation and no authorization by drag. Hidden operations/recipients cannot be approved through pagination. Manual keyboard/screen-reader and narrow-layout checks accompany automated accessibility results. Direct unauthorized commands fail even if client controls are bypassed.

### PD-04 — Full-code preparation and first certified harness path

**Dependencies:** PD-00, PD-01, PD-02. **Owner:** preparation/runtime and registry.

**Deliver:** one pinned harness adapter on organization-controlled sandbox compute; executable package acquire/normalize/decide/prepare boundaries; saved input/checkpoint artifacts; package/config digests; bounded revision jobs; portable task packet; registry skeleton and one declarative template. Select the actual harness after inspecting its supported launch/stop/continuation behavior.

**Accept:** generated code can prepare a real simulator proposal and run tests, but cannot access production secrets, kernel DB keys, host sockets or ungranted egress. Kill/restart, stale run token, revoked capability, missing mandatory dependency and malicious document tests pass. Request-changes starts bounded fresh work; a parked wait requires no native in-memory continuation. Domain-owned tests catch a planted extraction defect even when author tests pass.

### PD-05 — Coherent sandbox journey

**Dependencies:** PD-01, PD-02, PD-03, PD-04. **Owner:** product/domain integration responsibility, with all slice owners.

**Deliver:** the complete Section 5 journey through response draft, documented runbook, seeded demonstration dataset and reproducible end-to-end suite. Include actual persistence and process lifecycle, not a frontend-only scripted demo.

**Accept:** every integrated completion condition in Section 5 passes. Endorsement alone never causes a write; approved revision dispatches exactly its stored simulator operation set; independent readback gates success wording; a separate send request is generated only when requested. Restart during each durable boundary, double-click, concurrent revision, delayed callback, partial apply and required-context outage preserve correct state. Demonstrate kill switch and native/manual recovery instructions. Record real test output and remaining known limitations.

### PD-06 — Organizational planning and handoff completeness

**Dependencies:** PD-01, PD-03; PD-05 before pilot release. **Owner:** kernel contracts and frontend, with domain planning review.

**Deliver:** full temporal hierarchy/matrix editor, per-person/per-agent boards, milestone/dependency planning, explicit accepted handoffs, leave/cover policy UI and authorized cross-company portfolio. Separate planning dates from observed execution.

**Accept:** synthetic multi-company scenarios prove no cross-company search/count/cache leakage; reporting relationships do not grant access; matrix membership and cover expire correctly. Dependency cycles reject transactionally; concurrent edits conflict; milestone evidence controls completion. Recipient without required evidence sees awaiting-access rather than an accepted unusable handoff. Drag, menu and keyboard paths have identical authority checks.

### PD-07 — Real provider discovery and read-only pilot

**Dependencies:** PD-05; PD-06 for organizational pilot enrollment. **Owner:** connector implementation and platform operations, with domain owner.

**Deliver:** inventory of actual mail/inventory providers, account consent and residency requirements; operation-level capability report; real read-only connector against selected permitted accounts; provider-version fixtures; native recovery links; scoped real preview workflow with all writes disabled. If no suitable provider/API exists, deliver a documented blocked integration or authorized import/export preview path—not fictional API code.

**Accept:** verify actual upstream principal/account binding, pagination completeness, attachment handling, source freshness, rate limits, schema drift and read permissions with recorded provider evidence. Produce reviewed proposals from authorized real inputs without external writes. Operational/domain reviewers adjudicate sample correctness and ambiguity handling. Data retention and access revocation tests pass. Read-only success does not certify apply/readback.

### PD-08 — Narrow, gated real writes

**Dependencies:** PD-07 and explicit domain/security/operations authorization. **Owner:** effect gateway and connector implementation.

**Deliver:** allowlisted operation/account/environment, finite pilot limits, actual provider apply/readback conformance, reconciliation/compensation runbooks, kill switch and operator training. Where available, test provider sandbox first; production probes require approved exact actions and a recovery plan. Optional supplier send is a separately certified operation and approval path, not mandatory for the initial draft-ending pilot.

**Accept:** authorized exact-revision write and exact-target readback succeed against the selected real provider; wrong-account, revoked approver, changed precondition and duplicate/unknown-effect handling are evidenced at the supported provider level. Missing CAS or idempotency guarantees are explicitly classified and may block write enablement. Independently review actual external receipts and business correctness. Run restore and incident drills before widening traffic. No standing mandate is inferred from repeated successful approvals.

### PD-09 — Registry, templates and materially different BYO harness

**Dependencies:** PD-04, PD-05. **Owner:** preparation/runtime and registry.

**Deliver:** capability catalog UI; quarantine/verify/approve/install/ready/active/drain/disable lifecycle; effective configuration inspection; versioned templates and scoped customization; second materially different harness; plugin SDK conformance and compatibility policy. Optional isolated view plugin may be introduced only after host fallback and messaging tests.

**Accept:** both harnesses execute the same portable bounded task contract with correct stop/restart/fencing behavior. Employee configuration cannot raise ceiling or replace auth/queue/kernel dependencies. Missing/extra capabilities are visible; increased privileges require new approval. Parked pinned runs survive staged upgrades or require explicit migration/reapproval. Malicious renderer tests cannot conceal host inventory, access credentials or authorize actions. Native continuation differences are documented rather than promised away.

### PD-10 — Gontext, shared organization assistants and advisory Steward

**Dependencies:** PD-06, PD-09; PD-00 scope contracts. **Owner:** knowledge and assistants.

**Deliver:** inspected/tested Gontext retrieval/publication adapter with provenance and supersession; shared assistant directory and isolated invocation contexts; autonomous service mandates with sponsors/budgets; durable inbox and bounded causal dedupe; advisory Board Steward suggestions. Production outcomes publish knowledge only through reviewed provenance rules.

**Accept:** cross-caller/company cache, memory and credential leak tests pass. Caller mode intersects current caller permissions, agent ceiling, delegation, purpose and organization policy; service mode uses explicit mandate without fictitious caller. Required context outage parks dependent work while independent pinned-input work continues under policy. Notification loops stop under fan-out/depth/cooldown/no-progress caps. Unplugging Steward does not interrupt committed work or scheduler correctness; its suggestions cannot approve effects.

### PD-11 — Governed improvement and adoption evidence

**Dependencies:** PD-05, PD-09, PD-10; PD-08 before evaluating real-write outcomes. **Owner:** improvement/release evaluation with independent domain review.

**Deliver:** separate PersonalPreference, Correction, OutcomeObservation and ImprovementCandidate records; narrow-by-default correction capture; asynchronous root-cause clustering and budgets; protected regression/holdout corpus; read-only shadow; release review, bounded rollout and rollback UI; privacy-governed effort/adoption dashboard.

**Accept:** planted defect → adjudicated correction → regression case → candidate → independent unseen holdout → shadow → authorized release is demonstrated. A candidate that skips difficult items or weakens a verifier fails. Preferences/feedback cannot change authority; supplier-specific rules do not generalize automatically. Regression-triggered rollback prevents future runs while already applied effects enter separate authorized reconciliation. Report all-input denominators and delayed correction windows; no employee leaderboard or invented savings claim.

### PD-12 — Operational release and controlled expansion

**Dependencies:** PD-06, PD-08, PD-09, PD-10, PD-11. **Owner:** quality/security and operations with product/domain release authority.

**Deliver:** release evidence bundle, support/onboarding/offboarding guides, retention/restore policy implementation, accessibility sign-off, measured capacity budgets, incident ownership and staged multi-company rollout. Chat decisions, reversible Steward actions and narrow standing mandates are optional follow-ons behind their own reviewed contracts; web decisions remain the baseline.

**Accept:** restore drill recovers canonical tasks, gates, package revisions and effect reconciliation evidence; offboarding revokes current access and admissions; kill switch fences new effects without false claims about remote cancellation. Each enabled connector/harness/version has conformance evidence. Pilot users complete realistic journeys and recovery tasks without operator impersonation. Unresolved high-risk gaps block rollout; full-target features cannot disappear under a “pilot complete” label.

## 8. Parallel execution without contract races

**Critical integration spine:** PD-00 → PD-01/PD-02/PD-04 plus PD-03 → PD-05 → PD-07 → PD-08. PD-06 completes organizational use before real pilot enrollment. PD-09 and later knowledge/improvement work can proceed independently of provider procurement once their listed prerequisites hold. PD-12 is the full-target release gate, not a synonym for the early slice.

- Freeze PD-00 schema baseline and canonical fixture IDs before parallel implementation. Assign one owner to each migration family and wire contract. Generated Go/TypeScript clients and shared conformance fixtures are build outputs, not hand-edited alternative definitions.
- After baseline, parallelize frontend typed components/projection adapters, kernel/orchestration, connector simulator/effect ledger and sandbox package execution behind those contracts. Frontend mocks implement the exact schema and include revision/error cases; they cannot invent new status semantics.
- Give each workstream disjoint module/file ownership and an integration branch cadence. Database migration numbering and aggregate transaction changes pass a single migration owner to avoid duplicate numbering and split atomicity. Review shared schema changes before independent implementation proceeds.
- A contract change request includes producer/consumer impact, compatibility period, fixture changes, authorization implications and migration/replay behavior. Bump version or coordinate one atomic change; do not let both branches “fix” the contract differently. Security invariants are never relaxed to make a consumer pass.
- Integrate thin end-to-end paths continuously. Each merge runs schema compatibility, tenant isolation, canonical transition and simulator smoke suites. Reserve integration work explicitly; parallel component completion is not PD-05 completion.
- Keep domain acceptance and independent verification separate from package authorship. Parallel reviewer agents may inspect bounded modules but cannot independently redefine policy, mutate shared contracts or approve their own release evidence.
- Provider discovery can begin during sandbox development as read-only research/consent coordination; enabling real input or credentials still waits for PD-07 controls. Lack of provider infrastructure does not justify a fake “live” demo.

## 9. Test matrix and release evidence

Tests are planned acceptance work, not claimed results of this document. Every milestone evidence bundle records source/build/config versions, environment, input corpus provenance, commands/results, rejected cases, known limitations and accountable reviewer.

| Suite | Required coverage | First gate |
|---|---|---|
| Pure domain/property tests | Transition legality, immutable revision binding, dependency acyclicity, quorum/request separation, business operation uniqueness | PD-00/PD-01 |
| PostgreSQL concurrency tests | Concurrent approve/update/revoke, idempotent command replay, atomic response/gate/outbox, two-company constraints | PD-01 |
| Runtime fault injection | Lost launch response, duplicate River delivery, kill at checkpoint, stale callback, stopped/unknown cleanup, fairness and budget exhaustion | PD-01/PD-04 |
| Connector/effect tests | Wrong company, changed target/dependency, reused document identity, pagination omissions, timeout after commit, delayed readback, expired remote idempotency and partial batches | PD-02; real conformance PD-07/PD-08 |
| Preview/browser tests | Material field coverage, BCC/attachment bytes, typed fallback, pinned revision, focus/selection/drafts, personal dismissal, no drag authorization, restricted evidence | PD-03/PD-05 |
| Security and isolation | Source prompt injection, malicious preparation/package/plugin, privilege escalation, actor forgery, cross-caller memory/cache leak, untrusted success report | PD-04/PD-09/PD-10 |
| Human/domain evaluation | Contradictory documents, unit ambiguity, missing pages, incorrect-but-applied effect, explicit reviewer comprehension and recovery tasks | PD-05/PD-07 |
| Improvement evaluation | Protected holdouts, no skipped-input gaming, scoped feedback, candidate spend caps, shadow without writes, promotion separation and post-release delayed disputes | PD-11 |
| Operations/accessibility | Restore, revocation, kill switch, readback outage, connector security revocation, keyboard/screen-reader/zoom/narrow-screen/reduced-motion | PD-03 onward; PD-12 sign-off |

Use deterministic simulation for rare fault cases and actual provider evidence for provider-specific guarantees. A mock ETag test cannot prove a provider supports conditional writes; a real GET cannot prove write safety; a receipt cannot prove semantic correctness. Preserve these distinctions in release notes and UI maturity labels.

## 10. Adoption, coordination effort and privacy

Start with workflow cohorts and explicit consent/purpose rules, not activity scores. Measure adoption as eligible users/workflows onboarded, recurring voluntary use, completion of decision and recovery tasks, accessibility/usability friction, abandoned items and reported trust/confusion. A login or click is not an accepted outcome.

Measure coordination effort by workflow: clarification rounds, handoff acceptance delay, access-block duration, re-review after revision, approval/external/queue wait, time to find supporting evidence, duplicate requests, escalations, prepared-work WIP and reviewer queue age. Separate elapsed waiting from human handling effort; do not infer attention from tab focus. Estimated review time is labeled, bounded and calibrated by workflow, not a human performance rating.

Net-benefit evaluation includes specification, package/code review, domain approval, exception handling, repair, maintenance, reconciliation, incident response and governance. Compare matched baseline/pilot cohorts with case mix and sample limitations disclosed. Report human minutes per adjudicated acceptable outcome, execution and learning spend, false matches/omissions, rework and delayed corrections. Denominator is every received item, including skipped, quarantined and abandoned cases; audit samples of apparently successful items as well as exceptions. Agree acceptable risk and observation windows with the domain owner rather than invent universal savings targets.

Privacy controls precede analytics: aggregate defaults, documented purpose and retention, minimum cohort/suppression policy, role-filtered access, restricted exports, audit and employee contest/correction paths. Individual drill-down requires a justified operational purpose and explicit policy; manager status alone is insufficient. Avoid keystroke/screen surveillance, employee productivity scores and leaderboards. Optional self-reported handling-time samples must be labeled uncertain and collected under the approved policy. Keep sensitive business evidence out of analytics payloads unless necessary and authorized.

## 11. Final delivery boundary

The early deliverable is a reproducibly exercised sandbox supplier journey with real domain persistence, bounded preparation, durable human gates, exact-effect simulator execution and independent readback, ending in a truthful reply draft. The next deliverable is evidence from a selected real read-only provider; only then do narrowly authorized writes become eligible.

The full release additionally requires organizational/matrix planning, per-person/per-agent work, accepted handoffs, registry/templates/BYO harnesses, caller-scoped assistants, advisory Steward, Gontext and governed improvement with privacy-respecting adoption evidence. Each is assigned a delivery gate above. This chapter is the implementation specification for those outcomes, not evidence that they have already been built.

