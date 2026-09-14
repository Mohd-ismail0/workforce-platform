# Identity and authority: strategy

Established by reading the **deployed** instance rather than prose docs:

| Fact | Source |
|---|---|
| Issuer `https://auth.xsama.org/oidc` (ES384, PKCE S256) | `/.well-known/openid-configuration` → 200 |
| Management API is served under the `/api` **path**, OAuth2 client_credentials at `/oidc/token`, scope `all` | `/api/swagger.json` → 200 (openapi 3.0.1, 240 paths) |
| The **audience** used to authenticate is a different value — see the edition table below | Logto "Interact with Management API" |
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

**On that fourth line — the value that is easy to get backwards.** The Management API
indicator is *not* the issuer URL. Logto's "Interact with Management API" documentation
states it differs by edition:

| Deployment | Indicator |
|---|---|
| self-hosted / OSS (**this instance**) | `https://default.logto.app/api` |
| Logto Cloud | `https://[tenant-id].logto.app/api` |

This was got wrong twice before being settled from that page: the deployed instance's
OpenAPI document contains a worked **Cloud** curl example (`resource=<endpoint>/api`),
which is not the self-hosted form, so reasoning from it produces the Cloud answer for a
self-hosted box. The script now defaults to the OSS identifier, remains overridable, and
names both candidates in its failure path — because a wrong audience fails *identically to
a bad client secret*, which is what made the wrong answer so easy to keep.

Then: `python3 scripts/logto_provision.py` (dry run) → `--apply` → run again, which must
report *nothing to do*.

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
- **"Could not read" is never "nothing is configured."** The provisioner refuses to write
  when an existing application's metadata cannot be read, and refuses to treat an
  unexpected response shape as an empty collection. Both would otherwise delete or
  duplicate registrations the script does not own. A create whose response never arrives
  is reconciled by exact identity, precisely because the write may have succeeded.
- **A test that lies is worse than no test.** The intermittent verifier failures were my
  own fixture publishing malformed EC keys: `big.Int.Bytes()` drops leading zero bytes, so
  ~1.5% of generated P-384 coordinates encoded to 47 bytes instead of 48. The first
  explanation (shared connection pool) was tested and disproven, and the comment asserting
  it was corrected rather than deleted.

## Verified development topology (input to the browser flow)

Read from `web/vite.config.ts` and `README.md`, not assumed:

| Piece | Where it actually runs |
|---|---|
| SPA (Vite dev server) | `127.0.0.1:5175` |
| Backend / API | `127.0.0.1:8095` |
| Proxy | only `/api`, `/health`, `/ready` forward 5175 → 8095 |

Two consequences that decide the unbuilt session work:

- **The callback belongs to the backend**, so `http://127.0.0.1:8095/auth/callback` is the
  correct registration for a BFF. The browser navigates to the backend directly; the proxy
  is irrelevant to that hop. (The route does not exist yet — it is registered ahead of the
  handler, which is normal, and the provisioner's own output says so.)
- **Cookies are scoped to the host, not the port.** A session cookie set by 8095 on
  `127.0.0.1` is therefore sent by the SPA running on 5175, and its proxied `/api` calls
  carry it through. That is what makes the BFF workable unchanged in dev, and it is also why
  `SameSite` and an explicit CSRF defence are required rather than optional: the cookie is
  attached to cross-port requests automatically.

## Order

1. Verifier + policy — **done.** Verification delegates to `github.com/coreos/go-oidc`;
   tests run against a fixture that serves a real discovery document, and pass under
   `-race -count=50` with no data race.
2. Provisioning — **written and contract-tested (38/38 against a mock Management API),
   never executed against real Logto**, blocked on the credential above. Writing those
   tests found two real defects: the success path crashed on a fresh host after already
   mutating the tenant (it never created its own config directory), and pagination
   compared each page against a hardcoded size, which can stop early and miss an existing
   object — the condition that produces duplicates.
3. Identity linking + `AUTH_MODE=oidc` bearer path — **done** (`011_identity_links`,
   `identity_links` + `oidc_identity.go`, wired through `Server.auth`). Verified by unit
   tests; no live Logto token has been verified yet, because there is no credential.
4. Login/callback/session/cookies/CSRF — **implemented and tested** in the BFF foundation
   (`012_bff_sessions`, `oauth.go`, `bff.go`). The browser flow is still not live against
   Logto: provisioning, a real token exchange and a real human login remain unverified.
5. Human-approved first real integration — not started.

## Corrections to earlier claims

- Sending mail is **not reversible**. An earlier draft called it "reversible"; what is
  actually true is that it is *narrow and allowlistable*. Retract the stronger claim.
- A dedicated unprivileged UID is **not** sufficient sandboxing. It reduces exposure; it
  does not contain a process. Keep it a separate workstream.
- Clearing OIDC does not clear "all blockers": real providers, notifications, scheduled
  lease recovery, runner containment and Gontext integration all remain.
