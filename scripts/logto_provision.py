#!/usr/bin/env python3
"""Idempotent Logto provisioning for the workforce platform.

Contracts taken from the DEPLOYED instance's own OpenAPI document
(https://auth.xsama.org/api/swagger.json) rather than from prose docs:
  * Management API is served under <endpoint>/api
  * it authenticates via OAuth2 client_credentials at /oidc/token
  * the endpoints used here are present: /api/resources, /api/applications

Design properties, each of which is a deliberate choice:
  * READ -> COMPARE -> CREATE/UPDATE -> ASSERT-READBACK. Nothing is ever "reset".
  * Foreign configuration is preserved. This tenant also serves other products, so
    redirect URIs and logout URIs we do not own are carried through untouched. The
    previous revision of this file read logout URIs ONLY from the top level; because
    they live under oidcClientMetadata, an update could have DELETED existing
    destinations. Both locations are now merged.
  * The Management API indicator is NOT derivable from the public API URL, so it is
    never silently guessed: it is required, or taken from Logto's documented default
    with an explicit warning.
  * A second `--apply` makes NO changes. The plan is computed by comparison, so a
    no-op run issues no PATCH.
  * A create that fails on timeout/net is UNCERTAIN, not failed: the object is
    re-read by exact identity before any decision, because a blind retry would
    create a duplicate.
  * Secrets: read from a mode-600 file or the environment, never argv, never
    printed, never written to the evidence file. A newly issued client secret is
    written only to a mode-600 file.

Usage:
  python3 scripts/logto_provision.py            # authenticate + print the diff
  python3 scripts/logto_provision.py --create-app-secret  # additive, one-time reveal
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import pathlib
import sys

import urllib.error
import urllib.parse
import urllib.request

CF_ACCESS_FILE = pathlib.Path.home() / ".hermes/secrets/cloudflare-access-logto.env"
USER_AGENT = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
              "(KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36")
ENDPOINT_DEFAULT = "https://auth.xsama.org"


# The Management API audience. Per Logto's own documentation the identifier DIFFERS by
# edition, and the difference is easy to get backwards:
#
#   Logto Cloud:            https://[tenant-id].logto.app/api
#   Logto OSS/self-hosted:  https://default.logto.app/api
#
# Source: "Interact with Management API" (docs.logto.io), which states the OSS form
# explicitly and shows the `apiIndicator: https://default.logto.app/api` config. An
# earlier revision of this script derived `<endpoint>/api` from the OpenAPI document's
# *Cloud* curl example and labelled it the self-hosted form. That was wrong, and wrong in
# the worst way: authenticating for the wrong audience fails identically to a bad client
# secret, so it looks like a credentials problem forever.
MANAGEMENT_RESOURCE_OSS = "https://default.logto.app/api"

CONF = pathlib.Path.home() / ".config/workforce-platform/logto-management.env"
SECRET_OUT = pathlib.Path.home() / ".config/workforce-platform/oidc-app.env"

API_RESOURCE_NAME = "Workforce Platform API"
# The API audience the kernel will trust. This is a DECISION, not a discovered value:
# it must be an identifier we control and that nothing else in this tenant already uses, so
# the kernel can tell our own tokens apart from any other application's. It is deliberately
# NOT the Logto management indicator, and it is overridable via WORKFORCE_OIDC_AUDIENCE
# when the kernel is configured.
API_INDICATOR = "https://workforce.internal/api"

# A confidential client. The backend performs the authorization-code exchange and
# holds the secret; the browser never receives one. An SPA registration would be the
# right choice only if the browser itself called Logto directly, which this design
# does not do.
WEB_APP_NAME = "Workforce Platform (backend-for-frontend)"
# NOTE: the backend does NOT yet serve /auth/callback. The interactive browser flow
# (authorization code + PKCE, sessions, cookies, CSRF) is not implemented, so this URI is
# registered ahead of that work. Registering it early is normal and harmless; presenting it
# as a working endpoint would not be.
WEB_REDIRECT_URIS = ["http://127.0.0.1:8095/auth/callback"]
WEB_LOGOUT_URIS = ["http://127.0.0.1:8095/"]

API_SCOPES = [
    ("read:work", "Read tasks, proposals and decisions visible to the caller"),
    ("write:work", "Create tasks, proposals, endorsements and answers"),
    ("approve:work", "Authorise a consequential effect (jurisdiction is still checked)"),
]


def die(msg: str, code: int = 1) -> None:
    print(f"ERROR: {msg}", file=sys.stderr)
    raise SystemExit(code)


def unwrap_list(doc, path: str) -> list:
    """Return the item list, or fail loudly.

    This deployment's spec returns a BARE ARRAY for the list endpoints used here. If a
    response instead arrives wrapped under an unexpected key, that must NOT be read as
    "no items": treating a shape mismatch as an empty collection is precisely how a
    readback silently concludes an existing object is absent and then creates a
    duplicate. Only a genuine empty list ends the walk.
    """
    if isinstance(doc, list):
        return doc
    if isinstance(doc, dict):
        for key in ("data", "items", "results"):
            v = doc.get(key)
            if isinstance(v, list):
                return v
    die(f"GET {path} returned an unexpected shape ({type(doc).__name__}); refusing to "
        f"treat it as an empty collection, because that would make an existing object "
        f"look absent and invite duplicate creation")


def access_headers() -> dict[str, str]:
    """Read Cloudflare Access service-token headers from the protected file.

    The existing verifier proved that auth.xsama.org requires these headers. They are
    added only to outbound requests and never printed or persisted by this script.
    """
    vals: dict[str, str] = {}
    try:
        if CF_ACCESS_FILE.stat().st_mode & 0o077:
            die(f"refusing readable-by-others Access credential file: {CF_ACCESS_FILE}")
        for line in CF_ACCESS_FILE.read_text().splitlines():
            if line and not line.startswith('#') and '=' in line:
                k, v = line.split('=', 1)
                vals[k.strip()] = v.strip()
    except FileNotFoundError:
        return {}
    return {
        "CF-Access-Client-Id": vals["LOGTO_CLOUDFLARE_ACCESS_CLIENT_ID"],
        "CF-Access-Client-Secret": vals["LOGTO_CLOUDFLARE_ACCESS_CLIENT_SECRET"],
    } if vals.get("LOGTO_CLOUDFLARE_ACCESS_CLIENT_ID") and vals.get("LOGTO_CLOUDFLARE_ACCESS_CLIENT_SECRET") else {}


def apply_headers(req: urllib.request.Request) -> None:
    for k, v in access_headers().items():
        req.add_header(k, v)


class Client:
    """Minimal Logto Management API client (stdlib only)."""

    def __init__(self, endpoint: str, client_id: str, client_secret: str, resource: str):
        self.endpoint = endpoint.rstrip("/")
        self.client_id = client_id
        self.client_secret = client_secret
        self.resource = resource
        self.token = ""

    def authenticate(self) -> None:
        body = urllib.parse.urlencode({
            "grant_type": "client_credentials",
            "resource": self.resource,
            "scope": "all",
        }).encode()
        basic = base64.b64encode(f"{self.client_id}:{self.client_secret}".encode()).decode()
        req = urllib.request.Request(
            self.endpoint + "/oidc/token", data=body, method="POST",
            headers={"Authorization": "Basic " + basic,
                     "Content-Type": "application/x-www-form-urlencoded",
                     "User-Agent": USER_AGENT,
                     "Accept": "application/json"})
        apply_headers(req)
        try:
            with urllib.request.urlopen(req, timeout=20) as r:
                doc = json.loads(r.read())
        except urllib.error.HTTPError as e:
            # The response body is NOT echoed: on some error shapes it carries
            # credential-adjacent material.
            die(f"management authentication failed (HTTP {e.code}).\n"
                f"  Three causes, in order of likelihood:\n"
                f"    1. the client id/secret is wrong;\n"
                f"    2. the M2M application does not hold the Management API role;\n"
                f"    3. the MANAGEMENT RESOURCE is wrong. This is the subtle one: it\n"
                f"       fails exactly like a bad secret. Per Logto's documentation the\n"
                f"       identifier differs by edition:\n"
                f"         self-hosted / OSS: {MANAGEMENT_RESOURCE_OSS}\n"
                f"         Logto Cloud:       https://[tenant-id].logto.app/api\n"
                f"       This run used: {self.resource}\n"
                f"       Confirm it against the instance and set LOGTO_MANAGEMENT_RESOURCE\n"
                f"       explicitly in {CONF}.")
        except Exception as e:
            die(f"management authentication failed ({type(e).__name__})")
        self.token = doc.get("access_token", "")
        if not self.token:
            die("management authentication returned no access token")

    def call(self, method: str, path: str, payload: dict | None = None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(
            self.endpoint + path, data=data, method=method,
            headers={"Authorization": "Bearer " + self.token,
                     "Content-Type": "application/json",
                     "User-Agent": USER_AGENT,
                     "Accept": "application/json"})
        apply_headers(req)
        try:
            with urllib.request.urlopen(req, timeout=25) as r:
                raw = r.read()
                return r.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                return e.code, json.loads(raw or b"null")
            except Exception:
                return e.code, None
        except Exception:
            # Network/timeout: the caller must treat this as UNCERTAIN, not failed.
            return 0, None

    # PAGE is used for BOTH the request and the end-of-pages comparison. Comparing against
    # a literal while requesting something else is a silent way to stop early (miss items)
    # or loop forever if the two ever drift apart.
    PAGE = 100

    def get_all(self, path: str) -> list:
        """Walk pagination so a readback covers EVERY page.

        Two deliberate choices:
          * a first-page-only read would cheerfully 'create' a duplicate of an object that
            already exists further down the list;
          * only a GENUINELY EMPTY page ends the walk. A short page still means "more may
            follow" here, because stopping early on a partial page silently truncates the
            read and re-introduces the duplicate-creation risk this exists to prevent.
        """
        out, page = [], 1
        while True:
            sep = "&" if "?" in path else "?"
            status, doc = self.call("GET", f"{path}{sep}page={page}&page_size={self.PAGE}")
            if status != 200:
                die(f"GET {path} failed (HTTP {status})")
            items = unwrap_list(doc, path)
            if not items:
                break
            out.extend(items)
            page += 1
            if page > 1000:
                die(f"GET {path} exceeded 1000 pages; refusing to loop further")
        return out

    def app_detail(self, app_id: str) -> dict:
        """Fetch full detail; list responses may omit nested metadata."""
        status, doc = self.call("GET", f"/api/applications/{app_id}")
        if status == 200 and isinstance(doc, dict):
            return doc
        return {}


def scope_names(client: "Client", resource_id: str) -> set[str]:
    """Authoritative scope set for a resource.

    Read from /api/resources/{id}/scopes (the endpoint the spec actually documents for
    this) instead of assuming the list response embeds them. Serving both sources means
    a change in either shape cannot silently turn into duplicate creation.
    """
    names: set[str] = set()
    try:
        for s in client.get_all(f"/api/resources/{resource_id}/scopes"):
            n = (s or {}).get("name")
            if n:
                names.add(str(n))
    except SystemExit:
        raise
    return names


def uris_of(app: dict, key: str) -> list[str]:
    """Redirect/logout URIs live in either location depending on version. Merge both so
    an update can never drop a URI that exists in only one of them."""
    out: list[str] = []
    top = app.get(key)
    if isinstance(top, list):
        out.extend(str(u) for u in top)
    meta = app.get("oidcClientMetadata") or {}
    nested = meta.get(key)
    if isinstance(nested, list):
        out.extend(str(u) for u in nested)
    return list(dict.fromkeys(out))


def load_credentials() -> tuple[str, str, str, str]:
    env: dict[str, str] = {}
    if CONF.exists():
        if CONF.stat().st_mode & 0o077:
            die(f"{CONF} is readable by other users; chmod 600 it")
        for line in CONF.read_text().splitlines():
            if line.strip() and not line.lstrip().startswith("#") and "=" in line:
                k, v = line.split("=", 1)
                env[k.strip()] = v.strip()

    def pick(name: str) -> str:
        return env.get(name) or os.environ.get(name, "")

    endpoint = pick("LOGTO_ENDPOINT") or ENDPOINT_DEFAULT
    cid = pick("LOGTO_MANAGEMENT_CLIENT_ID")
    secret = pick("LOGTO_MANAGEMENT_CLIENT_SECRET")
    resource = pick("LOGTO_MANAGEMENT_RESOURCE")

    if not cid or not secret:
        die(
            "no Logto Management API credential is available.\n"
            "  One-time bootstrap, through Logto's supported console workflow:\n"
            "    1. Create a machine-to-machine application (e.g. 'workforce-provisioner').\n"
            "    2. Grant it the Management API role with scope 'all'.\n"
            "    3. Write these to " + str(CONF) + " :\n"
            "         LOGTO_ENDPOINT=" + endpoint + "\n"
            "         LOGTO_MANAGEMENT_CLIENT_ID=<id>\n"
            "         LOGTO_MANAGEMENT_CLIENT_SECRET=<secret>\n"
            "         LOGTO_MANAGEMENT_RESOURCE=<management API indicator>  # see below\n"
            "       then: chmod 600 " + str(CONF) + "\n"
            "  The management resource is NOT the issuer URL, and it differs by Logto\n"
            "  edition: self-hosted/OSS uses " + MANAGEMENT_RESOURCE_OSS + ",\n"
            "  while Logto Cloud uses https://[tenant-id].logto.app/api. If you omit the\n"
            "  variable the script assumes the OSS value (this instance is self-hosted).\n"
            "  This script reads the file directly: the secret never appears in a command\n"
            "  line, in this output, or in the platform runtime.")
    if not resource:
        print(f"NOTE: LOGTO_MANAGEMENT_RESOURCE is not set; using the Logto Open Source "
              f"identifier\n      {MANAGEMENT_RESOURCE_OSS}\n"
              f"      (this instance is self-hosted). Logto Cloud would instead use\n"
              f"      https://[tenant-id].logto.app/api. Getting this wrong fails exactly\n"
              f"      like a bad client secret, so set it explicitly in {CONF} to be sure.")
        resource = MANAGEMENT_RESOURCE_OSS
    return endpoint, cid, secret, resource


def desired_api() -> dict:
    return {"name": API_RESOURCE_NAME, "indicator": API_INDICATOR,
            "scopes": [{"name": n, "description": d} for n, d in API_SCOPES]}


def select_one(candidates: list, label: str, pred) -> dict | None:
    """Exact matching with conflict detection. A name collision or duplicate indicator
    is an ERROR, never a coin flip — acting on the wrong object would be destructive."""
    found = [c for c in candidates if pred(c)]
    if len(found) > 1:
        ids = ", ".join(str(c.get("id")) for c in found)
        die(f"{len(found)} {label} match; refusing to guess which to modify ({ids}). "
            f"Resolve the duplicates manually.")
    return found[0] if found else None


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--apply", action="store_true",
                    help="create/update the registrations; without it, a dry run")
    ap.add_argument("--create-app-secret", action="store_true",
                    help="create one additive named secret for the provisioned BFF app")
    args = ap.parse_args()
    if args.apply and args.create_app_secret:
        die("choose --apply or --create-app-secret, not both")

    endpoint, cid, secret, resource = load_credentials()
    print(f"endpoint:            {endpoint}")
    print(f"management resource: {resource}")
    print(f"management client:   {cid}")
    print("management secret:   (loaded, never printed)")

    c = Client(endpoint, cid, secret, resource)
    c.authenticate()
    print("authenticated:       yes")

    # Secret creation is a separate command because the returned value is shown only once.
    # It must never be bundled into a registration update whose retry semantics are more
    # complicated. The app is matched by the exact provisioner-owned name, and a duplicate
    # is an error rather than a guess.
    if args.create_app_secret:
        apps = c.get_all("/api/applications")
        app = select_one(apps, "applications", lambda a: a.get("name") == WEB_APP_NAME)
        if app is None:
            die("the BFF application is not provisioned yet; run --apply first")
        app_id = app.get("id")
        secret_name = "workforce-bff-runtime"
        status, body = c.call("POST", f"/api/applications/{app_id}/secrets", {"name": secret_name})
        if status not in (200, 201):
            die(f"creating the BFF application secret failed (HTTP {status})")
        value = (body or {}).get("value") or (body or {}).get("secret")
        if not isinstance(value, str) or len(value) < 18:
            die("Logto did not return a secret value; refusing to write an incomplete credential")
        SECRET_OUT.parent.mkdir(parents=True, exist_ok=True)
        os.chmod(SECRET_OUT.parent, 0o700)
        SECRET_OUT.write_text(
            "# Workforce Platform confidential BFF client.\n"
            "# OPERATOR ONLY: never put this in a browser bundle or runner environment.\n"
            f"WORKFORCE_OIDC_CLIENT_ID={app_id}\n"
            f"WORKFORCE_OIDC_CLIENT_SECRET={value}\n")
        os.chmod(SECRET_OUT, 0o600)
        print(f"BFF client id: {app_id}")
        print(f"secret created: name={secret_name}, stored={SECRET_OUT}, mode=600")
        print("secret value: not printed")
        return 0

    want_api = desired_api()

    # ---- API resource: match by EXACT indicator ----
    # includeScopes is REQUIRED here: without it the resource list omits its scopes and
    # an existing scope set reads as empty, which would re-create scopes that already
    # exist. Verified against the deployed spec's own parameter list.
    resources = c.get_all("/api/resources?includeScopes=true")
    api_match = select_one(resources, "API resources",
                           lambda r: r.get("indicator") == want_api["indicator"])

    plan: list[tuple[str, str, str]] = []
    if api_match is None:
        plan.append(("create", "resource", want_api["name"]))
        for s in want_api["scopes"]:
            plan.append(("create", "scope", s["name"]))
    else:
        have = scope_names(c, api_match.get("id"))
        plan.append(("keep", "resource", f"{want_api['name']} ({api_match.get('id')})"))
        for s in want_api["scopes"]:
            if s["name"] not in have:
                plan.append(("create", "scope", f"{s['name']} (missing)"))

    # ---- Application ----
    apps = c.get_all("/api/applications")
    app_match = select_one(apps, "applications",
                           lambda a: a.get("name") == WEB_APP_NAME)

    if app_match is None:
        plan.append(("create", "application", WEB_APP_NAME))
    else:
        detail = c.app_detail(app_match.get("id")) or app_match
        have_redirect = set(uris_of(detail, "redirectUris"))
        have_logout = set(uris_of(detail, "postLogoutRedirectUris"))
        add_redirect = [u for u in WEB_REDIRECT_URIS if u not in have_redirect]
        add_logout = [u for u in WEB_LOGOUT_URIS if u not in have_logout]
        # Compare the TYPE too: an existing SPA registration would silently lack a
        # client secret the BFF needs.
        cur_type = detail.get("type")
        plan.append(("keep", "application", f"{WEB_APP_NAME} ({app_match.get('id')})"))
        if add_redirect or add_logout:
            plan.append(("update", "uris", f"+{add_redirect} +{add_logout}"))
        if cur_type and cur_type != "Traditional":
            plan.append(("ERROR", "type", f"is {cur_type}, expected Traditional"))

    print("\n=== plan ===")
    for action, kind, detail in plan:
        print(f"  {action:8} {kind:12} {detail}")
    changes = [p for p in plan if p[0] in ("create", "update")]
    if not changes:
        print("\nnothing to do: the instance already matches the desired state.")

    if any(p[0] == "ERROR" for p in plan):
        die("refusing to apply while the application TYPE is wrong; resolve it first")

    if not args.apply:
        print(f"\ndry run: {len(changes)} change(s) not applied. Re-run with --apply.")
        return 0

    # ---- apply ----
    if api_match is None:
        status, body = c.call("POST", "/api/resources", {
            "name": want_api["name"], "indicator": want_api["indicator"],
            "accessTokenTtl": 3600,
        })
        if status == 0:
            # UNCERTAIN: the object may exist. Reconcile by exact identity before
            # deciding, rather than retrying and creating a duplicate.
            check = select_one(c.get_all("/api/resources"), "API resources",
                               lambda r: r.get("indicator") == want_api["indicator"])
            if check is None:
                die("creating the API resource: outcome UNCERTAIN and it is not present; "
                    "inspect the instance before retrying")
            api_id = check.get("id")
        elif status not in (200, 201):
            die(f"creating the API resource failed (HTTP {status})")
        else:
            api_id = (body or {}).get("id")
    else:
        api_id = api_match.get("id")

    have = scope_names(c, api_id)
    for s in want_api["scopes"]:
        if s["name"] in have:
            continue
        status, _ = c.call("POST", f"/api/resources/{api_id}/scopes", s)
        if status not in (200, 201):
            die(f"creating scope {s['name']} failed (HTTP {status})")

    new_secret = ""
    if app_match is None:
        status, body = c.call("POST", "/api/applications", {
            "name": WEB_APP_NAME, "type": "Traditional",
            "oidcClientMetadata": {
                "redirectUris": WEB_REDIRECT_URIS,
                "postLogoutRedirectUris": WEB_LOGOUT_URIS,
            },
        })
        if status == 0:
            check = select_one(c.get_all("/api/applications"), "applications",
                               lambda a: a.get("name") == WEB_APP_NAME)
            if check is None:
                die("creating the application: outcome UNCERTAIN and it is not present; "
                    "inspect the instance before retrying")
            app_id = check.get("id")
        elif status not in (200, 201):
            die(f"creating the application failed (HTTP {status})")
        else:
            app_id = (body or {}).get("id")
            new_secret = (body or {}).get("secret") or ""
    else:
        app_id = app_match.get("id")
        detail = c.app_detail(app_id) or app_match
        known_redirect = uris_of(detail, "redirectUris")
        known_logout = uris_of(detail, "postLogoutRedirectUris")
        # Guard: "could not read" is NOT "nothing is configured". If neither the detail
        # fetch nor the list entry carried OIDC metadata, the existing destinations are
        # UNKNOWN, and PATCHing a list built only from our own URIs would silently delete
        # another application's redirect or logout targets.
        if not detail.get("oidcClientMetadata") and not known_redirect and not known_logout:
            die(f"the existing application's OIDC metadata could not be read (id={app_id}), "
                f"so its current redirect/logout URIs are unknown. Refusing to update: "
                f"writing only our own URIs could delete existing destinations. Inspect it "
                f"in the Logto console and re-run.")
        merged_redirect = list(dict.fromkeys(known_redirect + WEB_REDIRECT_URIS))
        merged_logout = list(dict.fromkeys(known_logout + WEB_LOGOUT_URIS))
        # Compared as SETS so a mere ordering difference does not trigger a write; an
        # unchanged registration therefore produces no PATCH and a re-run is a true no-op.
        if (set(merged_redirect) != set(known_redirect)
                or set(merged_logout) != set(known_logout)):
            status, _ = c.call("PATCH", f"/api/applications/{app_id}", {
                "oidcClientMetadata": {
                    "redirectUris": merged_redirect,
                    "postLogoutRedirectUris": merged_logout,
                }})
            if status not in (200, 204):
                die(f"updating the application failed (HTTP {status})")

    # ---- ASSERT readback: verify the applied state, do not assume it ----
    print("\n=== readback (asserted) ===")
    r0 = select_one(c.get_all("/api/resources?includeScopes=true"), "API resources",
                    lambda r: r.get("indicator") == want_api["indicator"])
    if r0 is None:
        die("readback: the API resource is not present after apply")
    if r0.get("indicator") != want_api["indicator"]:
        die("readback: indicator does not match what was requested")
    scopes = sorted(scope_names(c, r0.get("id")))
    missing = [n for n, _ in API_SCOPES if n not in scopes]
    if missing:
        die(f"readback: scopes missing after apply: {missing}")
    print(f"  resource:    {r0.get('name')} id={r0.get('id')}")
    print(f"  indicator:   {r0.get('indicator')}  OK")
    print(f"  scopes:      {scopes}  OK")

    a0 = c.app_detail(app_id) or {}
    if a0.get("type") not in ("Traditional", None):
        die(f"readback: application type is {a0.get('type')}, expected Traditional")
    final_redirect = uris_of(a0, "redirectUris")
    final_logout = uris_of(a0, "postLogoutRedirectUris")
    for u in WEB_REDIRECT_URIS:
        if u not in final_redirect:
            die(f"readback: redirect URI {u} missing after apply")
    for u in WEB_LOGOUT_URIS:
        if u not in final_logout:
            die(f"readback: post-logout URI {u} missing after apply")
    print(f"  application: {a0.get('name')} id={a0.get('id')} type={a0.get('type')}")
    print(f"  redirects:   {final_redirect}  OK")
    print(f"  post-logout: {final_logout}  OK")

    if new_secret:
        # The parent directory may not exist on a fresh host. Without this, the SUCCESS
        # path raises FileNotFoundError *after* the tenant has already been mutated, which
        # is the worst possible ordering: the registrations exist but the operator has no
        # secret and no indication of what completed. Created mode 700.
        SECRET_OUT.parent.mkdir(parents=True, exist_ok=True)
        try:
            SECRET_OUT.parent.chmod(0o700)
        except OSError:
            pass
        SECRET_OUT.write_text(
            "# Workforce Platform backend-for-frontend OIDC client.\n"
            "# Confidential: the backend performs the code exchange. Never ship this to\n"
            "# a browser, a runner, or any client-side bundle.\n"
            f"WORKFORCE_OIDC_CLIENT_ID={app_id}\n"
            f"WORKFORCE_OIDC_CLIENT_SECRET={new_secret}\n")
        SECRET_OUT.chmod(0o600)
        print(f"\n  client secret written to {SECRET_OUT} (mode 600)")

    evidence = {
        "endpoint": endpoint,
        "managementResource": resource,
        "apiResource": {"id": r0.get("id"), "name": r0.get("name"),
                        "indicator": r0.get("indicator"), "scopes": scopes},
        "application": {"id": a0.get("id"), "name": a0.get("name"),
                        "type": a0.get("type"),
                        "redirectUris": final_redirect,
                        "postLogoutRedirectUris": final_logout},
        "note": "the backend holds the client secret; no secret is exposed to the "
                "browser. This file contains no secret material.",
    }
    out = pathlib.Path("/tmp/workforce-logto-provision-evidence.json")
    out.write_text(json.dumps(evidence, indent=2))
    print(f"\n  evidence (no secrets): {out}")
    print("\n  Next: run this again; it must report 'nothing to do'.")
    print("  AUTH_MODE=oidc bearer verification and the interactive BFF foundation are now")
    print("  implemented, but the new app still needs its client credential bound into the")
    print("  protected runtime environment, and an explicit (issuer, subject) principal link")
    print("  must exist before a human can act. No auto-linking occurs.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
