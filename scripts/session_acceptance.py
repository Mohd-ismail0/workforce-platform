#!/usr/bin/env python3
"""Browser acceptance for the remaining session behaviours.

Closes the two items that were previously claimed as covered by unit tests only:
  1. CSRF is actually enforced for a cookie-authenticated state-changing request.
  2. Logout actually invalidates access server-side.

Deliberately NON-DESTRUCTIVE for the CSRF probe: the API checks CSRF BEFORE routing, so a POST
to a path that does not exist distinguishes the two outcomes without writing any data.
  - without the CSRF header  -> 403 csrf_failed   (rejected before routing)
  - with the CSRF header     -> 404 not_found     (routed, so CSRF passed)

Prints only status codes and booleans. Never prints the CSRF token, cookies, or bodies beyond a
short error code.

Usage: python3 session_acceptance.py
"""
import json
import sys
import time
import urllib.request

import websocket

PORT = 9333
ORIGIN = f"http://127.0.0.1:{PORT}"
APP = "http://127.0.0.1:5175/"


def pick_page():
    tabs = json.load(urllib.request.urlopen(f"http://127.0.0.1:{PORT}/json/list", timeout=8))
    pages = [t for t in tabs if t.get("type") == "page" and t.get("url", "").startswith("http")]
    return (pages or [t for t in tabs if t.get("type") == "page"])[0]


class C:
    def __init__(self, ws_url):
        self.ws = websocket.create_connection(ws_url, timeout=30, origin=ORIGIN)
        self.n = 0

    def call(self, method, **params):
        self.n += 1
        mid = self.n
        self.ws.send(json.dumps({"id": mid, "method": method, "params": params}))
        deadline = time.time() + 30
        while time.time() < deadline:
            msg = json.loads(self.ws.recv())
            if msg.get("id") == mid:
                return msg
        raise TimeoutError(method)

    def ev(self, expr, await_promise=False):
        r = self.call("Runtime.evaluate", expression=expr, returnByValue=True,
                      awaitPromise=await_promise)
        res = r.get("result", {})
        if "exceptionDetails" in res:
            return f"<exception> {res['exceptionDetails'].get('text')}"
        return res.get("result", {}).get("value")

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


# Runs entirely in the page so the CSRF token is read and used without ever leaving it.
JS = r"""
(async () => {
  const j = async (r) => {
    let code = '';
    try { const b = await r.json(); code = (b && b.error && b.error.code) || ''; } catch (e) {}
    return { status: r.status, code };
  };

  // 1. who are we, and what CSRF token must be echoed back?
  const sess = await fetch('/auth/session',
      {credentials:'same-origin', headers:{Accept:'application/json'}});
  const sbody = await sess.json().catch(() => ({}));
  const csrf = sbody.csrf_token || '';
  const out = {
    session_status: sess.status,
    authenticated: !!sbody.authenticated,
    identity_id: (sbody.identity && sbody.identity.id) || null,
    csrf_present: !!csrf,
  };

  // 2. CSRF enforcement, proved without writing anything: the check happens before routing.
  const probe = '/api/v1/__csrf_probe_no_such_route__';
  out.no_csrf = await j(await fetch(probe, {
    method:'POST', credentials:'same-origin',
    headers:{'Content-Type':'application/json'}, body:'{}'
  }));
  out.with_csrf = await j(await fetch(probe, {
    method:'POST', credentials:'same-origin',
    headers:{'Content-Type':'application/json', 'X-CSRF-Token': csrf}, body:'{}'
  }));

  // 3. an authorized read still works with the cookie alone.
  out.me = await j(await fetch('/api/v1/me', {credentials:'same-origin'}));

  // 4. logout, then prove access is actually gone. Gated because it ends a real session;
  //    running it unconditionally signs the operator out every time this script is used.
  if (!__WITH_LOGOUT__) {
    out.logout_skipped = true;
    return JSON.stringify(out);
  }
  const lo = await fetch('/auth/logout', {
    method:'POST', credentials:'same-origin',
    headers:{'Content-Type':'application/json', 'X-CSRF-Token': csrf}, body:'{}'
  });
  out.logout_status = lo.status;

  const after = await fetch('/auth/session',
      {credentials:'same-origin', headers:{Accept:'application/json'}});
  const abody = await after.json().catch(() => ({}));
  out.after_session_status = after.status;
  out.after_authenticated = !!abody.authenticated;
  out.after_me = await j(await fetch('/api/v1/me', {credentials:'same-origin'}));
  return JSON.stringify(out);
})()
"""


def main():
    """Run the acceptance checks.

    The logout leg is OPT-IN (`--with-logout`). It ends a real session, so running it by
    default would sign the operator out of their own browser every time someone re-ran this
    script — which is exactly what happened the first time it was used.
    """
    with_logout = "--with-logout" in sys.argv
    c = C(pick_page()["webSocketDebuggerUrl"])
    try:
        c.call("Network.enable")
        c.call("Page.enable")

        # Start from a clean page load so the assertions describe a fresh visit, which also
        # checks that a reload keeps the login.
        c.call("Page.navigate", url=APP)
        time.sleep(4)
        print("=== reload preserved the login? ===")
        res = json.loads(c.ev(JS.replace("__WITH_LOGOUT__", "true" if with_logout else "false"),
                              await_promise=True))
        print("  session status      :", res.get("session_status"),
              " authenticated:", res.get("authenticated"),
              " identity:", res.get("identity_id"))
        print("  csrf token present  :", res.get("csrf_present"))
        print()
        print("=== CSRF enforcement (POST to a non-existent route) ===")
        print("  without X-CSRF-Token:", res.get("no_csrf"))
        print("  with    X-CSRF-Token:", res.get("with_csrf"))
        print()
        print("=== authorized read ===")
        print("  GET /api/v1/me      :", res.get("me"))
        print()
        print("=== logout invalidates access ===")
        if res.get("logout_skipped"):
            print("  SKIPPED (pass --with-logout to run it; it ends a real session)")
        else:
            print("  POST /auth/logout   :", res.get("logout_status"))
            print("  /auth/session after :", res.get("after_session_status"),
                  " authenticated:", res.get("after_authenticated"))
            print("  GET /api/v1/me after:", res.get("after_me"))
        print()

        ok = True
        checks = [
            ("reload kept the session",
             res.get("session_status") == 200 and res.get("authenticated") is True),
            ("identity resolved", res.get("identity_id") == "mdil"),
            ("CSRF token issued", res.get("csrf_present") is True),
            ("mutation without CSRF refused",
             res.get("no_csrf", {}).get("status") == 403
             and res.get("no_csrf", {}).get("code") == "csrf_failed"),
            ("mutation with CSRF routed (not refused)",
             res.get("with_csrf", {}).get("status") == 404),
            ("authorized read succeeded", res.get("me", {}).get("status") == 200),
        ]
        if not res.get("logout_skipped"):
            checks += [
                ("logout returned 200", res.get("logout_status") == 200),
                ("session reports signed out", res.get("after_authenticated") is False),
                ("API refuses after logout", res.get("after_me", {}).get("status") == 401),
            ]
        print("=== CHECKS ===")
        for name, passed in checks:
            print(f"  [{'PASS' if passed else 'FAIL'}] {name}")
            ok = ok and passed
        print()
        print("RESULT:", "PASS" if ok else "FAIL")
        return 0 if ok else 1
    finally:
        c.close()


if __name__ == "__main__":
    raise SystemExit(main())
