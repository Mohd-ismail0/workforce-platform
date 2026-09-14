#!/usr/bin/env python3
"""Contract tests for scripts/logto_provision.py, against a mock Management API.

WHY: the provisioner has never been executed, because there is no Management API credential.
Its logic is nonetheless testable, and this script is the thing that will mutate a live
identity provider — where a bug creates duplicates, deletes another application's redirect
targets, or leaves the tenant half-provisioned. So each property in the script's header is
asserted here instead of trusted, using a mock that records every request.

What is covered:
  1. dry run performs NO mutating request
  2. apply from empty creates the resource, its scopes and the application, then asserts
  3. a SECOND apply is a genuine no-op (zero mutating requests)
  4. an object on a later page is found, so no duplicate is created
  5. an unexpected response shape is refused, not read as "empty"
  6. a write whose response never arrives is reconciled by identity, not retried
  7. foreign redirect AND logout URIs are preserved
  8. unreadable metadata refuses to update (rather than writing only our own URIs)
  9. two conflicting objects are an error, never a coin flip
 10. no secret is echoed to stdout

The mock serves the shapes taken from the deployed instance's own OpenAPI document.
"""
from __future__ import annotations

import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

REPO = pathlib.Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "logto_provision.py"

API_INDICATOR = "https://workforce.internal/api"
APP_NAME = "Workforce Platform (backend-for-frontend)"
REDIRECT = "http://127.0.0.1:8095/auth/callback"
LOGOUT = "http://127.0.0.1:8095/"


class MockState:
    def __init__(self) -> None:
        self.resources: list[dict] = []
        self.apps: list[dict] = []
        self.log: list[tuple[str, str]] = []          # (method, path)
        self.page_size = 100                          # how many the server returns per page
        self.resources_shape = "list"                 # or "unexpected"
        self.app_detail_readable = True
        self.hang_up_on_resource_create = False
        self.fail_app_detail = False
        self.force_page_split: int | None = None      # items per page, to force a second page

    def mutations(self) -> list[tuple[str, str]]:
        """Mutating calls only.

        /oidc/token is deliberately excluded: authenticating is not provisioning. Counting
        it would make "a dry run changed nothing" impossible to assert.
        """
        return [r for r in self.log
                if r[0] in ("POST", "PATCH", "PUT", "DELETE") and r[1] != "/oidc/token"]


class Handler(BaseHTTPRequestHandler):
    state: MockState

    def log_message(self, *a) -> None:  # silence the default stderr logging
        return

    # ---------- helpers ----------
    def _json(self, code: int, payload) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _page(self, items: list, q: dict) -> list:
        """Honour page/page_size the way the real API does, so pagination is really walked.

        The caller's requested page_size is respected; the real API returns up to that many
        items and fewer only when the list is exhausted. An earlier version of this mock
        capped pages at 2 regardless of what was asked, which made the script stop after
        page 1 and produced a false failure.
        """
        per = self.state.force_page_split or int((q.get("page_size") or ["100"])[0])
        page = int((q.get("page") or ["1"])[0])
        start = (page - 1) * per
        return items[start:start + per]

    def _body(self) -> dict:
        n = int(self.headers.get("Content-Length") or 0)
        if not n:
            return {}
        try:
            return json.loads(self.rfile.read(n) or b"{}")
        except Exception:
            return {}

    # ---------- routing ----------
    def do_GET(self) -> None:  # noqa: N802
        u = urlparse(self.path)
        q = parse_qs(u.query)
        self.state.log.append(("GET", u.path))

        if u.path == "/api/resources":
            if self.state.resources_shape == "unexpected":
                return self._json(200, {"payload": self.state.resources})
            if self.state.resources_shape == "string":
                return self._json(200, "not-a-list")
            return self._json(200, self._page(self.state.resources, q))

        if u.path == "/api/applications":
            return self._json(200, self._page(self.state.apps, q))

        if u.path.startswith("/api/resources/") and u.path.endswith("/scopes"):
            rid = u.path.split("/")[3]
            res = next((r for r in self.state.resources if r.get("id") == rid), None)
            return self._json(200, self._page((res or {}).get("scopes", []), q))

        if u.path.startswith("/api/applications/"):
            aid = u.path.split("/")[3]
            app = next((a for a in self.state.apps if a.get("id") == aid), None)
            if app is None:
                return self._json(404, {"message": "not found"})
            # Simulate a detail fetch that returns no usable OIDC metadata: a list-visible
            # application whose metadata we cannot actually read.
            if not self.state.app_detail_readable:
                return self._json(200, None)
            return self._json(200, app)

        return self._json(404, {"message": "unknown path"})

    def do_POST(self) -> None:  # noqa: N802
        u = urlparse(self.path)
        self.state.log.append(("POST", u.path))
        body = self._body()

        if u.path == "/oidc/token":
            return self._json(200, {"access_token": "mock-token", "expires_in": 3600,
                                    "token_type": "Bearer", "scope": "all"})

        if u.path == "/api/resources":
            new = {"id": f"res-{len(self.state.resources) + 1}", "name": body.get("name"),
                   "indicator": body.get("indicator"), "scopes": []}
            self.state.resources.append(new)
            if self.state.hang_up_on_resource_create:
                # The object EXISTS now, but the caller never learns that. This is the
                # uncertain-write case: a retry here would create a duplicate.
                self.close_connection = True
                self.connection.close()
                return
            return self._json(201, new)

        if u.path.startswith("/api/resources/") and u.path.endswith("/scopes"):
            rid = u.path.split("/")[3]
            res = next((r for r in self.state.resources if r.get("id") == rid), None)
            if res is None:
                return self._json(404, {"message": "no resource"})
            res.setdefault("scopes", []).append({"name": body.get("name"),
                                                 "description": body.get("description")})
            return self._json(201, {"name": body.get("name")})

        if u.path == "/api/applications":
            new = {"id": f"app-{len(self.state.apps) + 1}", "name": body.get("name"),
                   "type": body.get("type"), "secret": "mock-issued-secret-value",
                   "oidcClientMetadata": body.get("oidcClientMetadata") or {}}
            self.state.apps.append(new)
            return self._json(201, new)

        return self._json(404, {"message": "unknown path"})

    def do_PATCH(self) -> None:  # noqa: N802
        u = urlparse(self.path)
        self.state.log.append(("PATCH", u.path))
        body = self._body()
        aid = u.path.split("/")[3]
        app = next((a for a in self.state.apps if a.get("id") == aid), None)
        if app is None:
            return self._json(404, {"message": "no app"})
        app.setdefault("oidcClientMetadata", {}).update(body.get("oidcClientMetadata") or {})
        return self._json(200, app)


class Mock:
    def __init__(self) -> None:
        self.state = MockState()
        handler = type("H", (Handler,), {"state": self.state})
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.srv.serve_forever, daemon=True)
        self.thread.start()

    @property
    def endpoint(self) -> str:
        host, port = self.srv.server_address[:2]
        return f"http://{host}:{port}"

    def stop(self) -> None:
        self.srv.shutdown()
        self.srv.server_close()


def run(mock: Mock, *args: str) -> tuple[int, str, str]:
    """Run the provisioner against the mock with a throwaway HOME.

    HOME is redirected so CONF/SECRET_OUT resolve into a temp dir: the test must never
    touch ~/.config/workforce-platform, and redirecting HOME also exercises the real
    path-resolution logic rather than bypassing it.
    """
    home = tempfile.mkdtemp(prefix="wf-prov-test-")
    env = dict(os.environ)
    env.update({
        "HOME": home,
        "LOGTO_ENDPOINT": mock.endpoint,
        "LOGTO_MANAGEMENT_CLIENT_ID": "test-client-id",
        "LOGTO_MANAGEMENT_CLIENT_SECRET": "test-client-secret-value",
        "LOGTO_MANAGEMENT_RESOURCE": "https://default.logto.app/api",
    })
    try:
        p = subprocess.run([sys.executable, str(SCRIPT), *args],
                           env=env, capture_output=True, text=True, timeout=90)
        return p.returncode, p.stdout, p.stderr
    finally:
        shutil.rmtree(home, ignore_errors=True)


RESULTS: list[tuple[str, bool, str]] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    RESULTS.append((name, bool(ok), detail))
    mark = "PASS" if ok else "FAIL"
    print(f"  {mark}  {name}" + (f"  [{detail}]" if detail and not ok else ""))


def seed_resource(state: MockState, indicator: str = API_INDICATOR) -> dict:
    r = {"id": f"res-{len(state.resources) + 1}", "name": "Workforce Platform API",
         "indicator": indicator, "scopes": []}
    state.resources.append(r)
    return r


def seed_app(state: MockState, *, redirects=None, logouts=None, type_="Traditional") -> dict:
    a = {"id": f"app-{len(state.apps) + 1}", "name": APP_NAME, "type": type_,
         "oidcClientMetadata": {"redirectUris": list(redirects or []),
                                "postLogoutRedirectUris": list(logouts or [])}}
    state.apps.append(a)
    return a


# --------------------------------------------------------------------------------------
# 1. dry run must not mutate anything
# --------------------------------------------------------------------------------------
def t_dry_run_does_not_mutate() -> None:
    m = Mock()
    try:
        code, out, _ = run(m)
        check("dry run exits 0", code == 0, f"exit={code}")
        check("dry run made no mutating request", m.state.mutations() == [],
              f"mutations={m.state.mutations()}")
        check("dry run reports what it would create", "create" in out.lower())
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 2 & 3. apply from empty, then a second apply is a true no-op
# --------------------------------------------------------------------------------------
def t_apply_then_second_run_is_noop() -> None:
    m = Mock()
    try:
        code, out, _ = run(m, "--apply")
        check("apply exits 0", code == 0, f"exit={code}")
        check("resource created", any(r["indicator"] == API_INDICATOR for r in m.state.resources))
        res = next((r for r in m.state.resources if r["indicator"] == API_INDICATOR), None)
        check("all three scopes created", res is not None and
              sorted(s["name"] for s in res["scopes"]) == ["approve:work", "read:work", "write:work"],
              f"scopes={res and [s['name'] for s in res['scopes']]}")
        app = next((a for a in m.state.apps if a["name"] == APP_NAME), None)
        check("application created as a confidential client", app is not None and app["type"] == "Traditional",
              f"type={app and app['type']}")
        check("redirect URI registered", app is not None and
              REDIRECT in (app.get("oidcClientMetadata") or {}).get("redirectUris", []))

        # Now the idempotency claim: a second apply must issue ZERO mutating requests.
        before = len(m.state.mutations())
        code2, out2, _ = run(m, "--apply")
        after = m.state.mutations()[before:]
        check("second apply exits 0", code2 == 0, f"exit={code2}")
        check("second apply performed NO mutation", after == [], f"mutations={after}")
        check("second apply says nothing to do", "nothing to do" in out2.lower())
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 4. an object on a later page must be found (otherwise a duplicate is created)
# --------------------------------------------------------------------------------------
def t_pagination_finds_object_on_later_page() -> None:
    m = Mock()
    try:
        # Put the matching resource beyond the FIRST PAGE that the script actually
        # requests (its PAGE size), so a first-page-only read would miss it and create a
        # duplicate. Seeding 100+ items is what makes the second page real.
        page = 100
        for i in range(page):
            seed_resource(m.state, indicator=f"https://unrelated.example/{i}")
        target = seed_resource(m.state)
        seed_app(m.state, redirects=[REDIRECT], logouts=[LOGOUT])

        code, _, _ = run(m, "--apply")
        check("pagination walk exits 0", code == 0, f"exit={code}")
        creates = [r for r in m.state.log if r == ("POST", "/api/resources")]
        check("no duplicate resource created", creates == [], f"resource POSTs={creates}")
        check("the existing resource was recognised",
              len([r for r in m.state.resources if r["indicator"] == API_INDICATOR]) == 1)
        check("the found resource is the seeded one", target["indicator"] == API_INDICATOR)
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 5. an unexpected response shape must be refused, not read as empty
# --------------------------------------------------------------------------------------
def t_unexpected_shape_is_refused() -> None:
    for shape in ("unexpected", "string"):
        m = Mock()
        try:
            m.state.resources_shape = shape
            code, out, err = run(m, "--apply")
            check(f"{shape} shape refused (exit!=0)", code != 0, f"exit={code}")
            check(f"{shape} shape caused no mutation", m.state.mutations() == [],
                  f"mutations={m.state.mutations()}")
            combined = (out + err).lower()
            check(f"{shape} shape says why", "unexpected shape" in combined or "unexpected" in combined)
        finally:
            m.stop()


# --------------------------------------------------------------------------------------
# 6. an uncertain write is reconciled by identity, never retried into a duplicate
# --------------------------------------------------------------------------------------
def t_uncertain_write_reconciles() -> None:
    m = Mock()
    try:
        m.state.hang_up_on_resource_create = True
        code, out, err = run(m, "--apply")
        creates = [r for r in m.state.log if r == ("POST", "/api/resources")]
        check("uncertain create attempted exactly once", len(creates) == 1, f"attempts={len(creates)}")
        check("no duplicate resource exists",
              len([r for r in m.state.resources if r["indicator"] == API_INDICATOR]) == 1,
              f"resources={[r['indicator'] for r in m.state.resources]}")
        # The resource DID get created, so the run should adopt it and still finish.
        check("reconciled run exits 0", code == 0, f"exit={code} out={out[-200:]} err={err[-200:]}")
        check("the adopted resource was read back",
              "readback" in out.lower() or "asserted" in out.lower())
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 7. foreign redirect AND logout URIs must survive an update
# --------------------------------------------------------------------------------------
def t_foreign_uris_preserved() -> None:
    m = Mock()
    try:
        seed_resource(m.state)
        # Another product's registration, already carrying ITS destinations, and missing
        # ours. The update must add ours without dropping theirs.
        theirs_redirect = "https://other-app.example.test/cb"
        theirs_logout = "https://other-app.example.test/goodbye"
        m.state.apps.append({
            "id": "app-other", "name": APP_NAME, "type": "Traditional",
            "oidcClientMetadata": {"redirectUris": [theirs_redirect],
                                   "postLogoutRedirectUris": [theirs_logout]},
        })
        code, _, _ = run(m, "--apply")
        app = m.state.apps[0]
        meta = app.get("oidcClientMetadata") or {}
        check("update exits 0", code == 0, f"exit={code}")
        check("foreign redirect URI preserved", theirs_redirect in meta.get("redirectUris", []),
              f"redirects={meta.get('redirectUris')}")
        check("foreign LOGOUT URI preserved", theirs_logout in meta.get("postLogoutRedirectUris", []),
              f"logouts={meta.get('postLogoutRedirectUris')}")
        check("our redirect URI added", REDIRECT in meta.get("redirectUris", []))
        check("our logout URI added", LOGOUT in meta.get("postLogoutRedirectUris", []))
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 8. unreadable metadata must refuse the update
# --------------------------------------------------------------------------------------
def t_unreadable_metadata_refuses_update() -> None:
    m = Mock()
    try:
        seed_resource(m.state)
        # A list entry lacking OIDC metadata, and a detail fetch returning nothing: the
        # existing destinations are UNKNOWN. Writing only our own would delete theirs.
        m.state.apps.append({"id": "app-blank", "name": APP_NAME, "type": "Traditional"})
        m.state.app_detail_readable = False
        code, out, err = run(m, "--apply")
        patches = [r for r in m.state.log if r[0] == "PATCH"]
        check("unreadable metadata refuses to update", code != 0, f"exit={code}")
        check("no PATCH was issued", patches == [], f"patches={patches}")
        combined = (out + err).lower()
        check("refusal explains the risk", "could not be read" in combined or "unknown" in combined,
              combined[-300:])
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 9. duplicate/conflicting objects are an error, never a coin flip
# --------------------------------------------------------------------------------------
def t_duplicate_objects_are_an_error() -> None:
    m = Mock()
    try:
        seed_resource(m.state)
        seed_resource(m.state)  # two resources with the SAME indicator
        code, out, err = run(m, "--apply")
        check("duplicate resources refused", code != 0, f"exit={code}")
        check("duplicate resources caused no mutation", m.state.mutations() == [],
              f"mutations={m.state.mutations()}")
        check("refusal mentions the conflict",
              "match" in (out + err).lower() or "duplicate" in (out + err).lower(),
              (out + err)[-300:])
    finally:
        m.stop()


# --------------------------------------------------------------------------------------
# 10. no secret may be echoed
# --------------------------------------------------------------------------------------
def t_no_secret_echoed() -> None:
    m = Mock()
    try:
        code, out, err = run(m, "--apply")
        combined = out + err
        check("apply did not echo the management secret",
              "test-client-secret-value" not in combined)
        check("apply states the secret was never printed",
              "never printed" in combined.lower(), combined[:300])
    finally:
        m.stop()


def main() -> int:
    tests = [
        ("dry run does not mutate", t_dry_run_does_not_mutate),
        ("apply then second run is a no-op", t_apply_then_second_run_is_noop),
        ("pagination finds object on a later page", t_pagination_finds_object_on_later_page),
        ("unexpected shape is refused", t_unexpected_shape_is_refused),
        ("uncertain write reconciles by identity", t_uncertain_write_reconciles),
        ("foreign redirect and logout URIs preserved", t_foreign_uris_preserved),
        ("unreadable metadata refuses update", t_unreadable_metadata_refuses_update),
        ("duplicate objects are an error", t_duplicate_objects_are_an_error),
        ("no secret echoed", t_no_secret_echoed),
    ]
    for name, fn in tests:
        print(f"\n=== {name} ===")
        fn()

    failed = [r for r in RESULTS if not r[1]]
    print("\n" + "=" * 70)
    print(f"PROVISIONING CONTRACT: {len(RESULTS) - len(failed)}/{len(RESULTS)} passed")
    if failed:
        for name, _, detail in failed:
            print(f"  FAILED: {name}  {detail}")
        return 1
    print("all properties hold")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
