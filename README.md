# Workforce Platform

A human-led multi-integration work platform: projects/tasks, agent preparation, immutable proposals, requester endorsement, distinct human approval, governed effects and evidence.

**Development build, not production ready.** External integrations are simulators; they do not send email, modify an ERP or edit external documents. Local opaque-token authentication is an explicit development mode, not organizational SSO. Registering a harness does not launch it.

## Design and build scope

- [Build specification](docs/build-spec/README.md)
- [Current implementation contract](BUILD_CONTRACT.md)
- [Build status and remaining scope](docs/BUILD_STATUS.md)

The full destination includes plugin infrastructure, multiple harnesses, organization/matrix permissions, human/agent boards, durable handoffs, Gontext, scoped shared assistants and governed improvement. A reference integration journey is a test of the platform, not its product boundary.

## Data infrastructure

Use an externally managed PostgreSQL database, with separate migration and runtime roles. Do not install a database in the application or assume Docker PostgreSQL is required. Credentials belong in an operator-controlled secret file outside the repository. Never commit source-system credentials or personal data.

## Run locally

Prerequisites: the pinned Go toolchain, Node/npm, and a dedicated existing PostgreSQL database. Operator-provided database credentials belong in `~/.config/workforce-platform/database.env` (mode 600) with `DATABASE_URL` and `MIGRATION_DATABASE_URL`. Local test identities belong in `~/.config/workforce-platform/local-auth.env` (mode 600): `AUTH_MODE=local`, `LOCAL_AUTH_TOKENS=<random-token>=<org>:<principal-id>:<role>` (comma-separated entries), `HTTP_ADDR=127.0.0.1:8095`. Never use these development identities for production.

```sh
python3 scripts/dev.py migrate
python3 scripts/dev.py seed
python3 scripts/dev.py test
python3 scripts/dev.py build
# Separate terminals:
python3 scripts/dev.py api
python3 scripts/dev.py worker
cd web && npm ci && npm run dev -- --host 127.0.0.1 --port 5175 --strictPort
```

Open `http://127.0.0.1:5175` on the development host and use an operator-provisioned opaque local token. The Vite proxy forwards authenticated requests to the API. The app does not expose tokens through a demo-login endpoint.

Synthetic identities are `org-fixture-a-requester`, `org-fixture-a-approver`, and equivalent principals in `org-fixture-b`. Seed records are explicitly simulated mail, documents and inventory. Seed does not reset modified records. Database migrations currently require the restricted owner role; runtime uses the separate app role. See build status for migration and production-auth limitations.

Verification performed: PostgreSQL-backed Go tests with race detection, actual River jobs applying all three simulator types and readback, Go vet/build, frontend tests/typecheck/build, and HTTP/proxy readiness checks. These are not full provider/browser end-to-end certification.

Source remains local; no public endpoint or GitHub remote is configured.
