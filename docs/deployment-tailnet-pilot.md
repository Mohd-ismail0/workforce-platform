# Pilot deployment — Tailnet-only

Decided: the pilot is reachable **only on the Tailnet**. No public hostname, no
Cloudflare Tunnel ingress, no exposure to the internet. That is a deliberate
scope limit for a first pilot, not a claim that the platform is ready for public
traffic.

## The shape

**One process, one origin, one port.**

The API serves the built single-page app from the same origin
(`WORKFORCE_UI_DIR`), so there is no separate web server and no cross-origin
proxying. This matters beyond tidiness: the browser identity flow uses an opaque
`__Host-` session cookie, which only works on one origin. Running the UI on a
separate dev-server port requires a proxy and careful cookie configuration, and
getting that wrong is exactly what broke sign-in once already.

| Component | Where | Notes |
|---|---|---|
| API + UI | one process, one port | stateless; Tailnet-reachable |
| Worker | separate process | shares the database and the River queue |
| PostgreSQL | existing shared LXC | dedicated `workforce_platform` database + roles |
| Object storage | not used yet | needed when attachments/artifacts arrive |

Stateful services stay outside the application host, matching the existing
homelab pattern: the database is not in the application's lifecycle.

## Build and run

```sh
# 1. Build the single-page app. Its output is what WORKFORCE_UI_DIR points at.
npm --prefix web ci
npm --prefix web run build

# 2. Migrations run as the migration owner, separately from the app.
python3 scripts/dev.py migrate

# 3. API and worker as two processes, with the operator's secret files.
WORKFORCE_UI_DIR="$PWD/web/dist" python3 scripts/dev.py api
python3 scripts/dev.py worker
```

The binary serves **only the API** when `WORKFORCE_UI_DIR` is unset, and answers
`503` with an explanatory message when the directory is set but empty. It does not
pretend to have a UI it was not given.

## What the single origin does, verifiably

Verified against a running instance:

| Request | Result |
|---|---|
| `GET /` | `200 text/html` — the app shell |
| `GET /tasks/abc123` | `200 text/html` — the shell, because a client route is not a file |
| `GET /assets/index-*.css` | `200 text/css` — a real asset is served as itself |
| `GET /health` | `200 application/json` |
| `GET /api/v1/me` without a token | `401` |
| `GET /api/v1/definitely-not-a-route` | JSON, never the shell |

The API is never answered with HTML: an unknown `/api/` path returning an index
page would look like a routing success while the caller silently received the
wrong thing. Path traversal outside the UI directory is refused.

## Exposing it on the Tailnet

1. Bring the Tailnet up on the host (`tailscale up`), and confirm the host's
   Tailnet address.
2. Bind the API to the Tailnet address rather than all interfaces:
   `HTTP_ADDR=<tailnet-ip>:8095`. Binding to `0.0.0.0` would also expose it on
   any other reachable network, which is not the intent.
3. Confirm reachability **from another Tailnet machine**, not from this host.
   Loopback working is not evidence that a colleague can reach it.
4. Do **not** add a Cloudflare Tunnel ingress route. If one is added later, that
   is a deliberate decision to become publicly reachable, and it should be made
   as one rather than inherited from a leftover config.

## Not yet true — do not imply otherwise

- **No real provider is connected.** Every integration is still a simulator, so
  no mail is sent, no stock moves and no document changes. The pilot exercises
  the platform's own loop, not a supplier's systems.
- **No second human approver is onboarded.** The requester/approver separation
  has never been demonstrated by two real people, so the pilot cannot yet show
  the full authority chain with real users.
- **Model calls may leave the machine** if an operator configures a real harness;
  the built-in runner is deterministic and calls no model.
- **No backups or restore drill** have been performed for the pilot database.
  That is required before anything real is stored.
- **Runner isolation is not built.** The decided shape is a dedicated LXC (agents
  are untrusted code); until it exists, a real agent runs on the application host.
