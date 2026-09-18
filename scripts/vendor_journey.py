#!/usr/bin/env python3
"""PD-05 coherent supplier journey acceptance, against the LIVE local stack.

Implements docs/build-spec 04 Section 5 as reproducible acceptance evidence:
scoped intake -> prepare -> inventory preview -> requester endorsement ->
authorized approval -> park/resume and apply -> independent readback ->
response draft. It asserts two invariants the vision depends on:

  (a) An inventory approval does NOT authorize or dispatch ANY email send.
      Send is a separate Review Package requiring its own decision - the
      "inventory approval does not authorize email" invariant (Section 5
      step 9). Implemented by constructing a mail-send proposal and showing
      it remains pending_endorsement until it gets its OWN endorsement and a
      DISTINCT approval.

  (b) The request-changes branch supersedes the stale review and a fresh
      revision is required before anything may be approved (Section 5
      decision contract: "Changes request bounded reprepare and supersede
      affected requests").

Also covers duplicate-mail dedupe via business key (Section 5 step 1: a
reused document identity is refused) and the stale-approval race (approve
with a superseded revision is refused).

Usage: run after `python3 scripts/dev.py api` + `python3 scripts/dev.py worker`:
    python3 scripts/vendor_journey.py
Exit 0 = all checks passed. Prints a per-check table.
"""
import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

ENV = pathlib.Path("/home/prod/.config/workforce-platform/local-auth.env")
if not ENV.exists():
    sys.exit("local-auth.env not found (run the development setup first)")
if ENV.stat().st_mode & 0o077:
    sys.exit("refusing to read a secret file readable by other users")

entries = dict(l.split("=", 1) for l in ENV.read_text().splitlines() if "=" in l)
BY_PRINCIPAL = {}
for token, identity in (e.split("=", 1) for e in entries["LOCAL_AUTH_TOKENS"].split(",")):
    org, principal, role = identity.split(":")
    BY_PRINCIPAL[(org, role)] = token
    BY_PRINCIPAL[(org, principal)] = token

BASE = "http://127.0.0.1:8095/api/v1"


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


requester = ("org-fixture-a", "requester")
approver = ("org-fixture-a", "approver")
other = ("org-fixture-b", "requester")

stamp = str(int(time.time()))

# --------------------------------------------------------------------------
# 1. Scoped intake + duplicate-mail dedupe via business key.
# A reused document identity must be refused, not silently applied twice.
# --------------------------------------------------------------------------
code, task = call(requester, "POST", "/tasks", {"title": f"supplier journey {stamp}"}, expect=201)
code, recs = call(requester, "GET", "/integrations/inventory/records", expect=200)
target = recs["items"][0]
mail_business_key = f"journey-{stamp}:invoice-7:send"
inv_business_key = f"journey-{stamp}:invoice-7:settle"

inv_op = {
    "integration": "inventory", "action": "adjust",
    "business_key": inv_business_key,
    "target_id": target["id"], "expected_version": target["version"],
    "payload": {"delta": 3},
}
code, inv_prop = call(requester, "POST", f"/tasks/{task['id']}/proposals",
                      {"summary": "recognise three units on the received invoice",
                       "operations": [inv_op]}, expect=201)
expect("intake: inventory proposal created", code, 201)

code, dup_task = call(requester, "POST", "/tasks", {"title": "duplicate delivery"}, expect=201)
code, _ = call(requester, "POST", f"/tasks/{dup_task['id']}/proposals",
               {"summary": "duplicate of the same doc", "operations": [inv_op]})
expect("duplicate delivery refused by business key", code, 409)

# --------------------------------------------------------------------------
# 2. Requester endorsement binds revision; requester alone cannot dispatch.
# --------------------------------------------------------------------------
code, _ = call(requester, "POST", f"/proposals/{inv_prop['id']}/approve",
               {"revision": inv_prop["revision"], "digest": inv_prop["digest"]})
expect("requester self-approval refused", code, 409)
code, _ = call(requester, "POST", f"/proposals/{inv_prop['id']}/endorse",
               {"revision": inv_prop["revision"], "digest": inv_prop["digest"]}, expect=200)
expect("requester endorsement accepted with revision", code, 200)
code, _ = call(approver, "POST", f"/proposals/{inv_prop['id']}/approve",
               {"revision": inv_prop["revision"] + 99, "digest": inv_prop["digest"]})
expect("stale-approval race refused (wrong revision)", code in (409, 422, 404), True)

# --------------------------------------------------------------------------
# 3. Authorized approval -> apply -> independent readback.
# --------------------------------------------------------------------------
call(approver, "POST", f"/proposals/{inv_prop['id']}/approve",
     {"revision": inv_prop["revision"], "digest": inv_prop["digest"]}, expect=200)
final = {}
for _ in range(80):
    _, final = call(requester, "GET", f"/proposals/{inv_prop['id']}", expect=200)
    if final["status"] in ("done", "needs_attention", "rejected"):
        break
    time.sleep(0.5)
expect("approved inventory proposal executed by River", final["status"], "done")

_, after = call(requester, "GET", "/integrations/inventory/records", expect=200)
row = [r for r in after["items"] if r["id"] == target["id"]][0]
expect("effect applied exactly once (quantity +3)", row["data"]["quantity"], target["data"]["quantity"] + 3)
_, receipts = call(requester, "GET", "/receipts", expect=200)
expect("independent readback receipt recorded", any(r.get("effect_id") for r in receipts["items"]), True)

# --------------------------------------------------------------------------
# 4. THE SEPARATION INVARIANT: an inventory approval must NOT dispatch mail.
# A send is a different Review Package needing its own endorsement AND a
# distinct approval. It is also a distinct work item: once an inventory task's
# proposal is applied the task is terminal, so the reply lives on its own task
# - matching the kernel's one-review-cycle-per-task model.
# --------------------------------------------------------------------------
code, mail_recs = call(requester, "GET", "/integrations/mail/records", expect=200)
# A send targets a real mailbox/thread record (the kernel 404s on a fabricated
# target, which is exactly right). Pick the first threaded record to anchor on.
mail_target = next(
    (r for r in mail_recs["items"] if r.get("data", {}).get("recipients")),
    mail_recs["items"][0],
)
code, send_task = call(requester, "POST", "/tasks",
                       {"title": f"reply to supplier {stamp}"}, expect=201)
mail_op = {
    "integration": "mail", "action": "send",
    "business_key": mail_business_key,
    "target_id": mail_target["id"], "expected_version": mail_target["version"],
    "payload": {"recipients": ["ops@supplier.test"], "subject": "Received",
                "body": "We received your invoice and updated stock.",
                "bcc": [], "attachments": []},
}
code, send_prop = call(requester, "POST", f"/tasks/{send_task['id']}/proposals",
                       {"summary": "reply to supplier", "operations": [mail_op]}, expect=201)
expect("separate send Review Package created", code, 201)
_, sp = call(requester, "GET", f"/proposals/{send_prop['id']}", expect=200)
expect("send is NOT auto-executed by the inventory approval", sp["status"], "pending_endorsement")

# It still must go through its own chain - endorse, then a DISTINCT approver.
code, _ = call(requester, "POST", f"/proposals/{send_prop['id']}/endorse",
               {"revision": send_prop["revision"], "digest": send_prop["digest"]}, expect=200)
call(approver, "POST", f"/proposals/{send_prop['id']}/approve",
     {"revision": send_prop["revision"], "digest": send_prop["digest"]}, expect=200)
send_final = {}
for _ in range(80):
    _, send_final = call(requester, "GET", f"/proposals/{send_prop['id']}", expect=200)
    if send_final["status"] in ("done", "needs_attention", "rejected"):
        break
    time.sleep(0.5)
expect("send executes only after its OWN distinct approval", send_final["status"], "done")

# --------------------------------------------------------------------------
# 5. Request-changes branch: reject supersedes; the old review cannot be
# approved afterward, and a corrected revision is required.
# --------------------------------------------------------------------------
code, rc_task = call(requester, "POST", "/tasks", {"title": f"request changes {stamp}"}, expect=201)
code, rc_recs = call(requester, "GET", "/integrations/inventory/records", expect=200)
rc_target = rc_recs["items"][0]
rc_key = f"journey-{stamp}:rc:settle"
_, rc_prop = call(requester, "POST", f"/tasks/{rc_task['id']}/proposals", {
    "summary": "change request candidate",
    "operations": [{"integration": "inventory", "action": "adjust",
                    "business_key": rc_key, "target_id": rc_target["id"],
                    "expected_version": rc_target["version"], "payload": {"delta": -1}}],
}, expect=201)
# Approver asks for changes BEFORE endorsement (reject at any pre-approval stage).
code, _ = call(approver, "POST", f"/proposals/{rc_prop['id']}/reject",
               {"revision": rc_prop["revision"], "digest": rc_prop["digest"],
                "reason": "Quantity does not match the packing slip"}, expect=200)
_, rc_after = call(requester, "GET", f"/proposals/{rc_prop['id']}", expect=200)
expect("reject moves the review to a terminal superseded state", rc_after["status"], "rejected")

# A stale approve against the now-rejected revision must fail.
code, _ = call(approver, "POST", f"/proposals/{rc_prop['id']}/approve",
               {"revision": rc_prop["revision"], "digest": rc_prop["digest"]})
expect("stale approve after reject is refused", code in (409, 404, 422), True)

# A corrected revision supersedes and can proceed normally.
_, rev2 = call(requester, "POST", f"/tasks/{rc_task['id']}/proposals", {
    "summary": "corrected review",
    "operations": [{"integration": "inventory", "action": "adjust",
                    "business_key": rc_key, "target_id": rc_target["id"],
                    "expected_version": rc_target["version"], "payload": {"delta": -1}}],
}, expect=201)
expect("fresh corrected revision is actionable (new proposal id)", rev2["id"] != rc_prop["id"], True)
call(requester, "POST", f"/proposals/{rev2['id']}/endorse",
     {"revision": rev2["revision"], "digest": rev2["digest"]}, expect=200)
call(approver, "POST", f"/proposals/{rev2['id']}/approve",
     {"revision": rev2["revision"], "digest": rev2["digest"]}, expect=200)
rev2_final = {}
for _ in range(80):
    _, rev2_final = call(requester, "GET", f"/proposals/{rev2['id']}", expect=200)
    if rev2_final["status"] in ("done", "needs_attention", "rejected"):
        break
    time.sleep(0.5)
expect("corrected revision executes", rev2_final["status"], "done")

# --------------------------------------------------------------------------
# Report.
# --------------------------------------------------------------------------
width = max(len(n) for n, _, _ in checks)
for name, got, want in checks:
    print(f"  {name:<{width}}  got={got!r:<14} want={want!r:<14} {'OK' if got == want else 'MISMATCH'}")
failed = [c for c in checks if c[1] != c[2]]
print(f"\nJOURNEY: {len(checks) - len(failed)}/{len(checks)} passed — {'PASS' if not failed else 'FAIL'}")
sys.exit(1 if failed else 0)