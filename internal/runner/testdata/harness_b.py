#!/usr/bin/env python3
"""A SECOND test-double harness, deliberately unlike the first.

One harness proves the boundary works. Two harnesses that share nothing but the
protocol prove the boundary is a CONTRACT rather than one adapter's shape. So this
one differs on every axis that could be accidentally baked in:

  * Invocation: it takes its context on stdin like any harness, but it is invoked
    with an ARGUMENT that changes nothing about the protocol — the protocol is not
    allowed to depend on how the command line happens to be shaped.
  * Envelope: it nests the protocol document TWO layers deep through the same
    documented terminal key, where the first double nests once. If unwrapping
    handled only one layer, that would have looked like a general solution and
    been wrong.
    It deliberately does not invent a new outer key: the adapter unwraps only
    vendor-documented terminal fields, and a harness claiming an arbitrary key
    SHOULD be refused. This double was first written with an invented
    "payload.envelope" wrapper and was correctly rejected with "no protocol
    document found" — the adapter's restraint is the feature, not the bug.
  * Question shape: a `selection` gate over a scalar enum, where the first double
    asks a `missing_information` question with a string and an integer. A gate
    path that assumed one question shape would pass one harness and fail the other.
  * Continuation: it has NO native continuation. Every invocation is fresh and
    deliberately non-deterministic in a way the protocol does not expose, so the
    platform must never assume it can resume in-memory state. This is documented
    rather than promised away.
  * Process shape: in `children` mode it spawns a long-lived grandchild, because a
    run that is killed must take its whole process tree with it. A harness that
    leaves orphans behind is a containment hole, and only a harness that actually
    forks can show it.

Modes come from HARNESS_MODE_B:
  success    a plain successful draft
  selection  ask a selection gate, then succeed once it is answered
  children   spawn a grandchild, record its pid, then hang past any timeout
  garbage    non-JSON
"""
import json
import os
import subprocess
import sys
import time

MODE = os.environ.get("HARNESS_MODE_B", "success")

if MODE == "children":
    # Record the grandchild's pid so the test can prove it was killed too, then
    # outlive any configured timeout.
    pid_path = os.environ.get("HARNESS_B_CHILD_PID_OUT")
    proc = subprocess.Popen(["sleep", "300"])
    if pid_path:
        with open(pid_path, "w", encoding="utf-8") as fh:
            fh.write(str(proc.pid))
    time.sleep(120)

raw = sys.stdin.read()
try:
    req = json.loads(raw)
except Exception:
    sys.stderr.write("could not parse stdin\n")
    sys.exit(2)

if MODE == "garbage":
    sys.stdout.write("]]]not json at all[[[\n")
    sys.exit(0)

records = req.get("records") or []
mail = next((r for r in records if r.get("integration") == "mail"), None)
supplied = {
    (i.get("name")): (i.get("value"))
    for i in (req.get("inputs") or [])
    if i.get("value")
}

# A different question shape from the first double: a named choice, not free text.
SELECTION_GATE = {
    "kind": "selection",
    "prompt": "Which correction should the reply describe?",
    "input_schema": {
        "type": "string",
        "enum": ["stock_mismatch", "price_mismatch", "both"],
    },
}


def selection_doc():
    return {
        "status": "waiting",
        "summary": "Need the correction type",
        "gate": SELECTION_GATE,
    }


def success_doc():
    # Default already expressed as JSON, matching the first double's convention so
    # the two harnesses differ in shape but not in how a value is carried.
    choice = json.loads(supplied.get("selection") or '"stock_mismatch"')
    return {
        "status": "succeeded",
        "summary": "Prepared a %s reply" % choice,
        "operations": [
            {
                "integration": "mail",
                "action": "send",
                # Business-fact key, exactly like the first harness: the protocol
                # is shared even though nothing else is.
                "business_key": "supplier-reply:%s:%s" % (mail["id"], choice),
                "target_id": mail["id"],
                "expected_version": mail.get("version", 1),
                "payload": {
                    "recipients": ["claims@supplier.test"],
                    "subject": "Correction: %s" % choice,
                    "body": "We are raising a %s against your last delivery." % choice,
                    "bcc": [],
                    "attachments": [],
                },
            }
        ],
    }


def protocol_doc():
    if MODE == "selection" and not supplied.get("selection"):
        return selection_doc()
    if mail is None:
        return {"status": "failed", "failure_reason": "no_suitable_record"}
    return success_doc()


# A deliberately DIFFERENT envelope: the protocol is nested TWO layers deep
# through the documented terminal key, where the first double nests once. This
# exercises the adapter's bounded recursion rather than one fixed depth.
#
# It deliberately does NOT invent a new wrapper key. The adapter unwraps only
# vendor-documented TERMINAL fields ("result"), because names like "message",
# "text" and "output" are generic enough that a tool result or a partial
# assistant message could be mistaken for the authoritative answer. A harness
# that claimed a new outer key SHOULD be refused, so doing that here would test
# the wrong thing.
inner = {"result": json.dumps(protocol_doc())}
emit = {"result": json.dumps(inner)}
json.dump(emit, sys.stdout)
sys.stdout.write("\n")
