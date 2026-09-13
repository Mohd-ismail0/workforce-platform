#!/usr/bin/env python3
"""MANDATORY real-model pause/resume acceptance.

Purpose: prove the pause path with a REAL model, not only with the deterministic
harness. The earlier real-model run went straight to `succeeded` because the shared
organisation's snapshot exposed historical supplier details, so the model had no reason
to ask. A qualification that accepts that as success qualifies nothing.

Design consequences:
  * a FRESH organisation holding exactly ONE record that does not contain the missing
    fact, so the model must ask;
  * reaching `succeeded` before a human answers is a FAILURE of this scenario, not a
    shortcut to accept;
  * the answer is built ONLY from predeclared fixture facts. Unknown required fields
    abort with the field named, rather than inventing values or defaulting booleans.

The scripted answer is a SIMULATED human input: it stands in for a person, and is
labelled as such. It is not a human interaction and proves nothing about usability.

Exit code 0 only if the pause path is exercised and every assertion holds.
"""
import json
import sys
import time
import urllib.error
import urllib.request

ORG, REQ, APP, ADM, REC, TOKN, RUNNER = sys.argv[1:8]
# Optional: the database URL, used ONLY to verify proposal-linked readback and the
# exact target version. The join lives in the schema; the receipts API does not
# expose it, and asserting a field that does not exist proved nothing.
DB_URL = sys.argv[8] if len(sys.argv) > 8 else ""

BASE = "http://127.0.0.1:8095/api/v1"
TOKENS = {(ORG, REQ): TOKN, (ORG, APP): TOKN + "b", (ORG, ADM): TOKN + "c"}
WHO = {"requester": (ORG, REQ), "approver": (ORG, APP), "admin": (ORG, ADM)}


def call(role, method, path, body=None):
    actor = WHO[role]
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        BASE + path, data=data, method=method,
        headers={"Authorization": "Bearer " + TOKENS[actor], "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, json.loads(r.read() or b"null")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"null")


results = []
def check(name, got, want):
    good = got == want
    results.append((name, good))
    print(f"  {'PASS' if good else 'FAIL'}  {name}  got={got!r} want={want!r}")
    return good

def poll(path, done, limit=200):
    last = {}
    for _ in range(limit):
        _, last = call("requester", "GET", path)
        if last.get("status") in done:
            return last
        time.sleep(1)
    return last

# ---------------------------------------------------------------- preconditions
print("=== preconditions ===")
check("the organisation holds exactly one scoped record", bool(REC), True)
_, recs = call("requester", "GET", "/integrations/mail/records")
check("no OTHER mail record leaks into the snapshot", len(recs.get("items", [])), 1)
before_version = (recs["items"][0]["version"] if recs.get("items") else None)
print(f"    record {REC[:10]}… version={before_version}")

print()
print("=== dedicated harness release for this runner ===")
stamp = str(int(time.time()))
code, rel = call("admin", "POST", "/registry/releases", {
    "family": f"pause-qual-{stamp}", "kind": "harness", "version": f"1.0.0-{stamp}",
    "digest": f"sha256:pause-{stamp}", "requested_capabilities": ["prepare_proposal"],
    "simulation": True, "compatibility_range": ">=1", "manifest": {"runner_id": RUNNER}})
check("harness release registered", code, 201)
for step in ("verified", "approved", "installed", "active"):
    code, rel = call("admin", "POST", f"/registry/releases/{rel['id']}/transition",
                     {"expected_version": rel["revision"], "target_state": step, "reason": "pause qualification"})
check("harness release active", rel.get("state"), "active")

# ---------------------------------------------------------------- A must pause
print()
print("=== run A: the destination for this enquiry is genuinely unknown ===")
code, task = call("requester", "POST", "/tasks", {"title": f"REAL pause acceptance {stamp}"})
check("task A created", code, 201)
code, agent = call("requester", "POST", "/agents",
                   {"name": f"Pause Qual Agent {stamp}", "harness": RUNNER})
check("agent registered to the real runner", code, 201)

code, run_a = call("requester", "POST", f"/tasks/{task['id']}/runs",
                   {"agent_id": agent["id"], "intent": "send the supplier follow-up"})
check("run A accepted", code, 201)
check("run A bound to the real runner", run_a.get("runner_id"), RUNNER)

st_a = poll(f"/runs/{run_a['id']}", {"waiting", "failed", "succeeded"})
print(f"    run A terminal: {st_a.get('status')!r} reason={st_a.get('failure_reason')!r}")
if st_a.get("status") == "succeeded":
    print("    !! the model completed WITHOUT asking. This scenario REQUIRES a question:")
    print("       a fresh scope exists precisely so the missing fact cannot be inferred.")
check("the real model ASKED instead of guessing", st_a.get("status"), "waiting")
check("nothing was published while run A waits", st_a.get("proposal_id"), "")
if st_a.get("status") != "waiting":
    print("\nPAUSE ACCEPTANCE: FAILED — the pause path was not exercised")
    sys.exit(1)

# ---------------------------------------------------------------- gate + routing
print()
print("=== the question is durable and routed to a person ===")
_, gates = call("requester", "GET", "/gates")
gate = next((g for g in gates.get("items", []) if g.get("run_id") == run_a["id"]), None)
check("a gate is persisted for run A", gate is not None, True)
if gate is None:
    sys.exit(1)
check("the gate is routed to the accountable owner", gate.get("respondent_id"), REQ)
print(f"    prompt: {str(gate.get('prompt'))[:160]}")

# Only predeclared facts may be supplied.
FACTS = {
    "email": f"ops+{stamp}@supplier.test",
    "quantity": 7,
    "business_ref": f"ACME-{stamp}",
    "subject": "Stock adjustment request",
    "body": "Requesting a stock adjustment of 7 units.",
}
# Field names map to PREDECLARED facts only. No name is guessed and no boolean is
# invented; an unmapped or unbuildable required field aborts the acceptance instead.
NAME_MAP = {
    "supplier_email": "email", "recipient_email": "email", "recipient": "email",
    "to": "email", "email": "email", "recipients": "email",
    "quantity": "quantity", "qty": "quantity",
    "supplier_business_key": "business_ref", "business_key_ref": "business_ref",
    "reference": "business_ref", "supplier_reference": "business_ref",
    "subject": "subject", "body": "body", "message": "body", "content": "body",
}
def coerce(value, spec):
    """Shape a predeclared fact to the type the model actually asked for.

    The strict acceptance previously failed with 422 because the model asked for
    `recipients` as an ARRAY of strings and the driver sent a bare string. Sending a
    value that does not match the declared type is the driver's bug, not the model's.
    Returns (ok, value); an unsupported declared type is reported, never guessed.
    """
    t = (spec or {}).get("type", "string")
    if t == "array":
        items = (spec or {}).get("items") or {}
        it = items.get("type")
        if it == "string":
            return True, [str(value)]
        if it == "integer":
            return True, [int(value)]
        if it == "number":
            return True, [float(value)]
        # An array of anything else cannot be built from the declared facts.
        return False, None
    if t == "string":
        return True, str(value)
    if t == "integer":
        return True, int(value)
    if t == "number":
        return True, float(value)
    if t == "boolean":
        # No predeclared boolean fact exists, and inventing one would be guessing.
        return False, None
    return False, None


schema = gate.get("input_schema") or {}
props = schema.get("properties") or schema.get("requested") or {}
required = schema.get("required") or list(props.keys())
answer, unresolved = {}, []
for field in required:
    key = NAME_MAP.get(field.lower())
    if not (key and key in FACTS):
        unresolved.append(f"{field} (no predeclared fact)")
        continue
    ok, value = coerce(FACTS[key], props.get(field) or {})
    if not ok:
        unresolved.append(f"{field} (declared type {(props.get(field) or {}).get('type')} cannot be built)")
        continue
    answer[field] = value
check("every required field maps to a PREDECLARED fact", unresolved, [])
if unresolved:
    print("    the model asked for facts this qualification does not declare:", unresolved)
    print("    refusing to invent values; add a declared fixture fact and re-run.")
    sys.exit(1)
print(f"    supplying (simulated human input): {sorted(answer)}")

# ------------------------------------------------- A holds no worker while B runs
print()
print("=== independent work proceeds while A waits ===")
code, task_b = call("requester", "POST", "/tasks", {"title": f"REAL pause holdout {stamp}"})
code, agent_b = call("requester", "POST", "/agents",
                     {"name": f"Pause Holdout Agent {stamp}", "harness": RUNNER})
code, run_b = call("requester", "POST", f"/tasks/{task_b['id']}/runs",
                   {"agent_id": agent_b["id"], "intent": "send the supplier follow-up"})
check("independent run B accepted while A waits", code, 201)
st_b = poll(f"/runs/{run_b['id']}", {"waiting", "succeeded", "failed"})
print(f"    run B reached: {st_b.get('status')!r} (A still parked, so B had the worker)")
check("run B was processed while A held no slot",
      st_b.get("status") in ("waiting", "succeeded"), True)
_, still_a = call("requester", "GET", f"/runs/{run_a['id']}")
check("A is still waiting after B progressed", still_a.get("status"), "waiting")

# ---------------------------------------------------------------- answer + resume
print()
print("=== a human answers; a FRESH invocation resumes with it ===")
code, _ = call("requester", "POST", f"/gates/{gate['id']}/respond",
               {"revision": gate["revision"], "response": answer})
check("answer accepted", code, 200)
st_final = poll(f"/runs/{run_a['id']}", {"succeeded", "failed", "waiting"})
print(f"    run A final: {st_final.get('status')!r} reason={st_final.get('failure_reason')!r}")
check("run A succeeded on the resumption", st_final.get("status"), "succeeded")
pid = st_final.get("proposal_id") or ""
check("the resumption produced a proposal", bool(pid), True)
if not pid:
    print("\nPAUSE ACCEPTANCE: FAILED — no proposal after resuming")
    sys.exit(1)

# ---------------------------------------------------------------- review boundary
print()
print("=== the proposal needs humans; the target is untouched ===")
_, prop = call("requester", "GET", f"/proposals/{pid}")
check("proposal awaits endorsement", prop.get("status"), "pending_endorsement")
ops = json.dumps(prop.get("operations") or [])
check("the operation targets the scoped record", REC in ops, True)
check("the business key is not run-scoped", str(run_a["id"]) not in ops, True)
check("the human's answer reached the prepared work", FACTS["email"] in ops, True)
_, after = call("requester", "GET", "/integrations/mail/records")
now = next((r for r in after.get("items", []) if r["id"] == REC), None)
check("the record is UNCHANGED before approval", now and now["version"], before_version)

print()
print("=== endorsement, a DIFFERENT approver, then the simulated effect ===")
d = {"revision": prop["revision"], "digest": prop["digest"]}
check("requester endorsed", call("requester", "POST", f"/proposals/{pid}/endorse", d)[0], 200)
check("requester cannot approve their own proposal",
      call("requester", "POST", f"/proposals/{pid}/approve", d)[0], 409)
check("independent approver approved",
      call("approver", "POST", f"/proposals/{pid}/approve", d)[0], 200)
done = poll(f"/proposals/{pid}", {"done", "needs_attention"})
check("proposal executed", done.get("status"), "done")

# Proposal-specific readback, checked where the linkage actually exists:
# receipts.effect_id -> effects.proposal_id. (The earlier version asserted a
# proposal_id field on the receipts API, which does not exist, so it could never pass.)
linked = 0
final_version = None
if DB_URL:
    import psycopg
    with psycopg.connect(DB_URL, connect_timeout=10) as c:
        with c.transaction():
            c.execute("select set_config('app.org_id', %s, true)", (ORG,))
            linked = c.execute(
                """SELECT count(*)
                     FROM receipts r
                     JOIN effects e ON e.id = r.effect_id
                    WHERE e.org_id = %s AND e.proposal_id = %s""",
                (ORG, pid)).fetchone()[0]
            final_version = c.execute(
                "SELECT version FROM simulator_records WHERE org_id = %s AND id = %s",
                (ORG, REC)).fetchone()[0]
    check("a receipt is linked to THIS proposal's effect", linked >= 1, True)
    # The target must advance by EXACTLY one version: an effect applied twice would show
    # as +2, and no effect would leave it unchanged.
    check("the target advanced by exactly one version", final_version, (before_version or 0) + 1)
else:
    print("    (no DB URL supplied: proposal-linked readback and exact target version "
          "are NOT verified by this run)")

failed = [n for n, good in results if not good]
print()
print(f"REAL PAUSE/RESUME ACCEPTANCE: {len(results)-len(failed)}/{len(results)} passed — "
      f"{'PASS' if not failed else 'FAIL'}")
print(f"  run_a={run_a['id']} (waited, then resumed)  run_b={run_b['id']} (holdout)  proposal={pid}")
print("  NOTE: the answer was a SIMULATED human input supplied by a script, not a person.")
for n in failed:
    print("   failed:", n)
sys.exit(1 if failed else 0)
