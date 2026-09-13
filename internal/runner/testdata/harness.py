#!/usr/bin/env python3
"""Test-double harness for the CLI runner's conformance tests.

This is a REAL subprocess: the runner launches it, writes the run context to its
stdin, and validates its stdout through the same path any harness's output takes.
It needs no model credential, so the execution boundary, the envelope handling and
the pause/resume loop can all be verified without pretending an AI ran.

Mode comes from HARNESS_MODE:
  success     emit our protocol directly: a valid draft with a business-fact key
  waiting     emit our protocol directly: a gate asking for input
  auto        wait for input, then succeed with the answer applied
  runscoped   draft whose business_key embeds the run id (must be rejected)
  garbage     non-JSON
  slow        sleep well past any configured timeout
  overflow    ~2MB (must trip the output cap and be killed)
  nonzero     write to stderr and exit non-zero
  envdump     record the child environment to $HARNESS_ENV_OUT, then succeed
  stdin       record the received stdin to $HARNESS_STDIN_OUT, then succeed

Envelope modes mirror what real harnesses actually emit, so the adapter's
unwrapping is exercised for real rather than assumed:
  claudelike  one JSON object: {"type":"result",...,"result":"<protocol as text>"}
  claudeerror one JSON object with "is_error": true
  codexlike   JSONL events, the last carrying "result"
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

run_id = req.get("run_id", "run")
records = req.get("records") or []
mail = next((r for r in records if r.get("integration") == "mail"), None)
supplied = {i.get("name"): i.get("value") for i in (req.get("inputs") or []) if i.get("value")}


def waiting_doc():
    return {"status": "waiting", "summary": "Need supplier details", "gate": GATE}


def success_doc():
    # Mode-agnostic: modes that are not about the gate at all (success, runscoped,
    # envdump, stdin, and the envelope shapes) still need usable values, so default
    # rather than assuming a human supplied them.
    raw_email = supplied.get("supplier_email") or '"ops@supplier.test"'
    raw_qty = supplied.get("quantity") or "42"
    email = json.loads(raw_email)
    qty = int(json.loads(raw_qty))
    # A BUSINESS-FACT key: identifies the real enquiry, not this run. Two independent
    # runs proposing the same follow-up must collide, or duplicate detection is defeated.
    key = "supplier-followup:" + email
    if MODE == "runscoped":
        key = "run:%s:%s" % (run_id, mail["id"])
    return {
        "status": "succeeded",
        "summary": "Prepared supplier follow-up for %s" % email,
        "operations": [{
            "integration": "mail",
            "action": "send",
            "business_key": key,
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
    }


def protocol_doc():
    """The protocol document this harness would emit, per mode."""
    if MODE == "waiting":
        return waiting_doc()
    if MODE == "auto" and (not supplied.get("supplier_email") or not supplied.get("quantity")):
        return waiting_doc()
    if MODE in ("success", "runscoped", "auto", "claudelike", "codexlike") and mail is None:
        return {"status": "failed", "failure_reason": "no_suitable_record"}
    return success_doc()


def emit(obj):
    json.dump(obj, sys.stdout)
    sys.stdout.write("\n")


# --- envelope modes: what real harnesses actually print ---
if MODE == "claudelike":
    # Claude Code --output-format json: one object whose "result" carries the text.
    emit({
        "type": "result", "subtype": "success", "is_error": False,
        "session_id": "session-abc", "total_cost_usd": 0.01, "num_turns": 3,
        "result": json.dumps(protocol_doc()),
    })
    sys.exit(0)

if MODE == "claudeerror":
    # Shape copied from the real binary: a failure arrives with subtype "success"
    # and is_error true, with the reason in terminal_reason. Do NOT "tidy" this to
    # an error-looking subtype -- that is not what the vendor sends, and a fixture
    # that disagrees with reality makes the parser's correctness coincidental.
    emit({
        "type": "result", "subtype": "success", "is_error": True,
        "terminal_reason": "api_error", "api_error_status": 400,
        "session_id": "session-abc", "total_cost_usd": 0, "num_turns": 1,
        "result": "Credit balance is too low - provider detail that must not surface",
    })
    sys.exit(0)

if MODE == "genericnoise":
    # The protocol document appears ONLY inside a generic field. A harness that runs
    # tools emits message/text/content constantly, so accepting those risks treating
    # quoted tool output or a partial message as the authoritative answer.
    emit({
        "type": "assistant",
        "message": json.dumps(protocol_doc()),
        "text": json.dumps(protocol_doc()),
        "content": json.dumps(protocol_doc()),
        "result": None,
    })
    sys.exit(0)

if MODE == "streamfail":
    # An early event looks successful; a later one reports failure. The failure must
    # win, or a run would publish work the harness itself rejected.
    emit({"type": "progress", "result": json.dumps(protocol_doc())})
    emit({"type": "result", "is_error": True, "terminal_reason": "api_error",
          "result": "failed after appearing to succeed"})
    sys.exit(0)

if MODE == "codexlike":
    # Codex exec --json: a stream of events, the last carrying the final message.
    emit({"type": "session.started", "session_id": "s-1"})
    emit({"type": "agent.message", "text": "working"})
    emit({"type": "task.completed", "result": json.dumps(protocol_doc())})
    sys.exit(0)

# --- plain protocol modes ---
emit(protocol_doc())
