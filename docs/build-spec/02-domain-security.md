# 02 — Domain model, authority, and transaction security

**Status: proposed implementation design; no implementation, deployment, provider conformance, or runtime qualification is claimed.** The baseline is a separate OSS organizational workforce product: Go modular monolith, PostgreSQL, River OSS dispatch, React/TypeScript web, isolated preparation runners and effect executor, and Gontext as a separate knowledge plane. Initial runtime qualification must validate this baseline; this chapter does not reopen language selection.

## 1. Normative scope and source precedence

This chapter translates the following supplied research into proposed build contracts:

- **S1:** `../research-org-workforce-plane-plan-v2.md`: trusted kernel, temporal relationships, accepted accountability, dispatch boundaries, handoffs, organizational agents.
- **S2:** `../research-workforce-unified-review-v2.md`: immutable review, actionable DecisionRequest, independent authorities and review UX.
- **S3:** `../research-workforce-executable-work-packages.md`: reusable automation, staged execution, evidence and checkpoints.
- **S4:** `../research-workforce-governed-action-gateway.md`: semantic operation classification and credential-separated gateway.
- **S5:** `../research-unified-review-security-stress-test.md`: identity, preview, revocation, external concurrency, idempotency and uncertainty requirements.

The explicit build decisions override conflicting source wording. In particular, S3's `verified_done` is **not a task status**. A task reaches `done` only with required verified evidence. Run success, queue completion, approval, external acceptance, and task completion are distinct facts. Gate states are exactly `pending/approved/rejected/expired/cancelled/superseded`; DecisionRequest states are exactly `pending/resolved/expired/cancelled/superseded`.

“Must” specifies an implementation acceptance requirement, not existing behavior. Database examples are schema patterns, not applied migrations. All identifiers below are opaque server-generated UUIDs unless explicitly described otherwise.

## 2. Bounded contexts and vocabulary

| Context / Go module | Owns authoritative writes | Does not own |
|---|---|---|
| `identity` | Organizations, principals, membership, authentication bindings, canonical humans | Business approval or upstream provider identity semantics |
| `organization` | Units, positions, typed temporal relationships, role acceptance | Implicit manager access to confidential work |
| `authorization` | Grants, delegations, mandates, deny state, policy versions, authorization decisions | Agent-generated requests as grants |
| `work` | Projects, milestones, tasks, prerequisites, accountability, handoffs | Queue/job status as task truth |
| `review` | Proposal lineage, immutable ReviewPackage, gates, attestations, DecisionRequest | General production write credentials |
| `automation` | Immutable AutomationPackage versions and separate certification decisions | Self-certification by generated tests |
| `execution` | Run attempts, checkpoints, admission references, effect intent and receipts | Global atomicity across external systems |
| `evidence` | Artifact metadata, verification, corrections, retention and legal hold | Gontext knowledge tables |
| `audit` | Versioned events, command receipts, transactional outbox/inbox | Unrestricted telemetry export |

Modules call typed application services inside the monolith; they do not update another module's tables directly. A command transaction may compose services using the same PostgreSQL transaction. No ordinary plugin receives database credentials. River is a delivery mechanism below these services, not a second state machine.

**Canonical terms:** an organization is a tenant, not a parent company with automatic child access. A principal is a human, agent, or service identity; an agent definition/package is not itself a principal. A canonical human groups verified login aliases for separation of duties. Accountability is an accepted obligation; execution assignment is who performs work. A delegation narrows existing authority; a mandate explicitly authorizes a service. A Gate is the authoritative decision aggregate; a DecisionRequest is an actionable projection for a particular recipient. An attestation is one authenticated response, not necessarily quorum. A ReviewPackage is one immutable concrete proposal revision; an AutomationPackage is reusable executable behavior. An effect is a stable business operation; a receipt records an observation/attempt, not permission. A prerequisite is a condition on other work/evidence, not containment.

## 3. Storage conventions and tenant integrity

### 3.1 Common fields and types

Every tenant-owned table has `org_id uuid NOT NULL` and a composite primary key `(org_id, id)`. Mutable aggregate roots additionally have `version bigint NOT NULL CHECK (version > 0)`, `created_at timestamptz NOT NULL`, `updated_at timestamptz NOT NULL`, `created_by uuid NOT NULL`. Immutable child facts have `recorded_at timestamptz NOT NULL` and authenticated attribution instead of mutable `updated_at`. All referenced tenant objects use composite foreign keys, including actors, attachments, audit subjects, parent tasks, dependency endpoints, provider connections and outbox subjects.

- `timestamptz` stores instants; API uses RFC 3339 UTC strings. Business timezone is a separate IANA name. Evaluate deadlines using database clock after acquiring locks; transaction-start `now()` must not make a long-waiting transaction treat expired approval as current.
- Money is `{amount: decimal string, currency: ISO code}`; database `numeric(38,12)` with operation-specific scale checks. Quantities use exact numeric plus an explicit unit. No floating point for approved material values.
- Digests are `bytea CHECK(octet_length(digest)=32)` with a separate algorithm/schema field; API encodes lowercase hexadecimal. Canonicalization version is mandatory. Canonical JSON rejects duplicate keys, unknown fields, non-finite numbers and ambiguous representations; material decimal values are normalized strings.
- States and kinds use text plus named CHECK constraints, maintained through reviewed migrations. `jsonb` is limited to validated versioned payloads, not unconstrained authority rules.
- `valid_during tstzrange` uses `[start,end)`, nonempty, finite start, optional unbounded end. `recorded_at` is distinct from effective business time.
- Sensitive text lives in classified encrypted payload/artifact storage where possible. Stable IDs, hashes and timestamps can also be personal or confidential data; they are not automatically public.

Representative constraint pattern:

```sql
CREATE TABLE task (
  org_id uuid NOT NULL REFERENCES organization(id),
  id uuid NOT NULL,
  project_id uuid NOT NULL,
  parent_task_id uuid,
  status text NOT NULL CHECK (status IN
    ('draft','ready','in_progress','blocked','in_review','done','cancelled')),
  version bigint NOT NULL CHECK (version > 0),
  PRIMARY KEY (org_id, id),
  FOREIGN KEY (org_id, project_id) REFERENCES project(org_id, id),
  FOREIGN KEY (org_id, parent_task_id) REFERENCES task(org_id, id),
  CHECK (parent_task_id IS NULL OR parent_task_id <> id)
);
```

Additional task fields are specified below; this excerpt is not a complete migration. Use a `work_resource(org_id,id,kind)` registry plus matching subtype foreign keys for contextual grants and audit subjects. Registry `kind` is checked against each subtype; arbitrary `(type,id)` JSON references cannot bypass referential integrity. Project-local containment requires an additional unique `(org_id,project_id,id)` and corresponding composite FK. Do not infer tenant identity from UUID uniqueness.

### 3.2 RLS and connection-pool trust

Enable and FORCE ROW LEVEL SECURITY on tenant tables. Runtime database roles are non-owner, `NOSUPERUSER`, `NOBYPASSRLS`; migration/backup roles are separately controlled trusted operators. Policies require both `USING` and `WITH CHECK`. For example:

```sql
CREATE POLICY tenant_isolation ON task
  USING (org_id = nullif(current_setting('app.org_id', true), '')::uuid)
  WITH CHECK (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
ALTER TABLE task ENABLE ROW LEVEL SECURITY;
ALTER TABLE task FORCE ROW LEVEL SECURITY;
```

Every request/job transaction starts by authenticating and binding the verified tenant, then uses `SELECT set_config('app.org_id', $1, true)` and an equivalent actor binding on that transaction's connection. Missing context sees no rows and cannot write. Never use session-level SET as the isolation boundary. Transaction pooling is permitted only when all protected reads/writes occur inside the same explicit transaction; do not release/reacquire a connection between SET LOCAL and the query. Rollback paths, canceled queries, prepared statements, job retries and alternating-tenant reuse require tests. RLS should use stable row values/current context, not user-supplied SQL fragments.

**Trust limitation:** a custom PostgreSQL setting is not a cryptographic tenant credential. A compromised runtime DB role capable of arbitrary SQL can set it to another tenant. RLS protects against accidental missing tenant predicates within a trusted kernel; it does not make arbitrary SQL from plugins safe. Application authorization still enforces resource visibility within a tenant. Prepared query-only repository methods, parameter binding, restricted DB roles, no plugin SQL, and no DB access from runners are required. SECURITY DEFINER functions are exceptional: fixed `search_path`, schema-qualified objects, restricted EXECUTE, no caller-controlled identifiers, explicit tenant checks and independent review. Views must preserve intended invoker/RLS behavior; qualify the deployed PostgreSQL version rather than assume view defaults.

River's internal scheduler may require cross-tenant queue access. Keep queue metadata minimal and in a separately privileged role/schema; job arguments contain opaque dispatch IDs and tenant routing IDs, no evidence, prompts or secrets. Domain handlers rebind tenant context and load canonical dispatch records. Scheduler privileges do not grant unrestricted domain reads. Maintenance/export paths use explicit tenant enumeration and audited operator authority, not request-controlled RLS bypass. Foreign-key/unique failures are mapped to non-leaking API errors: database referential checks are not a user-facing existence oracle.

## 4. Identity and organizational model

Notation: `?` means nullable; common fields from §3 are implicit. `FK` always means same-tenant composite FK unless specifically described as global identity storage. Unless a table explicitly overrides them, `*_id`/`*_by` fields are UUID foreign keys, `*_at`/`*_until`/deadlines are `timestamptz`, `*_version`/`*_epoch`/generation/counters are nonnegative `bigint` (aggregate versions start at one), `*_digest`/fingerprints are 32-byte `bytea`, named states/kinds are checked `text`, labels/opaque provider references are bounded `text`, and structured manifests/specifications/policies are schema-versioned `jsonb` or artifact FKs as named. Set-valued authority references (source grants, capabilities, correction sources) use normalized join tables with composite FKs rather than unchecked UUID arrays. Unspecified optionality means NOT NULL; fields conditional on lifecycle state have explicit CHECK constraints. External provider IDs remain exact opaque text, never normalized to email-like or numeric identifiers.

### 4.1 Identity records

| Table | Required fields beyond common fields | Constraints / behavior |
|---|---|---|
| `organization` | `id`, `slug text`, `display_name text`, `status active/suspended/closing/closed`, `default_timezone text`, `policy_version bigint`, `authz_epoch bigint`, `retention_policy_id uuid?` | Global organization root uses PK `id`; slug unique but not an authorization namespace. No parent-org inheritance. Suspension blocks new work/effects; audited reconciliation may continue. |
| `canonical_human` | `id`, `status active/disabled/erasure_pending`, `identity_version bigint` | Minimal global restricted identity registry, not tenant-searchable directory. Verified identity-link service alone reads/writes it. |
| `auth_identity` | `id`, `issuer text`, `subject text`, `principal_type human/agent/service`, `canonical_human_id uuid?`, `provider_tenant text?`, `disabled_at timestamptz?` | Unique provider-specific immutable identity key; namespace includes issuer/provider tenant where required. Never merge on email/name. Human type requires canonical human; workload types cannot borrow it. |
| `principal` | `kind human/agent/service`, `canonical_human_id uuid?`, `display_label text`, `status active/suspended/retired`, `authz_epoch bigint`, `sponsor_principal_id uuid?`, `agent_definition_id uuid?` | Human iff canonical human is set; unique `(org_id,canonical_human_id)` for human principals. Agent/service has sponsor and separate workload auth binding. Global FK to canonical human is explicit and restricted. |
| `principal_auth_binding` | `principal_id`, `auth_identity_id`, `valid_during`, `revoked_at?` | Controlled global identity mapping with tenant-owned binding; type compatibility enforced. No client-supplied actor selection. |
| `membership` | `principal_id`, `status invited/active/suspended/ended`, `valid_during`, `accepted_at?`, `ended_reason_code?` | At most one effective membership interval per principal at an instant; acceptance and active principal required for normal commands. Membership alone grants no business role. |
| `agent_definition` | `name`, `template_digest`, `harness_adapter_id`, `approved_config_digest`, `ceiling_grant_id`, `lifecycle draft/approved/active/draining/disabled` | Definition reuse does not merge invocation identity, memory, permissions, or workspaces. |
| `workload_binding` | `principal_id`, `issuer`, `subject`, `audience`, `key_reference`, `status`, `expires_at` | Authenticates runtime/service separately; no secret inline. Bind issued run credentials to attempt, org, purpose and audience. |

The canonical-human registry is a platform trust dependency. Aliases count as the same person; unverified aliases cannot be used for independent quorum. Linking/unlinking humans requires verified identity evidence and independent privileged review, increments identity versions and invalidates affected eligibility caches. On discovering duplicate humans after approval, block undispatched effects and open an exception; preserve historical attestations. Do not expose whether an email or human belongs to another company.

### 4.2 Temporal structure, roles and delegation

| Table | Fields | Enforcement |
|---|---|---|
| `org_unit` | `name`, `kind department/team/division`, `parent_unit_id?`, `status active/archived` | Same-tenant tree, cycle-free; unit structure conveys no access by itself. |
| `position` | `unit_id`, `title`, `position_code`, `status open/filled/closed` | Unique `(org_id,position_code)`; occupancy is a temporal relationship, not a hardcoded employee column. |
| `org_relationship` | `subject_resource_id`, `object_resource_id`, `kind reports_to/member_of/occupies/covers_for/project_member`, `valid_during`, `asserted_by`, `accepted_at?`, `ended_at?`, `source_record_id?`, `supersedes_id?` | Kind defines permitted endpoint types. `reports_to` human/agent principal→principal; `member_of` principal→unit; `occupies` principal→position; `covers_for` principal→principal; `project_member` principal→project. Reporting edges cycle-free at every overlapping effective interval. Matrix memberships allowed. Cover relationship itself grants nothing. |
| `role_definition` | `name`, `scope_kind`, `policy_digest`, `human_only bool`, `requires_acceptance bool`, `risk_class` | Versioned policy artifact. Role label does not contain implicit executable authority. |
| `role_assignment` | `principal_id`, `role_definition_id`, `scope_resource_id`, `state offered/accepted/declined/revoked/expired`, `valid_during`, `offered_by`, `accepted_by?`, `accepted_at?`, `revoked_at?` | Self-acceptance by assignee required for accepted obligations; authorized administrator offers role. Offering is not acceptance or active grant. For nonhumans, sponsor accepts an explicit service/agent mandate, not a human-only role. |
| `capability_grant` | `grantee_id`, `capability text`, `scope_resource_id`, `resource_filter_schema`, `resource_filter jsonb`, `purpose text`, `classification_ceiling`, `valid_during`, `source_role_assignment_id?`, `state active/revoked`, `grantor_id`, `grant_version` | Allowlisted declarative filters, no arbitrary SQL; permissions require explicit approved capability expansion. Deny rules override. |
| `delegation` | `delegator_id`, `delegate_principal_id`, `parent_delegation_id?`, `task_id?`, `scope_resource_id`, `capability_set`, `purpose`, `valid_during`, `max_depth smallint`, `remaining_depth smallint`, `state active/revoked/expired`, `source_grant_ids`, `ceiling_digest` | Child scope/time/capabilities/budget are subsets of parent and live delegator authority. Cycles forbidden; no transfer of nondelegable human decision capabilities. No renewal through a revoked ancestor. |
| `service_mandate` | `service_principal_id`, `sponsor_id`, `scope_resource_id`, `purpose`, `allowed_operations`, `account_binding_id`, `budget_policy_id`, `valid_during`, `state proposed/approved/revoked/expired`, `approval_gate_id?` | Explicit organizational authority for autonomous work or effect execution; not fabricated on-behalf-of identity. Sponsor/mandate liveness checked under policy. |
| `authz_guard` | `scope_kind`, `scope_id`, `epoch bigint`, `revoked_at?`, `reason_code?` | Materialized serialization rows for org, principal, grant, delegation, mandate and integration release; unique `(org_id,scope_kind,scope_id)`. |

Use GiST exclusion constraints with `btree_gist` (qualification required) for mutually exclusive interval assignments, such as one position occupant or one accountable person over a given interval. Reject overlaps with `&&`; preserve ended rows. Cross-row cycle/subset checks run in commands under graph/authorization locks; CHECK constraints alone cannot enforce them. Backdated HR correction records an amendment with both effective time and recorded time. It never silently rewrites the authority used for a past dispatch.

## 5. Work, accountability, prerequisites and handoffs

### 5.1 Work records

| Table | Fields | Constraints / meaning |
|---|---|---|
| `project` | `name`, `description_artifact_id?`, `status draft/active/paused/completed/cancelled/archived`, `visibility_policy_id`, `start_at?`, `target_at?`, `completion_policy_digest` | Project membership and manager relationship are inputs to explicit policy, not blanket read grants. |
| `milestone` | `project_id`, `title`, `status planned/active/achieved/cancelled`, `target_at?`, `acceptance_spec_id` | Achievement requires its required task/evidence predicates. Date does not imply achievement. |
| `task` | `project_id`, `milestone_id?`, `parent_task_id?`, `title`, `description_artifact_id?`, `status`, `priority smallint`, `due_at?`, `acceptance_spec_id`, `current_proposal_id?`, `execution_epoch bigint`, `graph_version bigint`, `block_reason_code?`, `closed_at?` | `status` values in §3. Parent and milestone in same project. Priority cannot override authorization or prerequisite checks. Current proposal pointer never mutates immutable content. |
| `task_participant` | `task_id`, `principal_id`, `kind requester/contributor/reviewer/observer/executor`, `valid_during`, `assignment_source` | Multiple participants allowed; participation is not accountability or general task read access. Executor is a separate assignment from accountable person. |
| `accountability_assignment` | `task_id`, `principal_id`, `state offered/accepted/declined/revoked/expired/ended`, `valid_during`, `offered_by`, `accepted_by?`, `accepted_at?`, `handoff_id?` | Exactly one accepted effective accountable human for actionable work in the baseline. Agent/service executes but has named human accountability. Draft/import repair may be unassigned and blocked. |
| `acceptance_spec` | `task_id`, `revision int`, `canonical_spec jsonb`, `digest`, `created_by`, `created_at` | Immutable, unique `(org_id,task_id,revision)`. Required criteria have stable criterion IDs, verifier class, evidence type, freshness and waiver policy. Changing criteria is a governed task revision, never retroactive silent relaxation. |
| `task_prerequisite` | `dependent_task_id`, `prerequisite_task_id?`, `required_evidence_id?`, `kind task_done/evidence_verified/gate_approved`, `gate_id?`, `required_version?`, `freshness_seconds?`, `state active/removed`, `created_by` | Exactly one typed target according to kind, no self-edge, unique active logical edge. Independent from task parent/child containment. Required verification/approval remains current at use, not merely at edge creation. |
| `task_blocker` | `task_id`, `kind prerequisite/decision/access/external/verification/exception`, `target_resource_id?`, `opened_at`, `resolved_at?`, `visibility_policy_id` | Multiple simultaneous blockers; one label on a task is not the underlying truth. Hidden blockers expose only policy-permitted generic explanation. |
| `board_view` | `owner_principal_id?`, `scope_resource_id`, `filter_schema`, `filter_json`, `presentation_preferences` | Projection configuration only. Same canonical task appears across boards; no cloned task truth. |

Task graph changes lock the tenant's `work_graph_guard` row initially, providing a simple correct baseline for cross-project prerequisite cycles. Cycle detection considers all active prerequisite edges; containment is checked separately. Later sharding this guard requires a proved locking protocol. Parent completion policy explicitly lists child requirements; containment alone neither schedules children nor makes parent completion automatic.

### 5.2 Handoff records

`handoff` fields: `task_id`, `offered_task_version bigint`, `from_accountability_id`, `to_principal_id`, `kind accountability/execution/both`, `state offered/awaiting_access/accepted/declined/clarification_requested/expired/cancelled/superseded`, `packet_artifact_id`, `packet_digest`, `required_evidence_manifest_id`, `expires_at`, `hop_count smallint`, `max_hops smallint`, `offered_by`, `accepted_by?`, `accepted_at?`, `gate_id`, `dispute_case_id?`. Only one live accountability-transfer offer per task is permitted using a partial unique index over live states. All handoffs have a canonical Gate, including acceptance-only human work without a ReviewPackage.

`handoff_packet` is an immutable classified artifact schema containing goal, criteria version, observed task version, portable checkpoint, verified outputs, rejected approaches, open questions, authorized citations, prerequisite versions, next action and native-state references if any. It contains no credential or assumed permission grant. Recipient evidence-read capability must be checked before acceptance; when insufficient, use `awaiting_access` and create a separate access request or authorized redacted derivative. A redacted derivative must meet the acceptance information policy; a citation alone is not access.

Accepting accountability atomically ends the former assignment and creates the accepted successor. Until that commit the former owner remains accountable; an offer does not strand work. Execution-only transfer does not change accountability. Changing executor fences obsolete run authority and schedules a fresh attempt only after admission; it does not transfer source tokens or claim cross-harness instruction-pointer recovery. The acceptance command binds the current task version; material edits supersede the offer. Clarification records a question and keeps the offer nonaccepted; expiry still applies. Each hop retains lineage and policy-bound hop limit, preventing endless delegation loops.

## 6. Immutable packages, canonical gates and decision projections

### 6.1 AutomationPackage

`automation_package` fields: `family_id uuid`, `release text`, `schema_version int`, `source_artifact_id`, `source_digest`, `build_artifact_id`, `build_digest`, `dependency_lock_digest`, `provenance_artifact_id`, `input_schema jsonb`, `output_schema jsonb`, `checkpoint_schema jsonb`, `entrypoints jsonb`, `requested_capabilities jsonb`, `egress_manifest jsonb`, `resource_limits jsonb`, `retry_policy jsonb`, `verification_contract jsonb`, `publisher_principal_id`, `maintainer_principal_id`, `created_at`, `manifest_digest`. Unique `(org_id,family_id,release)`; exact release content is insert-only.

Certification is separate: `package_certification(package_id, policy_version, reviewer_id, evidence_manifest_id, status pending/certified/rejected/revoked, decided_at, expires_at?, revocation_reason?)`. Revoking certification does not edit package bytes. Publisher artifacts may be globally distributed, but each tenant has its own package installation/trust decision and grant ceiling. Requested capabilities never become permissions automatically. Author tests are labeled author evidence; independent certification/verification has distinct provenance.

### 6.2 ReviewPackage and operation binding

`proposal` is a mutable aggregate: `task_id`, `head_revision int`, `head_package_id`, `version`. `review_package` is insert-only with:

| Field group | Required fields |
|---|---|
| Identity and lineage | `proposal_id`, `revision int > 0`, `supersedes_package_id?`, `task_id`, `task_version`, `acceptance_spec_id`, `schema_version`, `canonicalization_version`, `canonical_bytes_artifact_id`, `digest`, `created_at`, `expires_at` |
| Authority context | `requester_principal_id`, `preparer_principal_id`, `preparation_invocation_id`, `delegation_id?`, `service_mandate_id?`, `prepared_policy_version`, `classification`, `viewer_policy_id` |
| Implementation pins | `automation_package_id`, `automation_digest`, `connector_release_id`, `connector_digest`, `renderer_schema_version`, `renderer_digest?`, `validator_release_id`, `provider_api_version?` |
| Business scope | `objective`, `account_binding_id`, `environment production/test`, `acceptance_digest`, `operation_manifest_digest`, `limits jsonb`, `known_side_effects jsonb`, `bounded_unknowns jsonb` |
| Evidence and recovery | `evidence_manifest_id`, `validation_manifest_id`, `required_decision_policy_id`, `verification_policy_digest`, `reconciliation_policy_digest`, `compensation_policy_digest?` |

Unique `(org_id,proposal_id,revision)` and `(org_id,id,digest)` support exact approval FK binding. Supersession must remain within the same proposal/task and advance revision. Store annotations and display preferences elsewhere; immutable means ordinary UPDATE/DELETE is denied, not exemption from lawful deletion procedures in §12.

`review_operation` fields: `review_package_id`, `operation_id uuid`, `business_operation_id uuid`, `semantic_kind`, `account_binding_id`, `target_type`, `target_id text?`, `normalized_payload jsonb`, `payload_digest`, `explicit_defaults jsonb`, `before_snapshot_artifact_id?`, `expected_after jsonb`, `source_preconditions jsonb`, `target_preconditions jsonb`, `dependency_preconditions jsonb`, `idempotency_class`, `verification_spec jsonb`, `compensation_spec jsonb?`. Unique `(org_id,review_package_id,operation_id)`. Operation dependency rows reference both operations within the same package; graph is acyclic. The operation catalog specifies required recipients/BCC/sender, destinations, currency/units/rounding, sharing, attachments pinned by byte digest, provider triggers and account/mode. Unknown material fields or unresolved destinations block effect approval.

`business_operation` establishes durable uniqueness independently of proposal IDs: `account_binding_id`, `operation_family`, `business_key text`, `state reserved/dispatch_started/concluded/tombstoned`, `first_proposal_id`, `retention_until`. Unique `(org_id,account_binding_id,operation_family,business_key)`. The reviewed connector defines business-key semantics (for example invoice settlement intent plus installment identity), not a browser-generated random key. A revision may reuse the same business operation if it has not dispatched; once dispatch has started, its payload is fixed and changed intent requires explicit resolution/recovery, not overwriting the fingerprint. Tombstones prevent replay throughout the approved business/recovery retention horizon.

### 6.3 Gate, attestations and DecisionRequest

| Table | Fields and constraints |
|---|---|
| `gate` | `task_id`, `kind requester_endorsement/technical_review/effect_approval/clarification/handoff_acceptance`, `review_package_id?`, `review_revision?`, `review_digest?`, `handoff_id?`, `state`, `policy_version`, `eligibility_policy_id`, `quorum_required smallint > 0`, `distinct_requester_required bool`, `expires_at`, `resolved_at?`, `outcome_reason?`, `supersedes_gate_id?`. Effect/endorsement gates require exact ReviewPackage binding; handoff gate requires handoff; clarification may have neither. Policy has role slots and rejection semantics; baseline any eligible rejection terminates gate. |
| `gate_operation_selection` | `gate_id`, `review_package_id`, `operation_id`; FK ensures selected operation belongs to the bound package. Immutable selected set is prerequisite-closed and has a digest in the gate. Partial approval uses a new explicit selection gate; invisible pagination is never selection. |
| `attestation` | `gate_id`, `decision_request_id`, `actor_principal_id`, `canonical_human_id?`, `canonical_identity_version?`, `decision endorse/approve/reject/clarify/accept/decline`, `bound_package_digest?`, `bound_selection_digest?`, `policy_version`, `role_assignment_id?`, `auth_assurance`, `challenge_id`, `comment_artifact_id?`, `recorded_at`, `command_id`. Insert-only; unique `(org_id,gate_id,canonical_human_id)` for human-counted decisions; technical agent findings live in evidence, not human approval rows. |
| `decision_request` | `gate_id NOT NULL`, `task_id`, `review_package_id?`, `review_revision?`, `review_digest?`, `kind`, `recipient_principal_id`, `eligibility_policy_id`, `requested_role_id?`, `state`, `deadline`, `creation_event_id`, `resolved_attestation_id?`, `resolved_at?`, `supersedes_request_id?`, `version`. Unique live `(org_id,gate_id,recipient_principal_id,kind)`; references mirror Gate binding, verified transactionally. |
| `decision_audience` | `gate_id`, `role_definition_id?`, `unit_id?`, `principal_id?`, `routing_policy_version` | Exactly one audience selector; used to materialize per-recipient requests. Audience/group membership is not a grant and is re-evaluated before showing/resolving cards. |
| `decision_challenge` | `request_id`, `principal_id`, `session_binding_digest`, `review_digest?`, `selection_digest?`, `nonce_digest`, `expires_at`, `consumed_by_command_id?` | Server-issued, short-lived, one-use, session/request/revision bound. Nonce is not returned in logs or reusable across requests. |
| `inbox_preference` | `request_id`, `principal_id`, `snoozed_until?`, `dismissed_at?`, `version` | Personal-only; never changes Gate, request state, deadline or shared task. |
| `decision_draft` | `request_id`, `principal_id`, `base_review_digest?`, `encrypted_body`, `expires_at`, `version` | Private, revision-bound; rebasing explicit. Access checked after offboarding or task confidentiality change. |

DecisionRequest is durable so routing, draft identity and user outcomes survive restarts; it has no standalone authorization endpoint. `ResolveDecision` loads and locks its Gate and resolves through the kind-specific command. When one vote leaves quorum pending, only that recipient's request becomes `resolved`. When quorum/rejection terminates a gate, remaining requests become `resolved` with a derived `gate_closed` outcome and no fabricated attestation. Expiry/cancellation/supersession map to the corresponding request state. Already-resolved requests retain their original response history; the canonical gate outcome appears separately.

Requester endorsement means “this revision expresses my requested outcome.” Effect approval means “under my current eligible organizational role I authorize these exact effects.” An execution service's upstream credential means “this provider recognizes this technical principal.” None substitutes for another. Granting an approver a new role must not widen the requesting agent's grant, delegation, context, cached data or workspace.

## 7. Execution, effects and independently verified evidence

### 7.1 Execution and evidence records

| Table | Required fields / invariants |
|---|---|
| `run` | `task_id`, `attempt_number int`, `attempt_id uuid`, `automation_package_id`, `input_manifest_digest`, `task_version_at_admission`, `execution_epoch`, `invocation_id`, `state admitted/launching/running/parking/parked/succeeded/failed/cancel_requested/cancelled/unknown`, `lease_owner?`, `lease_until?`, `fence_epoch bigint`, `reservation_id`, `started_at?`, `finished_at?`, `cleanup_verified_at?`. Unique `(org_id,task_id,attempt_number)` and `(org_id,attempt_id)`. A parked run is historical; resumption normally creates a newly admitted attempt. |
| `invocation` | `principal_id`, `mode on_behalf_of/autonomous`, `caller_principal_id?`, `delegation_id?`, `service_mandate_id?`, `purpose`, `task_id`, `effective_ceiling_digest`, `authz_epoch_snapshot`, `workspace_id`, `expires_at`. On-behalf-of requires caller/delegation; autonomous requires mandate. Isolation key includes org, invocation and authority context, not agent name alone. |
| `step_checkpoint` | `run_id`, `step_key`, `checkpoint_revision`, `input_digest`, `output_artifact_id?`, `state pending/running/completed/failed/unknown`, `recorded_at`. Unique `(org_id,run_id,step_key,checkpoint_revision)`; completed output reused on duplicate delivery. |
| `effect` | `business_operation_id`, `review_package_id`, `operation_id`, `approval_gate_id`, `payload_digest`, `account_binding_id`, `provider_idempotency_key`, `request_fingerprint`, `state`, `dispatch_generation bigint`, `authorization_decision_id?`, `first_dispatch_at?`, `reconcile_after?`, `remote_object_id?`, `last_receipt_id?`. Unique `(org_id,business_operation_id)`; gate selection includes operation. Keys are stored before sending and never rotated to escape uncertain errors. |
| `effect_dispatch` | `effect_id`, `generation`, `executor_principal_id`, `fence_epoch`, `authorization_decision_id`, `state reserved/send_started/response_recorded/unknown/abandoned_before_send`, `reserved_at`, `send_started_at?`, `remote_request_id?`. Unique `(org_id,effect_id,generation)`; at most one unresolved send generation. |
| `execution_receipt` | `effect_id`, `dispatch_id?`, `kind dispatch_intent/provider_acceptance/provider_result/readback/reconciliation/compensation`, `observation_source`, `observed_at`, `recorded_at`, `account_binding_id`, `downstream_principal_reference`, `remote_request_id?`, `remote_object_id?`, `result_artifact_id?`, `result_digest?`, `outcome_code`, `verifier_principal_id?`, `command_id`. Append-only; receipt describes evidence, not overwritten success flags. |
| `artifact` | `storage_reference`, `bytes_digest`, `media_type`, `size_bytes bigint`, `classification`, `owner_resource_id`, `source_provenance_id?`, `encryption_key_reference`, `retention_until`, `privacy_state live/restricted/deletion_pending/deleted`, `legal_hold_count`, `created_by`. References never carry permanent public access tokens. |
| `verification` | `task_id`, `acceptance_spec_id`, `criterion_id`, `effect_id?`, `artifact_id`, `verifier_principal_id`, `verifier_release_digest`, `state passed/failed/inconclusive/withdrawn`, `observed_at`, `valid_until?`, `target_version?`, `account_binding_id?`, `supersedes_id?`. Immutable observations; withdrawal/new observation is appended with a current validity projection. |
| `task_completion` | `task_id`, `task_version_before`, `acceptance_spec_id`, `verification_manifest_digest`, `completed_by`, `completed_at`, `command_id`. One completion per task version; explicit reopen preserves prior completion. |
| `account_binding` | `connector_installation_id`, `provider_tenant_reference`, `official_account_reference`, `environment`, `downstream_principal_reference`, `credential_reference`, `scope_digest`, `state active/revoked`, `trust_epoch`. No secrets in domain rows. Rotation preserving identity/scope is distinct from semantic identity/account change. |

Verification policies select an independent trusted verifier or human/domain reviewer appropriate to risk; the preparing program cannot be the sole authority for its own success. Required evidence binds exact target/account, criteria revision, relevant operation and observation time. A provider acceptance receipt does not establish application; applied inventory does not establish later settlement; email acceptance does not establish delivery/read. Later drift opens a new exception and may explicitly reopen work—it does not falsify what was actually observed historically.

### 7.2 Supporting durable security records

- `authorization_decision`: `org_id`, `id`, `actor_principal_id`, `invocation_id?`, `action text`, `resource_id`, `outcome allow/deny`, `policy_version`, `guard_epoch_manifest jsonb`, `human_quorum_manifest jsonb`, `external_authority_observed_at?`, `authority_valid_until`, `review_digest?`, `selection_digest?`, `account_binding_id?`, `reason_codes jsonb`, `decided_at`, `command_id`. Insert-only, classified; current guards are rechecked rather than trusting this historical record indefinitely.
- `domain_event`: `org_id`, `id`, `aggregate_resource_id`, `aggregate_version`, `event_index int`, `event_type text`, `schema_version int`, `actor_principal_id`, `command_id`, `causation_id?`, `correlation_id`, `occurred_at`, `classification`, `payload_artifact_id?`, `payload_digest?`. Unique `(org_id,aggregate_resource_id,aggregate_version,event_index)`; restricted payload separate from minimal indexing metadata. No total chronological ordering inferred across external systems.
- `outbox`: `org_id`, `id`, `event_id`, `destination text`, `delivery_key text`, `state pending/delivering/delivered/exception`, `attempt_count int`, `available_at`, `lease_until?`, `last_error_code?`. Unique `(org_id,destination,delivery_key)`; receiver identity and destination are allowlisted, never arbitrary URLs from agents.
- `provider_inbox`: `org_id`, `id`, `account_binding_id`, `provider_event_id text`, `schema_version int`, `authenticated_at`, `observed_at?`, `payload_artifact_id?`, `payload_digest`, `state received/processing/processed/rejected/exception`, `processed_at?`. Unique `(org_id,account_binding_id,provider_event_id)`; raw payload storage is privacy-filtered and bounded before acknowledgement. Provider-specific authentication and replay-window rules must be qualified.
- `work_graph_guard`: `(org_id PRIMARY KEY, version bigint)`; acquired for graph changes and dependency-sensitive admission. `privacy_guard`: `(org_id,scope_resource_id PRIMARY KEY within org, epoch bigint, state live/restricted/deletion_pending/deleted, purge_generation bigint)`; approved scopes use normalized memberships for exact affected objects.
- `gate_dependency`: `(org_id,gate_id,required_gate_id)` composite FKs, unique pair, no self-edge; DAG validated under task/graph locks. An effect Gate explicitly depends on its exact requester-endorsement Gate and any technical-review gates. Runtime liveness checks cover the whole dependency set; a generic approved gate cannot be substituted.

### 7.3 State transition tables

All transitions use authenticated commands, expected versions and the transactions in §10. Any edge not listed is rejected. A self-transition is permitted only as an idempotent replay of the same command or a documented metadata update, not a second effect.

**Task**

| From | Command → to | Preconditions |
|---|---|---|
| `draft` | Activate → `ready` | Accepted accountable human, criteria fixed, required inputs/readiness valid; otherwise Activate → `blocked` with explicit blockers. |
| `ready` | Admit → `in_progress` | Current authorization, prerequisites, budget/reservation, execution fence and transactional dispatch intent. |
| `ready`, `in_progress`, `in_review` | Block → `blocked` | Persist typed blocker; if execution must stop, fence it and track cleanup independently. |
| `in_progress` | SubmitReview → `in_review` | Immutable package/gate/requests committed; parking requested, not automatically confirmed. |
| `blocked`, `in_review` | Reevaluate → `ready` | Every mandatory blocker cleared; approval alone does not bypass other prerequisites. |
| `in_progress`, `in_review`, `blocked`, `ready` | Complete → `done` | All required acceptance criteria have current valid verification; mandatory effects verified or an explicitly permitted criterion waiver; no unresolved unknown mandatory effect or live execution requiring cleanup. |
| Any nonterminal | Cancel → `cancelled` | Authorized cancellation and durable fencing; cancellation means stop new work, not rollback prior effects. In-flight/cleanup exceptions remain visible on cancelled task. |
| `done`, `cancelled` | Reopen → `draft` | Explicit authorized reason, new task version and evidence re-evaluation. Never automatic queue retry. Historical completion/cancellation remains. |

**Gate and DecisionRequest**

| Aggregate | From | To | Trigger |
|---|---|---|---|
| Gate | `pending` | `approved` | Kind-valid affirmative attestations meet current distinct-human/role quorum before expiry; endorsement gate approval is not effect approval. |
| Gate | `pending` | `rejected` | Eligible negative decision under pinned rejection policy. |
| Gate | `pending` | `expired` | Database decision time reaches deadline. Reads/commands treat overdue pending rows as unusable before sweeper materializes expiry. |
| Gate | `pending` | `cancelled` | Authorized task/gate withdrawal; fence dependent undispatched effects. |
| Gate | `pending` | `superseded` | New material proposal/selection/criteria revision replaces this decision. |
| Gate | Any terminal state | No transition | New decision requires a new Gate. An approved historical Gate may become currently unusable through revocation, expiry or changed dependencies without rewriting the decision. |
| DecisionRequest | `pending` | `resolved` | Recipient response recorded, or canonical gate approved/rejected and request closed by derived outcome. |
| DecisionRequest | `pending` | `expired`, `cancelled`, `superseded` | Corresponding canonical gate/work outcome or eligibility withdrawal; reason distinguishes cases. |
| DecisionRequest | Any terminal state | No transition | New routing/revision creates a new request. Personal snooze/dismiss never enters this state machine. |

**Handoff**

| From | To | Trigger |
|---|---|---|
| `offered` | `awaiting_access`, `clarification_requested` | Evidence access missing or recipient asks a bounded question. |
| `awaiting_access`, `clarification_requested` | `offered` | Authorized evidence/access resolved or answer provided; packet changes may instead require superseding offer. |
| `offered` | `accepted`, `declined` | Recipient resolves matching handoff Gate; accept verifies current access, task version, time and ownership. |
| Any nonterminal | `expired`, `cancelled`, `superseded` | Deadline, authorized withdrawal or materially changed task/packet. |
| Any terminal | No transition | New offer with new version/lineage required. |

**Run**

| From | To | Meaning |
|---|---|---|
| `admitted` | `launching`, `cancelled` | Dispatch or cancel before launch with proven no launch. |
| `launching` | `running`, `failed`, `unknown`, `cancel_requested` | Deterministic runner attempt inspection determines actual state; launch-response loss is unknown until inspected. |
| `running` | `parking`, `succeeded`, `failed`, `cancel_requested`, `unknown` | Checkpoint/gate, process outcome, cancellation, or lost contact. |
| `parking` | `parked`, `unknown`, `cancel_requested` | `parked` requires persisted continuation and independently verified resource cleanup. |
| `cancel_requested` | `cancelled`, `unknown` | Cancellation requires verified runner termination/cleanup; external effects reconciled separately. |
| `unknown` | `running`, `parking`, `parked`, `succeeded`, `failed`, `cancel_requested`, `cancelled` | Trusted runner inspection/reconciliation, not guessed retry. |
| `parked`, `succeeded`, `failed`, `cancelled` | No transition | New domain attempt for further work. Resources remain charged until cleanup is verified even if process outcome already known. |

**Effect**

| From | To | Required evidence/control |
|---|---|---|
| `prepared` | `authorized` | Exact operation selected by approved effect Gate plus endorsement/policy requirements. This is not dispatch permission forever. |
| `prepared`, `authorized` | `stale`, `rejected`, `cancelled_before_dispatch` | Changed preconditions, denied policy/approval, or cancellation before send admission. |
| `authorized` | `dispatching` | Durable exclusive dispatch reservation plus current authorization decision. |
| `dispatching` | `accepted`, `applied`, `failed_no_effect`, `unknown`, `stale`, `rejected`, `cancelled_before_dispatch` | Trusted provider result; ambiguous timeout/crash → `unknown`, never inferred no-effect. The last three outcomes require proven no send/known no-effect rejection (for example enforced provider CAS); cancellation after `send_started` is not proven no send. |
| `accepted` | `applied`, `failed_no_effect`, `unknown` | Provider-specific observation; asynchronous acceptance alone stays accepted. |
| `applied` | `verified`, `needs_attention` | Independent readback/invariants pass or fail/inconclusive. |
| `unknown` | `accepted`, `applied`, `verified`, `failed_no_effect`, `needs_attention` | Reconciliation evidence with exact account/object/correlation. Empty eventually consistent search is insufficient proof of no effect. |
| `failed_no_effect` | `authorized` | Explicit retry command only if unchanged intent, still valid authority, bounded retry budget and provider-conformance rules allow; append attempt history. |
| `needs_attention` | `applied`, `verified`, `failed_no_effect` | Adjudicated new evidence; no unilateral preparer assertion. |
| `verified`, `stale`, `rejected`, `cancelled_before_dispatch` | No normal execution transition | Recovery/new revision is separately governed; later drift is an exception observation. |

Compensation is a separate effect linked by `compensates_effect_id`, with its own approval or exact preauthorized bounded compensation policy and the same state machine. It does not erase the original verified effect. Batch status is a projection (`unstarted/in_progress/partial/verified/needs_attention/cancelled`) computed from mandatory operations; no promise of atomic rollback. Settled business status is a separately observed domain criterion, not an assumed effect-state synonym.

## 8. Authorization model and matrix

### 8.1 Evaluation algorithm

Authenticate before resource lookup: verify credential issuer, audience, validity, token/principal type and trusted provider-specific immutable subject mapping. Browser sessions use secure HttpOnly cookies, CSRF and origin checks for mutations; high-risk human decisions require the configured authentication assurance/reauthentication. Workload tokens cannot call human-only endpoints regardless of role-like claims.

For an on-behalf-of invocation:

`effective preparation authority = current caller grants ∩ agent ceiling ∩ delegation chain ∩ task/resource/purpose bounds ∩ classification/export policy ∩ org policy`, with current denies overriding every allow.

For an autonomous invocation, replace caller/delegation with an explicit current service mandate and sponsor policy. Never synthesize a human caller. Effective scope and data isolation are per invocation; a reviewer uses a fresh scoped invocation even when selecting the same agent definition as requester.

For effect dispatch:

`allowed = stored approved exact selection ∧ required endorsement ∧ current eligible human quorum ∧ current organizational execution mandate ∧ live account/connector trust ∧ provider credential capability ∧ valid target/dependency predicates ∧ budget/time/fence constraints`.

Requester need not have official-write permission to propose a payment. Their endorsement contributes intent, not the missing write grant. The approver's organizational mandate authorizes constrained service execution; it does not elevate the requester or impersonate the approver at the provider. If genuine provider user delegation is required, use the provider-supported flow or declare unsupported. Default initial policy requires requester membership and all counted approvers to remain eligible at dispatch; any future “approval survives offboarding” policy requires explicit governance, not a hidden exception.

Current entitlement checks use local committed deny/epoch state plus provider/directory checks where required. External directory propagation is not instantaneous. Configure and record maximum allowed authority age per risk class; mandatory stale/unavailable checks fail closed. New local revocations linearize as specified in §10. Historical policy snapshots explain decisions but cannot override current deny state.

### 8.2 Capability matrix

Every “yes” below additionally requires current tenant/resource/purpose authorization. No column grants blanket organization visibility.

| Operation | Requester human | Scoped preparer agent | Eligible human reviewer/approver | Accountable human | Org authority administrator | Execution/verifier service |
|---|---|---|---|---|---|---|
| Read task/evidence | Granted subset | Caller/mandate intersection only | Granted review subset | Granted subset, not all secrets | Only explicit content access | Narrow operation evidence |
| Create task / draft proposal | If task.create/proposal.prepare | If delegated | If granted | If granted | If granted | Only explicit mandate |
| Offer accountability/role | With assign/role.offer capability | Suggest only initially | With explicit assign capability | Can offer handoff under policy | Can offer permitted roles | No human acceptance |
| Accept own human accountability/role | Yes when offered | No | Yes when offered | Yes when offered | Cannot silently accept for another human | No |
| Endorse requester revision | Named requester only | No | Only if named requester; then cannot count as distinct effect approver | Only if named requester | No by virtue of admin | No |
| Technical evidence | May submit evidence | May submit labeled checks | May certify if technically eligible | May submit evidence | Only designated certification role | Independent verification mandate |
| Approve effects | Only if eligible and distinct-requester policy satisfied; baseline requester excluded | No | Yes, exact human role/quorum | Only if independently eligible | No automatic bypass | Never |
| Dispatch official effects | No direct credential release | No | No direct credential release | No | No direct credential release | Effect executor only after full policy check |
| Verify required outcome | Only if designated independent human verifier | Not sole verifier of own work | If designated independent verifier | Not automatic authority | No automatic authority | Trusted scoped verifier, not preparing code |
| Complete task | If completion capability and evidence | Propose completion only initially | If completion capability and evidence | Yes with evidence | Only if granted and evidence | Kernel controller with evidence |
| Change permission/mandate | Request only | Request only | Separate authority-policy process | Request only | Approved governance workflow; separation rules apply | No self-grant |
| Personal snooze/draft | Own requests | No human inbox impersonation | Own requests | Own requests | Own requests only | Delivery projection only |
| Cancel shared work | If task.cancel capability | Only explicit reversible mandate, disabled initially | If granted | If granted | If granted, audited | Enforce approved cancellation/fences |
| Privacy deletion / legal hold | Request rights process | No | Designated case role only | No automatic right | Separate privacy/hold authority, not all admins | Execute approved scoped retention workflow |

### 8.3 Same-human quorum and separation of duties

At approval and each new dispatch, resolve human principal→canonical human through verified mapping. Count distinct canonical-human IDs, not accounts, browser sessions, agent IDs, role names or endorsements. Record identity-map version. A human holding two required roles may fill at most one quorum slot when policy requires independent humans. Implement slot assignment as a deterministic matching problem over eligible humans and required roles; simply checking `count(distinct human) >= q` is insufficient when a required role slot is absent.

Baseline effect policy requires a requester endorsement and at least one distinct eligible effect approver; requester is excluded by canonical-human identity. Higher-risk policy can require multiple role slots, disallow preparer/technical author as final verifier, and require independently controlled technical review. Approver agents contribute evidence only. A human's alternate account cannot manufacture independence. Unknown/unverified human linkage fails closed for quorum-sensitive actions. Rejecting a gate must come from an eligible recipient under that gate's decision policy; a random reader cannot veto.

Keep gate immutable decision outcomes distinct from current executability. An approved gate whose approver is revoked remains historically approved but `executable=false` with an authorized reason. Reapproval creates a new gate referencing the same immutable package if still valid; changing package content creates a new package revision. Never “repair quorum” by silently replacing an old signer with someone who did not attest.

## 9. Command/query contract and preconditions

### 9.1 Wire envelope

All state-changing operations use `POST /v1/orgs/{org_id}/commands` with an allowlisted command schema. Queries and `ReadEvents` are separately authenticated, policy-filtered contracts. The path org is a requested tenant, validated against the authenticated binding; body actor/tenant overrides are rejected.

```json
{
  "schema_version": 1,
  "command_id": "opaque-client-generated-uuid",
  "command_type": "ResolveDecision",
  "target": {"kind": "decision_request", "id": "opaque-uuid"},
  "expected": {
    "aggregate_version": 7,
    "task_version": 12,
    "gate_version": 3,
    "review_revision": 2,
    "review_digest": "server-issued-canonical-digest"
  },
  "input": {
    "choice": "approve",
    "challenge": "single-use-host-challenge",
    "operation_selection_digest": "stored-selection-digest",
    "comment_artifact_id": null
  }
}
```

UUID/digest strings above are illustrative placeholders, not valid fixture tokens. The authenticated context supplies `org_id`, `actor_principal_id`, canonical-human identity (when human), session/workload identity, invocation/delegation chain, assurance, request trace and current authorization versions. The browser does not submit approver identity, replacement operations, execution prompts, raw credential references, or arbitrary callback URLs.

Response envelope: `{command_id, status: applied|replayed|rejected, aggregate:{kind,id,version}, result:{...typed result...}, event_cursor?, error?:{code, retryable, permitted_details}}`. Async acceptance returns a durable domain ID/state, not a fabricated completed effect. A replay must be authorized to read the stored result now; revocation cannot be bypassed by retrieving a previously successful command response.

`command_receipt` fields: `org_id`, `command_id`, `actor_principal_id`, `command_type`, `request_digest`, `target_id`, `state`, `result_digest`, `restricted_result_artifact_id?`, `created_at`, `committed_at?`; unique `(org_id,command_id)`. Reuse with different actor/type/digest is rejected. Command ID expresses retry identity, not authority. Nonce is consumed in the same transaction as the attestation; exact committed replay returns its receipt without inserting another vote. Rate-limit unresolved requests and decision challenge issuance.

### 9.2 Kind-specific preconditions

| Command | Required checks before write |
|---|---|
| `CreateDelegation` | Current grant subset, purpose/resource/classification compatibility, lifetime/depth/budget narrowing; nondelegable roles excluded. |
| `AcceptRole`, `AcceptAccountability` | Authenticated intended human, active membership, live offer, compatible scope/time, no conflicting accepted assignment. Acceptance alone cannot expand capability policy beyond approved offer. |
| `PublishReviewPackage` | Current task/proposal version, schema/canonical bytes/digests verified server-side, preparation provenance, account binding, complete material fields, operation DAG, immutable evidence references, business identity reservation. |
| `ResolveDecision` | Gate/request/task versions and package/selection exact; request pending; challenge valid; current eligible human/assurance; no supersession/expiry; necessary endorsement; distinct-human policy. Partial votes commit without premature dispatch. |
| `RequestRevision` | Named current revision, proposal.prepare permission, bounded feedback and preparation budget; supersedes affected pending gates, never patches approved bytes. |
| `AcceptHandoff` | Current offer/task version, intended recipient, evidence sufficient and currently readable, hop limit/deadline, existing owner matches offer; atomic owner transfer. |
| `AdmitRun` | Task ready, accepted accountability, current prerequisites/evidence, granted invocation, package certified, resource/budget caps and fence current. |
| `AuthorizeEffectDispatch` | Current full §8 effect predicate, unchanged stored payload, prerequisites verified, no existing ambiguous dispatch, live integration and account binding. |
| `RecordReceipt` | Authenticated connector/verifier identity scoped to effect/account/attempt, valid schema/provenance, receipt dedupe and legal state transition. Agent summary cannot take this path as provider truth. |
| `CompleteTask` | Exact criteria/task version and complete required verification set under locks; no unresolved mandatory effects; completion event and status commit together. |
| `CancelTask`, `RevokeGrant` | Explicit capability, current aggregate version; increment relevant fences/epochs and record recovery obligations before dispatching cancellation notifications. |

Use 401 for failed authentication; nonenumerating 404 for absent/unauthorized object reads; 403 only when existence may be disclosed; 409 for state/idempotency conflict; 412 for stale expected version/digest; 422 for invalid semantic schema; 503 for unavailable mandatory authority dependency. Redact conflicts that would reveal a hidden participant, evidence item or sibling tenant. Stale decisions require explicit user rereview; clients must not automatically update expected revision and retry an approval.

## 10. Transaction protocol and ten critical races

### 10.1 Shared transaction discipline

The first implementation favors correctness over maximum write throughput. Commands run at PostgreSQL READ COMMITTED with explicit serialization guards and row locks; no correctness claim relies on READ COMMITTED alone. Graph mutations acquire `work_graph_guard`. Authority-sensitive mutations/dispatch acquire applicable `authz_guard` rows, including the organization guard, in deterministic `(scope_kind,scope_id)` order. Reads of guards use `FOR SHARE`; revocation/changes use `FOR UPDATE`. Acquiring shared org guard serializes with org policy/revocation changes without serializing independent ordinary commands. Fine-grained revocation still takes the org guard in shared mode plus its affected guard exclusively.

Global lock order: **command receipt claim → organization/identity and other authorization guards → work graph guard if needed → capacity/budget guards in organization, agent, pool/host, connector order (stable ID order within each class) → task rows sorted by ID → proposal/handoff rows → gate rows → DecisionRequest rows → business operation/effect/dispatch rows → verification/artifact-retention rows sorted by ID**. A command that needs additional earlier locks aborts/retries from the start; it never acquires them out of order. Gate creation precedes request creation; circular handoff↔gate references use a deferred same-tenant constraint or link table within one transaction. Role/grant revocation need not synchronously rewrite every task: increment guard/deny state and publish invalidation; dispatch always checks canonical state.

After locks, reread authoritative values and obtain `clock_timestamp()` for the decision/expiry check. Compare expected versions; use `UPDATE ... WHERE version=$expected` and increment once for each changed aggregate. Insert domain audit event(s), outbox entry, command result and any River transactional dispatch together in the same transaction. A commit response lost to the client is resolved via the command receipt. Rollback must leave none of the business transition, attestation, event or queue intent committed independently.

Use River OSS transactional insertion only after the selected Go driver/API is qualified; if the domain transaction cannot compose directly with the queue API, atomically write a domain outbox and use a deduplicating bridge, rather than nontransactional enqueue. The queue cannot call a provider from inside the SQL transaction. No remote calls, directory network lookups or model invocations while holding database locks. Resolve external authority observations beforehand, record freshness, and fail closed if their freshness budget expires before the local decision. Local locks cannot serialize against an external directory or native application writer.

Serialization/deadlock errors are bounded retries with the same command ID and no external side effect inside the retry region. Under SERIALIZABLE in any later optimization, the whole transaction must retry on serialization failures; it is not a substitute for provider idempotency or tenant composite keys. Outbox/inbox handlers are idempotent and verify actual canonical state before acting.

### 10.2 Required race protocols

| # | Critical race | Exact transaction semantics and required outcome |
|---|---|---|
| **R01** | Proposal revision vs approval | Both commands take auth guards, task, proposal, gate and request locks in that order. Publish validates expected proposal/task version, inserts new immutable revision, advances head, supersedes pending gates/requests and invalidates undispatched eligibility in one commit. Resolve rereads head/revision/digest after locks. If publish wins, old approval fails 412 with no vote. If approval wins, it records a historically valid vote; later publish prevents new dispatch of obsolete intent. Already-send-started effects stay visible and cannot be silently replaced. |
| **R02** | Duplicate click, replay or two final quorum votes | Claim unique command receipt before aggregate locks; conflict waits for existing transaction then verifies actor/request digest. Lock Gate and its pending requests. Insert at most one attestation per canonical human, consume challenge and compute role-slot matching using live eligibility. Only transition `pending→approved` once and insert unique dispatch readiness/outbox key `(org,gate,approved-version)`. Two different commands by one human cannot count twice; unrelated distinct votes serialize and second observes current quorum. |
| **R03** | Offboarding/role or connector revocation vs effect send | Approval/dispatch takes applicable auth guards FOR SHARE, reads current membership/role/mandate/trust and epochs. Revocation takes relevant guard FOR UPDATE, writes deny/increments epoch and outbox atomically. Dispatcher reserves effect then, in a short separate final-send transaction using the same lock protocol, rechecks guards/expiry/task epoch and CASes `reserved→send_started`. Whichever commits first defines local authorization ordering. Revocation first prevents send; send-start first means in-flight and reconciliation required. No claim that revocation recalls an accepted provider request or closes the commit-to-network gap. |
| **R04** | Handoff acceptance vs task edit, second handoff or lost recipient access | Task/offer/Gate/recipient authority guards serialize both paths. Acceptance validates offer task version, packet digest, evidence ACL epochs and current owner; ends old interval and inserts new accepted owner plus attestation/request outcome in one transaction. Partial uniqueness/exclusion rejects double ownership. Access revoked first yields awaiting_access/nonacceptance; accepted first preserves attribution but subsequent reads still deny. Task edits that alter meaning supersede the offer. |
| **R05** | Concurrent prerequisite/containment additions and admission | Both graph edits and admission lock work_graph_guard before affected tasks. Edits check complete active graph for cycles and scope, write edges and increment graph/task versions. Admission rereads edges and target evidence after locks and binds the graph/task version into its run. Two individually harmless edges cannot concurrently form a cycle. Edits affecting an admitted task fence/reblock it through explicit policy; they cannot pretend its existing input snapshot included a new prerequisite. |
| **R06** | Task completion vs evidence withdrawal, acceptance change or new unknown effect | All relevant commands lock task before effects and verification/artifact validity projections. Complete reads criteria version, every mandatory criterion and required effect state from canonical rows under locks; inserts completion and changes status together. Withdrawal/criteria edit winning first blocks completion. Completion winning first remains a historical completion; later discovery records exception and explicit reopen. No background `run.succeeded` callback can issue an unchecked `task.done` update. |
| **R07** | Task cancellation vs admission/launch and parked cleanup | Both lock task and compare execution epoch. Cancel increments epoch, persists cancellation, fences existing attempts and outbox stop commands. Admission loses if cancelled/version changed. A queued stale dispatch observes epoch mismatch and cannot launch. If launch handoff already occurred, deterministic attempt inspection/stop reconciles it; task can be cancelled while run is cancel_requested/unknown and reservation remains charged until cleanup verification. Queue ack is not cleanup evidence. |
| **R08** | Lease takeover, timeout-after-commit, duplicate business proposal or aged provider key | Lock business_operation then effect/dispatch; unique business key prevents a second proposal from minting an independent effect. Only one unresolved send generation exists. CAS with current fence authorizes first send; timeout/crash makes result unknown. Lease takeover allows inspect/reconcile, not a second send just because lease expired. Retry requires proven no-effect or qualified provider same-key replay within actual supported horizon; preserve payload/key. Aged ambiguous request remains unknown/needs_attention. Delayed old worker receipt is stored as evidence but cannot bypass current fence/state rules. |
| **R09** | Native provider writer changes target/dependency between preview and apply | Local transaction binds exact preconditions and records dispatch, then connector sends stored payload with qualified provider atomic CAS/predicate. A stale provider rejection records stale/no-effect evidence and requires reprepare/reapproval; never replace ETag and retry old intent. A local lock does not protect native writers or unrelated bank/period records. Without provider atomic coverage/exclusive writer proof, high-risk automatic operation is unsupported. This race cannot be solved by PostgreSQL transaction isolation alone. |
| **R10** | Duplicate/out-of-order webhook, reconciliation and privacy deletion/legal hold | Authenticate raw webhook/account/mode outside domain mutation, durably insert unique provider/account/event inbox before acknowledgement. Reconciliation obtains task/effect locks, applies legal evidence-driven transition and dedupes observation; timestamps alone cannot order business truth. Before reading/storing payload, lock relevant artifact-retention/privacy guard; deletion and hold commands use the same lock. Active hold prevents purge; deletion winning first causes a minimal restricted receipt/tombstone rather than recreating deleted payload from a replay. A queued payload job rechecks privacy state before materializing content. Provider delivery is not an approval or task completion. |

For R03, a local `send_started` reservation is the irreversible authorization boundary of this protocol, not proof bytes were transmitted. If the executor dies after that commit, treat the outcome conservatively as unknown unless trusted runner/connector evidence proves no send. Stop/credential withdrawal may reduce exposure, but neither PostgreSQL nor River can guarantee instantaneous revocation at a remote provider. Conformance must measure and document that boundary.

## 11. Access revocation, historical attribution and metadata containment

Revocation affects future reads, decisions, dispatches, refreshes and context retrieval. It does not erase who requested, prepared, endorsed, approved or executed past work. Record identity and role/policy snapshots in restricted audit facts; UI resolves current display labels only if permitted. An offboarded principal retains a nonlogin tombstone and historical canonical references. Do not leave old credentials active merely to render history. A previously authorized artifact download or human memory cannot be recalled; minimize exports and expose this boundary honestly.

Authorization is checked at every read/use, including artifacts, attachments, historical versions, comments, search, exports, event replay, websocket subscriptions, notification expansion, signed download links and Gontext retrieval. Prefer gateway-authenticated streaming or short-lived narrowly scoped links; signed URLs cannot promise immediate revocation after issue. Long-running streams must recheck on epoch changes/defined intervals and terminate on revoke. A receipt proving past access is not perpetual access.

**Metadata is data.** Apply the same contextual labels to task titles, counts, blocked-by links, decision recipients, operation counts/amount aggregates, audit actor names, autocomplete, search snippets, attachment sizes, timestamps, cache hit behavior and error messages. Filter before pagination/counting and before ranking; do not fetch a broad result and hide rows only in React. Cursor tokens bind org, caller/scope, query and policy epoch and must not expose hidden sort keys. Cross-tenant and unauthorized IDs return the same external shape. Authorized viewers may receive a generic “restricted prerequisite” only if policy permits existence disclosure.

Org hierarchy and global agent discovery do not confer confidential HR/finance visibility. Managers get authorized aggregates with purpose/retention controls; suppress small sensitive cohorts under configured policy. Do not publish individual activity leaderboards as a proxy for contribution. User-facing analytics must allow contest/correction and distinguish queue wait, approval wait, external wait, rework and verified outcome.

Caches, vector indexes, invocation memories and logs carry `(org,authority-context,resource-label,policy-epoch)` boundaries; never cache approver-only findings under a shared agent/task key. Separate review-agent workspaces and credentials from requester context. Derived summaries can leak restricted facts and require authorized declassification/redaction, not just removal of raw citations. Gontext receives scoped outbox observations with provenance/labels; it is not shared-table workflow storage. Context-required work blocks when current context authority cannot be established. Events to Gontext may lag without widening access.

Rich previews are untrusted presentation: host owns tenant/account/environment banners, complete material operation inventory, warnings and approval controls. Use inert typed host components by default; optional separate-origin sandboxed views get minimal filtered data, no credentials/ambient cookies, restricted schema-checked messaging and deny-by-default network. Unknown material fields block approval. A viewer unable to see decisive fields must route to an eligible reviewer, not approve a redacted green check. Neutralize active HTML/tracking pixels/formulas and show material bidi/lookalike risks. Artifact digests bind bytes, not human comprehension or factual truth.

## 12. Corrections, privacy, deletion and legal hold

### 12.1 Separate feedback domains

| Record | Required fields | Effect |
|---|---|---|
| `presentation_preference` | `principal_id`, `scope`, `schema_version`, `settings`, `version` | Personal layout/language/expansion choices only; cannot alter material preview fields. |
| `case_correction` | `task_id`, `subject_resource_id`, `base_revision/digest`, `reported_by`, `correction_kind factual/extraction/attribution/verification`, `claim_artifact_id`, `supporting_evidence_id?`, `state submitted/triaged/accepted/rejected/superseded`, `adjudicator_id?`, `decision_reason_artifact_id?`, `supersedes_id?` | Append correction/adjudication; accepted material correction creates a new review/criteria/evidence revision and invalidates affected pending authority. It never edits past attestations or provider receipts. |
| `business_rule_proposal` | `source_correction_ids`, `scope_resource_id`, `supplier/account_filter`, `candidate_digest`, `evaluation_manifest_id?`, `state proposed/evaluating/approved/rejected/promoted`, `approver_id?` | Governed reusable rule change, with independent evaluation and explicit scoped promotion. One corrected case does not authorize generalization. |
| `authority_change_request` | `requested_capabilities`, `scope`, `justification_artifact_id`, `policy_base_version`, `gate_id`, `state` | Separate governance workflow. “Allow next time” or natural-language feedback never grants a role. |
| `privacy_case` | `subject_reference`, `request_type access/rectify/restrict/erase/contest`, `scope_resource_id`, `verified_requester_id`, `lawful_basis_code`, `jurisdiction_policy_id`, `state opened/assessed/approved/denied/executing/completed/exception`, `decision_by?`, `deadline?`, `result_manifest_id?` | Scoped rights/retention adjudication; legal policy determined by operator/counsel, not invented universal legal rules. |
| `legal_hold` | `case_reference`, `scope_manifest_id`, `issued_by`, `authority_basis_artifact_id`, `effective_at`, `released_at?`, `release_authorized_by?` | Active holds prevent eligible physical purge/key destruction, not automatically authorize broader access. Release is independently authorized/audited. |
| `deletion_job` | `privacy_case_id`, `scope_manifest_digest`, `state planned/blocked_by_hold/running/partial/completed/exception`, `generation`, `result_manifest_id?`, `not_before` | Idempotent deletion by approved exact scope, with storage/index/cache/replica/knowledge-system acknowledgement tracking. |

### 12.2 Retention and erasure protocol

1. Classify data at ingest and assign retention, access and purpose policy. Minimize provider responses and raw prompts; secrets must not enter approval/audit payloads. Store encrypted sensitive payload separately from minimal operational metadata so lawful erasure need not corrupt foreign-key integrity.
2. A privacy officer verifies requester identity, jurisdiction/policy, scope, retention obligations, pending disputes and active holds. The system does not promise that every request warrants deletion or that all audit records are exempt. Hold existence/details may themselves be restricted.
3. Approve a versioned deletion manifest. In a transaction lock privacy/retention guards and affected artifact rows, recheck holds, mark data restricted/deletion_pending, revoke retrieval capability and enqueue deletion. Security/privacy restrictions apply immediately at the local authorization boundary; physical purge is asynchronous.
4. Purger reacquires the same guards immediately before each destructive storage action. A hold winning first blocks deletion. If deletion wins before a later hold is committed, record unavailability honestly; a hold cannot resurrect purged data. External delete/hold races need storage-specific protocol or conservative staging; no database transaction makes an object-store delete atomic with SQL.
5. Remove approved content from artifacts, search indexes, derived previews, caches, native session archives and permitted Gontext observations through each system's contract. Keep only approved minimal tombstones/business uniqueness markers under their own lawful retention policy. A bare hash may identify low-entropy personal data; assess whether to delete, key, or restrict it rather than assume anonymization.
6. Backups use documented retention/expiry and restricted restore procedures. If immediate selective backup erasure is unsupported, disclose the limitation and enforce a deletion/deny manifest before restored data is served or reindexed. Cryptographic erasure is claimed only when all relevant key copies and plaintext replicas are covered and verified. Legal hold must prevent destroying keys for held content.
7. Read back exact external deletion targets/status where supported, record acknowledgements or unresolved exceptions, and mark the case completed only when its approved completion criteria are met. No assumption that Gontext/provider export deletion is instantaneous; maintain partial/exception state and operator follow-up.

Historical integrity means append-only ordinary business updates and traceable amendments, not indefinite retention of personal content. When required material is lawfully purged, retain an authorized minimal marker “evidence deleted under policy” and restrict claims: a digest/tombstone does not permit future re-verification of missing bytes. Hold and privacy administrators cannot change effect payloads or retroactively manufacture an approval. Replayed webhooks/outbox jobs and late provider responses must consult deletion fences to avoid recreating erased content.

## 13. Invariants and implementation acceptance boundary

The implementation must express the following as constraint tests, command tests and cross-process integration tests, not documentation alone:

1. Every tenant reference—including actors, operation prerequisites, receipts and event subjects—is composite or explicitly mediated through the restricted global identity registry. A two-company fixture cannot cross-link any of them.
2. Human, agent and service authentication types cannot be confused; same-human aliases never produce extra quorum; role-slot matching is correct and current.
3. An accepted accountable human exists for actionable work; offers, execution assignment, manager hierarchy and project membership cannot silently substitute for acceptance or permissions.
4. Effective delegation only narrows. An approver's new role never widens requester invocation, caches, grants or provider credentials.
5. Immutable package/operation bytes and explicit semantic defaults are what the approver sees and executor loads. Material changes create new revisions and renewed applicable decisions.
6. DecisionRequest is a Gate projection. Personal dismissal, stale browser state, event delivery or an agent finding cannot approve effects or cancel shared work.
7. Task `done` requires the exact versioned verification contract. A succeeded run, completed River job, accepted HTTP response or generated self-test is insufficient.
8. Gate parking frees task-dedicated resources only after independent cleanup confirmation. Unknown cleanup and unknown remote effects stay visible.
9. Every new effect needs current authority and an exclusive durable dispatch record; provider uncertainty never becomes a blind new-key retry. Compensation is a separately governed effect.
10. Read authorization includes metadata, drafts, historical versions, streams, downloads and derived knowledge. Revocation blocks future use without rewriting historical attribution.
11. Case correction, business-rule promotion, personal preferences and permission change are separate commands/records. Privacy deletion and legal hold use explicit competing transitions and do not silently erase effect history.
12. Current-state changes, attestations, audit events, command receipts and dispatch intent commit atomically; duplicate delivery does not produce duplicate domain transitions.

**Required test fixtures:** two unrelated companies with colliding human-readable external IDs; one human with two verified login aliases; matrix manager without finance access; requester lacking production-write rights; distinct eligible approver with restricted evidence; the same agent definition in requester/reviewer invocations; expired cover delegation; handoff lacking citation access; hidden prerequisite; connector revoked after approval; ambiguous provider timeout; late webhook after deletion; legal hold racing purge.

**Required evidence for R01–R10:** deterministic barrier-controlled concurrent transactions; committed rows/events/command receipts; captured authorized request envelope and outbound connector semantics where applicable; actual failure/response; asserted absence of unauthorized dispatch. RLS qualification includes sequential alternating tenants on reused connections, transaction-pooling behavior, rollback/cancel, prepared queries, missing settings, privileged-role separation, view behavior and error enumeration. External races require a real sandbox provider or an explicitly labeled simulator; a simulator pass is not provider certification.

**Build sequencing within this chapter:** first implement schema/constraint fixtures and identity/tenant isolation; next work/acceptance/prerequisite commands; then immutable review/Gate/DecisionRequest and quorum; then dispatch/effect/verification transitions and the ten race tests; finally privacy/hold/replay tests and contextual projection leak tests. This ordering is dependency guidance, not a claim that any step has run.

**Open qualification items:** exact PostgreSQL/driver/River versions and transactional composition; identity-provider alias verification and freshness limits; operation-level provider CAS, idempotency retention, readback, triggers and actual upstream principal behavior; sandbox/egress and resource cleanup guarantees; storage retention/hold/delete semantics; Gontext access/deletion contracts. Unsupported high-risk behavior blocks certification. The design intentionally makes no universal exactly-once, distributed rollback, instantaneous external revocation, or provider-enforced ACL claim.

