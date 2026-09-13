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

# 5. Agent runs: the gate, the run, and the fact that a run only ever prepares work.
admin_tok = admin
_, run_task = call(requester, "POST", "/tasks", {"title": "live agent run"}, expect=201)
_, agent = call(requester, "POST", "/agents", {"name": "Live Check Agent", "harness": "simulator"}, expect=201)
run_body = {"agent_id": agent["id"], "intent": "inventory"}
code, first = call(requester, "POST", f"/tasks/{run_task['id']}/runs", run_body)
if code == 409:
    expect("run refused without an active harness", first["error"]["code"], "no_active_harness")
    revision = None
    code, hrel = call(admin_tok, "POST", "/registry/releases", {
        "family": "harness-live-" + stamp, "kind": "harness", "version": "1.0.0-" + stamp,
        "digest": "sha256:live-" + stamp, "requested_capabilities": ["prepare_proposal"],
        "simulation": True, "compatibility_range": ">=1",
        # A harness release only binds to a runner it names. Without this the
        # release can never satisfy an agent whose harness is "simulator".
        "manifest": {"runner_id": "simulator"}})
    expect("harness release registered", code, 201)
    for step in ("verified", "approved", "installed", "active"):
        code, hrel = call(admin_tok, "POST", f"/registry/releases/{hrel['id']}/transition",
                          {"expected_version": hrel["revision"], "target_state": step, "reason": "live check"})
        expect(f"harness promoted to {step}", code, 200)
    code, first = call(requester, "POST", f"/tasks/{run_task['id']}/runs", run_body)
expect("run accepted once a harness is active", code, 201)
expect("run queued, not falsely working", first["status"], "queued")
expect("run records its harness release", bool(first["harness_release_id"]), True)

finished = {}
for _ in range(60):
    _, finished = call(requester, "GET", f"/runs/{first['id']}", expect=200)
    if finished["status"] in ("succeeded", "failed"):
        break
    time.sleep(0.5)
expect("run completed", finished["status"], "succeeded")
expect("run recorded a finish time", bool(finished["finished_at"]), True)
expect("run linked a proposal", bool(finished["proposal_id"]), True)
if finished["proposal_id"]:
    _, rp = call(requester, "GET", f"/proposals/{finished['proposal_id']}", expect=200)
    expect("run output still needs endorsement", rp["status"], "pending_endorsement")
code, _ = call(other, "GET", f"/runs/{first['id']}")
expect("cross-org run read denied", code, 404)



# 6. Pause, resume, and the promise that other work keeps moving.
_, pause_agent = call(requester, "POST", "/agents", {"name": "Live Pause Agent", "harness": "simulator"}, expect=201)
_, pause_task = call(requester, "POST", "/tasks", {"title": "live pause resume"}, expect=201)
_, mails = call(requester, "GET", "/integrations/mail/records", expect=200)
expect("a mail destination exists for the resumed work", len(mails["items"]) > 0, True)

code, prun = call(requester, "POST", f"/tasks/{pause_task['id']}/runs",
                  {"agent_id": pause_agent["id"], "intent": "clarify the supplier quantity"}, expect=201)

parked = {}
for _ in range(40):
    _, parked = call(requester, "GET", f"/runs/{prun['id']}", expect=200)
    if parked["status"] in ("waiting", "failed"):
        break
    time.sleep(0.5)
expect("run parks when it needs human input", parked["status"], "waiting")
expect("a parked run is not a failure", parked["failure_reason"], "")
expect("a parked run publishes nothing", parked["proposal_id"], "")

_, gates = call(requester, "GET", "/gates", expect=200)
gate = next((g for g in gates["items"] if g["run_id"] == prun["id"]), None)
expect("the question is visible to its respondent", gate is not None, True)
if gate:
    good = {"supplier_email": "ops@supplier.test", "quantity": 42}
    code, _ = call(approver, "POST", f"/gates/{gate['id']}/respond",
                   {"revision": gate["revision"], "response": good})
    expect("a non-respondent cannot answer", code, 403)
    code, _ = call(requester, "POST", f"/gates/{gate['id']}/respond",
                   {"revision": gate["revision"], "response": {"supplier_email": "ops@supplier.test"}})
    expect("a malformed answer is refused", code, 422)
    call(requester, "POST", f"/gates/{gate['id']}/respond",
         {"revision": gate["revision"], "response": good}, expect=200)

    resumed = {}
    for _ in range(60):
        _, resumed = call(requester, "GET", f"/runs/{prun['id']}", expect=200)
        if resumed["status"] in ("succeeded", "failed"):
            break
        time.sleep(0.5)
    expect("the answered run resumes to success", resumed["status"], "succeeded")
    if resumed["proposal_id"]:
        _, rp2 = call(requester, "GET", f"/proposals/{resumed['proposal_id']}", expect=200)
        expect("resumed output still needs a human", rp2["status"], "pending_endorsement")


# 7. A REAL subprocess harness, through the whole governed loop.
#
# Everything above used the in-process simulator. This section configures a harness
# that is launched as a SEPARATE PROCESS by the CLI runner, receives the run context
# on stdin, and whose stdout is validated by the same proposal path any harness's
# output takes. It still needs no model credential, so this proves the execution
# boundary and the pause/resume loop without pretending an AI ran.
_, h_task = call(requester, "POST", "/tasks", {"title": "live real harness run"}, expect=201)
code, h_agent = call(requester, "POST", "/agents",
                     {"name": "Live CLI Harness Agent", "harness": "test-harness"}, expect=201)

# This database persists between runs, so a PREVIOUS live check may have left an
# ACTIVE harness release binding to this runner. Revoke any such binding first, or
# the "no active release" gate below cannot be observed at all.
_, existing = call(admin, "GET", "/registry/releases", expect=200)
for r in existing.get("items") or []:
    if r.get("kind") == "harness" and r.get("state") in ("verified", "approved", "installed", "active", "draining"):
        code, _ = call(admin, "POST", f"/registry/releases/{r['id']}/transition",
                       {"expected_version": r["revision"], "target_state": "revoked",
                        "reason": "live check reset"})
        if code != 200:
            # Not every state can jump straight to revoked; disabling still stops
            # the binding from satisfying the gate.
            call(admin, "POST", f"/registry/releases/{r['id']}/transition",
                 {"expected_version": r["revision"], "target_state": "disabled",
                  "reason": "live check reset"})

# An agent whose harness has no active release must be refused, not silently simulated.
code, refused = call(requester, "POST", f"/tasks/{h_task['id']}/runs",
                     {"agent_id": h_agent["id"], "intent": "send the supplier follow-up"})
if code == 409 and refused["error"]["code"] == "harness_unsupported":
    # Fail with something actionable instead of a confusing assertion: this section
    # only works when BOTH the api and worker processes can actually run the harness.
    harness_path = pathlib.Path(__file__).resolve().parents[1] / "internal/runner/testdata/harness.py"
    raise SystemExit(
        "This live check needs the CLI harness runner configured on BOTH the api and "
        "worker processes. Relaunch them with:\n"
        "  export WORKFORCE_RUNNER_CLI_IDS=test-harness\n"
        "  export WORKFORCE_RUNNER_CLI_TEST_HARNESS_COMMAND='python3 %s'\n"
        "  export WORKFORCE_RUNNER_CLI_TEST_HARNESS_ENV=HARNESS_MODE\n"
        "  export WORKFORCE_RUNNER_CLI_TEST_HARNESS_TIMEOUT=30s\n"
        "  export HARNESS_MODE=auto\n" % harness_path)
if code != 409:
    raise SystemExit(f"expected the run to be refused while no harness release is active, got {code}: {refused}")
expect("real harness refused without an active release", code, 409)
expect("refusal names the missing harness", refused["error"]["code"], "no_active_harness")

code, h_rel = call(admin, "POST", "/registry/releases", {
    "family": "harness-real-" + stamp, "kind": "harness", "version": "1.0.0-" + stamp,
    "digest": "sha256:real-" + stamp, "requested_capabilities": ["prepare_proposal"],
    # This release is a TEST DOUBLE, so it is marked as a simulation honestly.
    "simulation": True, "compatibility_range": ">=1",
    "manifest": {"runner_id": "test-harness"}}, expect=201)
for step in ("verified", "approved", "installed", "active"):
    code, h_rel = call(admin, "POST", f"/registry/releases/{h_rel['id']}/transition",
                       {"expected_version": h_rel["revision"], "target_state": step, "reason": "live check"})
    expect(f"real harness promoted to {step}", code, 200)

code, h_run = call(requester, "POST", f"/tasks/{h_task['id']}/runs",
                   {"agent_id": h_agent["id"], "intent": "send the supplier follow-up"}, expect=201)
expect("real harness run accepted", code, 201)
expect("run binds the real runner", h_run["runner_id"], "test-harness")

h_state = {}
for _ in range(60):
    _, h_state = call(requester, "GET", f"/runs/{h_run['id']}", expect=200)
    if h_state["status"] in ("waiting", "failed", "succeeded"):
        break
    time.sleep(0.5)
expect("real harness asks instead of guessing", h_state["status"], "waiting")
expect("its process published nothing while paused", h_state["proposal_id"], "")

_, h_gates = call(requester, "GET", "/gates", expect=200)
h_gate = next((g for g in h_gates["items"] if g["run_id"] == h_run["id"]), None)
expect("its question reaches the respondent", h_gate is not None, True)
if h_gate:
    supplier = f"ops+{stamp}@supplier.test"
    call(requester, "POST", f"/gates/{h_gate['id']}/respond",
         {"revision": h_gate["revision"],
          "response": {"supplier_email": supplier, "quantity": 7}}, expect=200)
    resumed_state = {}
    for _ in range(80):
        _, resumed_state = call(requester, "GET", f"/runs/{h_run['id']}", expect=200)
        if resumed_state["status"] in ("succeeded", "failed"):
            break
        time.sleep(0.5)
    expect("real harness resumes after the answer", resumed_state["status"], "succeeded")
    if resumed_state["proposal_id"]:
        _, hp = call(requester, "GET", f"/proposals/{resumed_state['proposal_id']}", expect=200)
        expect("its output still needs a human", hp["status"], "pending_endorsement")
        ops = json.dumps(hp.get("operations") or [])
        expect("the human's answer reached the work", supplier in ops, True)
        expect("the key names the real enquiry, not the run",
               str(h_run["id"]) not in ops, True)

width = max(len(n) for n, _, _ in checks)
for name, got, want in checks:
    print(f"  {name:<{width}}  got={got!r:<12} want={want!r:<12} {'OK' if got == want else 'MISMATCH'}")
failed = [c for c in checks if c[1] != c[2]]
print(f"\nLIVE CHECK: {len(checks) - len(failed)}/{len(checks)} passed — {'PASS' if not failed else 'FAIL'}")
sys.exit(1 if failed else 0)
