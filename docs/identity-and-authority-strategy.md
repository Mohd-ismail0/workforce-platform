# Identity and authority: strategy

Established by reading the **deployed** instance rather than prose docs:

| Fact | Source |
|---|---|
| Issuer `https://auth.xsama.org/oidc` (ES384, PKCE S256) | `/.well-known/openid-configuration` → 200 |
| Management API under `<endpoint>/api`, OAuth2 client_credentials at `/oidc/token`, scope `all` | `/api/swagger.json` → 200 (openapi 3.0.1, 240 paths) |
| `GET /api/resources` omits scopes unless `includeScopes=true` | spec parameter list |
| `POST /api/applications` type enum includes `Traditional` (confidential) | spec schema |
| `PATCH`/`GET` use `/api/applications/{id}` — **not** `{applicationId}` | spec path list |

## The one thing that is genuinely blocked

No Logto Management API credential was found in the locations searched. Every
`LOGTO_*M2M*` match is in `vlobel-platform` and is an `.example`/test/docs file; the
browser credential vault is empty; no `~/.cloudflared` here, so the origin is not even
identifiable locally. That is "not found where I looked", not "does not exist" — a
credential could live somewhere I did not inspect.

This is a **human bootstrap step**, not something to work around. Editing Logto's
database or lifting a signed-in browser session would both be wrong: the first bypasses
the product's own authorization model, the second misrepresents a human's consent.
Neither is necessary — the credential is one console action.

**Ask (one action, then everything downstream is automatic):**

1. In the Logto console, create an M2M application `workforce-provisioner`.
2. Grant it the **Management API** role, scope `all`.
3. Write to `~/.config/workforce-platform/logto-management.env` (chmod 600):

```
LOGTO_ENDPOINT=https://auth.xsama.org
LOGTO_MANAGEMENT_CLIENT_ID=<id>
LOGTO_MANAGEMENT_CLIENT_SECRET=<secret>
LOGTO_MANAGEMENT_RESOURCE=<the Management API indicator>
```

**On that fourth line — this is the one genuinely ambiguous value.** The Management API
indicator is *not* the issuer URL, and the two candidates are:

| Deployment | Indicator |
|---|---|
| self-hosted (this instance) | `<endpoint>/api` |
| Logto Cloud | `https://default.logto.app/api` |

This instance's own spec documents the `resource=<endpoint>/api` form, so the script
**derives** `https://auth.xsama.org/api` when the variable is unset and prints a NOTE. It
does not require the variable, because the derivation is the right answer for a
self-hosted instance — but it names both candidates in the failure path, since getting
this wrong fails *identically to a bad secret*. Then: `python3 scripts/logto_provision.py`
(dry run) → `--apply` → run again, which must report *nothing to do*.

## Trust boundary

```
browser ──cookie──> workforce backend ──bearer──> workforce kernel
                          │                            │
                          └── Logto (who) ─────────────┘
                                                       └── kernel rows (what)
```

**Authentication answers "who". Authority answers "what", and lives in kernel rows.**
Organization membership, role, task ownership, approval jurisdiction and separation of
duties are never derived from a token claim. A token is a claim about identity, not a
grant of authority.

### Decisions

- **Confidential BFF, not a public SPA client.** The backend performs the
  authorization-code exchange and holds the secret; the browser never receives one.
  Session state is server-side. (An SPA registration would be right only if the browser
  called Logto directly, which this design does not do.)
- **Explicit `(issuer, subject)` → principal linking.** No auto-provisioning on first
  login, and *never* linking by email — email is not a stable identifier and is
  reassignable, so email-based linking is an account-takeover primitive. An unlinked
  identity authenticates and is still refused.
- **Global uniqueness on `(issuer, subject)`.** One external account maps to exactly one
  principal platform-wide, which is what prevents the same human becoming two different
  principals in two orgs by signing in twice.
- **Scopes are capabilities, not roles** (`read:work`, `write:work`, `approve:work`). A
  scope that encoded authority would move authority into the identity provider.
- **Verification delegates to `go-oidc`.** The first revision hand-rolled JWS parsing and
  got real things wrong: constructed the JWKS URL as `issuer + "/jwks"` instead of using
  discovery, ignored the JWK's own algorithm, and could trust a cached key indefinitely
  after a failed refresh. Policy (algorithm allowlist, audience, age bound) stays ours.
- **Production refuses local auth.** `AUTH_MODE=local` remains dev-only; there is no
  "either works" fallback.

## Order

1. Verifier + policy — **done**, hermetic tests green (discovery-driven fixture).
2. Provisioning — **written**, dry-run path verified, blocked on the credential above.
3. Identity linking + `AUTH_MODE=oidc` bearer path — **done** (`011_identity_links`,
   `identity_links` + `oidc_identity.go`, wired through `Server.auth`). Verified by unit
   tests; no live Logto token has been verified yet, because there is no credential.
4. Login/callback/session/cookies/CSRF — **not started.** The browser flow is the
   remaining half of identity; today `oidc` mode serves API clients that present a
   token.
5. Human-approved first real integration — not started.

## Corrections to earlier claims

- Sending mail is **not reversible**. An earlier draft called it "reversible"; what is
  actually true is that it is *narrow and allowlistable*. Retract the stronger claim.
- A dedicated unprivileged UID is **not** sufficient sandboxing. It reduces exposure; it
  does not contain a process. Keep it a separate workstream.
- Clearing OIDC does not clear "all blockers": real providers, notifications, scheduled
  lease recovery, runner containment and Gontext integration all remain.
