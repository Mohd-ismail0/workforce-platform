#!/usr/bin/env python3
"""Drive one full browser sign-in and report the exact failing boundary.

Written because the earlier failure was ONLY visible in the browser: the callback succeeded,
a session row was created, and the page still showed the sign-in screen because the browser
had discarded the session cookie. Testing the HTTP layer alone missed it entirely.

Never prints cookie values, authorization codes, tokens, or field contents — only names,
attributes, status codes and the page text a human is already reading.

Usage: python3 signin_acceptance.py [--start]
"""
import json
import sys
import time
import urllib.request

import websocket

PORT = 9333
ORIGIN = f"http://127.0.0.1:{PORT}"
APP = "http://127.0.0.1:5175/"
SESSION_COOKIE = "__Host-workforce_session"


def pick_page():
    tabs = json.load(urllib.request.urlopen(f"http://127.0.0.1:{PORT}/json/list", timeout=8))
    pages = [t for t in tabs if t.get("type") == "page" and t.get("url", "").startswith("http")]
    if not pages:
        pages = [t for t in tabs if t.get("type") == "page"]
    return pages[0]


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
        raise TimeoutError(f"{method} did not answer")

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


def app_cookies(c):
    return c.call("Network.getCookies", urls=[APP]).get("result", {}).get("cookies", [])


def show(label, c):
    print(f"--- {label} ---")
    print("  url   :", c.ev("location.href"))
    print("  title :", c.ev("document.title"))
    txt = c.ev("document.body ? document.body.innerText.replace(/\\s+/g,' ').slice(0,220) : 'NO BODY'")
    print("  text  :", repr((txt or "").strip()[:210]))
    ck = app_cookies(c)
    names = [x.get("name") for x in ck]
    print("  cookies on the app origin:", names or "(none)")
    for x in ck:
        print(f"     {x.get('name'):<30} httpOnly={x.get('httpOnly')} secure={x.get('secure')} "
              f"sameSite={x.get('sameSite')} path={x.get('path')}")
    has = SESSION_COOKIE in names
    print(f"  session cookie present: {has}")
    return has


def main():
    start = "--start" in sys.argv
    c = C(pick_page()["webSocketDebuggerUrl"])
    try:
        c.call("Network.enable")
        c.call("Page.enable")

        print("=== BEFORE ===")
        show("current page", c)

        if start:
            print()
            print("=== LOGIN ROUND TRIP ===")
            # Remove only THIS app's cookies. Clearing the whole browser jar is what logged the
            # user out of the provider last time; that must not happen again.
            for stale in (SESSION_COOKIE, "workforce_csrf"):
                c.call("Network.deleteCookies", name=stale, url=APP)
            print("  cleared app cookies only:", [x.get("name") for x in app_cookies(c)] or "(none)")

            c.call("Page.navigate", url=f"{APP}auth/login?return_to=%2F")
            deadline = time.time() + 60
            trail = []
            returned = False
            while time.time() < deadline:
                time.sleep(1.5)
                href = c.ev("location.href") or ""
                if not isinstance(href, str):
                    continue
                host = href.split("?")[0]
                if not trail or trail[-1] != host:
                    trail.append(host)
                if host.startswith(APP) and "/auth/" not in host:
                    returned = True
                    break
            print("  navigation trail:")
            for h in trail:
                print("     ", h)
            print("  returned to the app:", returned)
            if not returned:
                print("  NOTE: still away from the app -> the provider has not completed the flow,")
                print("        so nothing about our cookie handling is proven yet.")
                c.call("Page.bringToFront")
                show("where it stopped", c)
                return 2

        print()
        print("=== AFTER ===")
        has_cookie = show("landing page", c)

        print()
        print("=== SERVER-SIDE, FROM THIS BROWSER (cookies attach automatically) ===")
        sess = c.ev(
            "(async () => { const r = await fetch('/auth/session',"
            "{credentials:'same-origin',headers:{Accept:'application/json'}});"
            "return r.status + ' ' + (await r.text()); })()", await_promise=True)
        print("  GET /auth/session ->", str(sess)[:260])

        me = c.ev(
            "(async () => { const r = await fetch('/api/v1/me',{credentials:'same-origin'});"
            "return r.status + ' ' + (await r.text()); })()", await_promise=True)
        print("  GET /api/v1/me    ->", str(me)[:300])

        print()
        print("=== VERDICT ===")
        authd = '"authenticated":true' in str(sess)
        if authd and has_cookie:
            print("  PASS: the browser holds the session cookie and the server authenticates it.")
            print("        The sign-in loop is fixed.")
            return 0
        if authd and not has_cookie:
            print("  PARTIAL: server authenticates, but no session cookie is visible here.")
            print("           The cookie name/attributes or transport still need attention.")
            return 1
        if has_cookie and not authd:
            print("  FAILURE at the SERVER session/link validation step:")
            print("          the cookie exists but the server refuses it.")
            return 1
        print("  FAILURE at the COOKIE step: the callback ran, no usable session cookie was kept.")
        return 1
    finally:
        c.close()


if __name__ == "__main__":
    raise SystemExit(main())
