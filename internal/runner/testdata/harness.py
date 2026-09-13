#!/usr/bin/env python3
"""Test-double harness for the CLI runner's conformance tests.

This is a REAL subprocess: the runner launches it, writes the run context to its
stdin, and validates its stdout through the same proposal path any harness uses.
It needs no model credential, so the execution boundary (isolation, timeout,
output caps, failure mapping) can be verified without pretending an AI ran.

Mode comes from HARNESS_MODE:
  success    emit a valid draft with a business-fact key
  waiting    emit a gate asking for input
  runscoped  emit a draft whose business_key embeds the run id (must be rejected)
  garbage    emit non-JSON
  slow       sleep well past any configured timeout
  overflow   emit ~2MB (must trip the output cap and be killed)
  nonzero    write to stderr and exit non-zero
  envdump    write the child environment to $HARNESS_ENV_OUT, then succeed
  stdin      write the received stdin to $HARNESS_STDIN_OUT, then succeed
"""
import json
import os
import sys
import time

MODE = os.environ.get("HARNESS_MODE", "success")

if MODE == "slow":
    time.sleep(120)

if MODE == "garbage":
    sys.stdout.write("definitely not json\n")
    sys.exit(0)

if MODE == "overflow":
    block = "x" * 8192
    for _ in range(256):  # ~2MB
        sys.stdout.write(block + "\n")
    sys.exit(0)

if MODE == "nonzero":
    sys.stderr.write("harness exploded: internal detail that must not surface\n")
    sys.exit(3)

raw = sys.stdin.read()
try:
    req = json.loads(raw)
except Exception:
    sys.stderr.write("could not parse stdin\n")
    sys.exit(2)

if MODE == "envdump":
    path = os.environ.get("HARNESS_ENV_OUT")
    if path:
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(dict(os.environ), fh)

if MODE == "stdin":
    path = os.environ.get("HARNESS_STDIN_OUT")
    if path:
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(raw)

run_id = req.get("run_id", "run")
records = req.get("records") or []
mail = next((r for r in records if r.get("integration") == "mail"), None)

if MODE == "waiting":
    print(json.dumps({
        "status": "waiting",
        "summary": "Need supplier details",
        "gate": {
            "kind": "missing_information",
            "prompt": "Which supplier and quantity?",
            "input_schema": {
                "properties": {
                    "supplier_email": {"type": "string"},
                    "quantity": {"type": "integer"},
                },
                "required": ["supplier_email", "quantity"],
            },
        },
    }))
    sys.exit(0)

GATE = {
    "kind": "missing_information",
    "prompt": "Which supplier, and what quantity should this follow-up cover?",
    "input_schema": {
        "properties": {
            "supplier_email": {"type": "string"},
            "quantity": {"type": "integer"},
        },
        "required": ["supplier_email", "quantity"],
    },
}

if MODE == "auto":
    # Pause until the human supplies what is missing, then use their answer.
    # `or []` as well as a default: a key present with a null value still yields None.
    supplied = {i.get("name"): i.get("value") for i in (req.get("inputs") or []) if i.get("value")}
    if not supplied.get("supplier_email") or not supplied.get("quantity"):
        print(json.dumps({"status": "waiting", "summary": "Need supplier details", "gate": GATE}))
        sys.exit(0)
    email = json.loads(supplied["supplier_email"])
    qty = int(json.loads(supplied["quantity"]))
    if mail is None:
        print(json.dumps({"status": "failed", "failure_reason": "no_suitable_record"}))
        sys.exit(0)
    print(json.dumps({
        "status": "succeeded",
        "summary": "Prepared supplier follow-up for %s" % email,
        "operations": [{
            "integration": "mail",
            "action": "send",
            # A BUSINESS-FACT key: identifies the real enquiry, not this run. Two
            # independent runs proposing the same follow-up must collide, or
            # duplicate detection is defeated.
            "business_key": "supplier-followup:" + email,
            "target_id": mail["id"],
            "expected_version": mail.get("version", 1),
            "payload": {
                "recipients": [email],
                "subject": "Stock adjustment request",
                "body": "Requesting a stock adjustment of %d units." % qty,
                "bcc": [],
                "attachments": [],
            },
        }],
    }))
    sys.exit(0)

if mail is None:
    print(json.dumps({"status": "failed", "failure_reason": "no_suitable_record"}))
    sys.exit(0)

# A business-fact key, deliberately NOT derived from the run id.
key = "supplier-followup:ACME-2026-0042"
if MODE == "runscoped":
    key = "run:%s:%s" % (run_id, mail["id"])

print(json.dumps({
    "status": "succeeded",
    "summary": "Prepared supplier follow-up",
    "operations": [{
        "integration": "mail",
        "action": "send",
        "business_key": key,
        "target_id": mail["id"],
        "expected_version": mail.get("version", 1),
        "payload": {
            "recipients": ["ops@supplier.test"],
            "subject": "Stock adjustment request",
            "body": "Requesting a stock adjustment.",
            "bcc": [],
            "attachments": [],
        },
    }],
}))
