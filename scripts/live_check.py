#!/usr/bin/env python3
"""Live end-to-end check against the running local API. No secrets are printed.

Usage: python3 scripts/live_check.py            # after scripts/dev.py api + worker are running
"""
import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

ENV = pathlib.Path("/home/prod/.config/workforce-platform/local-auth.env")
if not ENV.exists():
    sys.exit("local-auth.env not found; run the development setup first")
if ENV.stat().st_mode & 0o077:
    sys.exit("refusing to read a secret file that is readable by other users")

entries = dict(l.split("=", 1) for l in ENV.read_text().splitlines() if "=" in l)
# Each entry is token=org:principal:role. Index by (org, role) so the check
# always authenticates as the intended principal in the intended organization.
BY_PRINCIPAL = {}
for token, identity in (e.split("=", 1) for e in entries["LOCAL_AUTH_TOKENS"].split(",")):
    org, principal, role = identity.split(":")
    BY_PRINCIPAL[(org, role)] = token
    BY_PRINCIPAL[(org, principal)] = token

BASE = "http://127.0.0.1:8095/api/v1"
A = ("org-fixture-a", "requester")


def call(actor, method, path, body=None, expect=None):
    org, who = actor
    token = BY_PRINCIPAL[(org, who)]
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        BASE + path, data=data, method=method,
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req) as resp:
            code, payload = resp.status, json.loads(resp.read() or b"null")
    except urllib.error.HTTPError as e:
        code, payload = e.code, json.loads(e.read() or b"null")
    if expect is not None and code != expect:
        raise SystemExit(f"FAIL {method} {path} as {actor}: got {code} want {expect} :: {payload}")
    return code, payload


checks = []
def expect(name, got, want):
    checks.append((name, got, want))


admin = ("org-fixture-a", "admin")
requester = ("org-fixture-a", "requester")
approver = ("org-fixture-a", "approver")
other = ("org-fixture-b", "requester")

# 1. Operator-only plugin release lifecycle.
stamp = str(int(time.time()))
code, rel = call(admin, "POST", "/registry/releases", {
    "family": f"supplier-inventory-{stamp}", "kind": "connector", "version": f"0.4.{stamp[-4:]}",
    "digest": f"sha256:live-{stamp}", "requested_capabilities": ["read:inventory"],
    "granted_capabilities": ["write:inventory"], "simulation": True,
    "provenance": {"source": "live_check"}, "license_info": {"spdx": "MIT"},
}, expect=201)
expect("registry starts quarantined", rel["state"], "quarantined")
expect("submitter grants ignored", len(rel.get("granted_capabilities") or []), 0)
for reject_actor in (requester, other):
    code, _ = call(reject_actor, "POST", f"/registry/releases/{rel['id']}/transition",
                   {"expected_version": rel["revision"], "target_state": "verified", "reason": "self"})
    expect(f"non-operator cannot verify ({reject_actor[1]}@{reject_actor[0]})", code in (403, 404, 409), True)
for step in ("verified", "approved", "installed", "active"):
    code, rel = call(admin, "POST", f"/registry/releases/{rel['id']}/transition",
                     {"expected_version": rel["revision"], "target_state": step, "reason": "live check"}, expect=200)
expect("registry reaches active", rel["state"], "active")
code, _ = call(other, "GET", f"/registry/releases/{rel['id']}")
expect("cross-org registry read denied", code, 404)
code, rel = call(admin, "POST", f"/registry/releases/{rel['id']}/transition",
                 {"expected_version": rel["revision"], "target_state": "revoked", "reason": "live check"}, expect=200)
expect("registry revocable", rel["state"], "revoked")

# 2. Stable business key prevents a duplicate official effect.
code, task = call(requester, "POST", "/tasks", {"title": "live business key check"}, expect=201)
code, records = call(requester, "GET", "/integrations/inventory/records", expect=200)
target = records["items"][0]
key = f"live-check-{stamp}:invoice-1:settle"
operation = {"integration": "inventory", "action": "adjust", "business_key": key,
             "target_id": target["id"], "expected_version": target["version"], "payload": {"delta": 1}}
code, prop = call(requester, "POST", f"/tasks/{task['id']}/proposals", {"summary": "live check", "operations": [operation]}, expect=201)
expect("proposal created", code, 201)

# A different task claiming the same official operation must be refused.
code, other_task = call(requester, "POST", "/tasks", {"title": "live cross-task claim"}, expect=201)
code, _ = call(requester, "POST", f"/tasks/{other_task['id']}/proposals", {"summary": "cross-task claim", "operations": [operation]})
expect("cross-task business key refused", code, 409)

# The original proposal is still the live revision and remains actionable.
_, current = call(requester, "GET", f"/proposals/{prop['id']}", expect=200)
expect("original proposal still pending", current["status"], "pending_endorsement")

# 3. Endorsement, separate approval, real River execution, independent readback.
call(requester, "POST", f"/proposals/{prop['id']}/endorse", {"revision": prop["revision"], "digest": prop["digest"]}, expect=200)
code, _ = call(requester, "POST", f"/proposals/{prop['id']}/approve", {"revision": prop["revision"], "digest": prop["digest"]})
expect("requester cannot approve own proposal", code, 409)
call(approver, "POST", f"/proposals/{prop['id']}/approve", {"revision": prop["revision"], "digest": prop["digest"]}, expect=200)
final = {}
for _ in range(80):
    _, final = call(requester, "GET", f"/proposals/{prop['id']}", expect=200)
    if final["status"] in ("done", "needs_attention"):
        break
    time.sleep(0.5)
expect("proposal executed by River", final["status"], "done")
_, after = call(requester, "GET", "/integrations/inventory/records", expect=200)
row = [r for r in after["items"] if r["id"] == target["id"]][0]
expect("effect applied once", row["data"]["quantity"], target["data"]["quantity"] + 1)
_, receipts = call(requester, "GET", "/receipts", expect=200)
expect("readback receipt recorded", any(r.get("effect_id") for r in receipts["items"]), True)
_, events = call(requester, "GET", "/events", expect=200)
expect("audit trail present", len(events["items"]) > 0, True)

# 4. Cross-organization isolation on work items.
code, _ = call(other, "GET", f"/tasks/{task['id']}")
expect("cross-org task read denied", code, 404)
code, _ = call(other, "GET", "/registry/releases", expect=200)
expect("other org sees only its own releases", any(r["id"] == rel["id"] for r in _["items"]), False)

width = max(len(n) for n, _, _ in checks)
for name, got, want in checks:
    print(f"  {name:<{width}}  got={got!r:<12} want={want!r:<12} {'OK' if got == want else 'MISMATCH'}")
failed = [c for c in checks if c[1] != c[2]]
print(f"\nLIVE CHECK: {len(checks) - len(failed)}/{len(checks)} passed — {'PASS' if not failed else 'FAIL'}")
sys.exit(1 if failed else 0)
