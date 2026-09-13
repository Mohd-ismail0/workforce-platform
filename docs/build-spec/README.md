# Organizational Workforce Platform — Build Specification

**Status:** detailed proposed build specification. No application implementation, production deployment or runtime safety certification is included. This directory is the canonical implementation-planning entry point; earlier research under `/home/prod/research-*.md` is supporting evidence, not competing build instructions.

## Product

Humans delegate work to approved agents on organizational compute. Agents use authorized reads, shared Gontext knowledge and isolated local code preparation to produce reusable automations and concrete review packages. Employees inspect domain-native previews and endorse work; eligible humans approve exact official changes. A constrained executor applies approved effects, records receipts and obtains independent verification. Durable human waits release agent capacity while independent work proceeds. Per-human/agent/project boards, accepted handoffs, organization/matrix relationships, scoped global agents and governed improvement support the complete target.

## Read by implementation responsibility

| Document | Owner of specification | Read when |
|---|---|---|
| [Architecture](01-architecture.md) | Baseline stack, module seams, repository layout, precedence | Starting any implementation work |
| [Domain and security](02-domain-security.md) | Entity fields, constraints, states, permissions, transactions | Changing schema, commands, authority, decisions or handoffs |
| [Execution and integrations](03-execution-integrations.md) | SDK/checkpoints, queue-to-run mapping, gateways, plugins, Gontext, learning | Building execution, connectors, packages or agents |
| [Product and delivery](04-product-delivery.md) | Screens, preview/draft behavior, milestones and workstream ownership | Building frontend or selecting next implementation slice |
| [Operations and assurance](05-operations-assurance.md) | Deployment, degradation, recovery and adversarial tests | Before enabling real identities, data or effect execution |

## Implementation baseline

Go modular monolith, PostgreSQL current state with transactional audit/outbox, River OSS dispatch, React/TypeScript frontend. Separate constrained runner and effect executor. Logto authenticates; explicit policy and OpenFGA govern relationships; tenant isolation is reinforced in PostgreSQL. Gontext is independent organizational context, not task storage. Artifact storage holds evidence/builds/checkpoints. External providers remain source of truth. Version pins, provider capabilities and runtime isolation are qualified in early milestones rather than invented here.

## Resolved design conflicts

- River OSS can deliver dispatch commands beneath domain-owned gates; River Pro and a custom general-purpose queue are not prerequisites.
- `run` means an admitted domain attempt; transport retries retain the same launch/attempt identity. No second parent-run authority is introduced.
- Task terminal success is `done`, guarded by verified evidence. `verified_done` is not another task status. Run success does not imply task success.
- Gate and DecisionRequest have different jobs: one authorizes a decision outcome, the other makes it actionable for a recipient.
- Human review never silently refreshes the reviewed revision. New content requires deliberate revision switch and applicable renewed approval.
- Pending gate may precede process cleanup; fully parked/released capacity requires independent cessation evidence. Unknown/disconnected execution stays visible.
- All transactions use the domain chapter's single lock order, including budget/capacity guards; runtime no longer specifies an incompatible lock order.
- Requester authority never expands because an approver has stronger rights. Exact approved effects execute under an explicitly identified service mandate.
- Read-only access is semantically scoped; method names, local simulation, provider acknowledgements and UI success are not safety proofs.
- Continuous improvement produces reviewed candidates; it cannot rewrite protected verifier, policy or production behavior unilaterally.

## Delivery route

Follow PD-00 through PD-12 in the delivery chapter. The first implementation starts with PD-00 contracts/fixtures, then transaction and dispatch proof, effect simulator and review shell, one sandboxed preparer, and the integrated supplier-email/inventory review journey. Real read-only provider work precedes narrowly approved writes. Registry, second harness, organization agents, Gontext and governed improvement have explicit later gates without deleting them from the domain model.

Before any real data or writes, applicable operations/authority/privacy/restore gates apply regardless of milestone number. PD-12 is full-product release qualification, not permission to defer basic safety until the end.

## Acceptance and validation boundary

The assurance chapter specifies 52 adversarial scenarios (AT-001–AT-052), all NOT RUN, mapped to 20 invariant requirements (OA-01–OA-20). The domain chapter specifies ten transaction race protocols. The product chapter specifies 13 dependency-linked milestones (PD-00–PD-12).

`validate_spec.py` checks document presence, fences, scenario/requirement identifiers, trace references and milestone dependency acyclicity. Its result is document validation only: it does not execute the system, validate SQL semantics, prove authorization, certify a provider or run security tests. Output is recorded in `validation.json`.

## Decisions required before relevant activation

- Product name/repository and core distribution license.
- Actual mail/inventory providers, account scopes and sandbox availability.
- Pinned language/runtime/dependency versions and first harness certification.
- Operational authority matrix: eligible approvers, substitutes, segregation and emergency procedures.
- Data residency, retention, legal hold and employee analytics purposes.
- Gontext deployed grant/revocation/intake capabilities.
- Threat tier and hosting isolation, capacity and recovery objectives.
- Third-party code/assets reuse permission where needed; attribution and notices retained.

These do not prevent contract and synthetic-fixture work. Missing external-provider facts prevent live integration certification, not honest simulator implementation.

## Build handoff rule

Implement the smallest milestone slice with failing acceptance tests first. Use real PostgreSQL for transaction/race claims, explicit synthetic connectors for fault injection, and actual provider output for provider certification. Capture exact commands/results, pin source/config versions, and report unsupported cases. A merged plan or a passing document validator is never a completed software deliverable.
