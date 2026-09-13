# Harness adapter — decision record

Status: implementing. This replaces the in-process simulator as the *only* runner,
without changing the authority model around it.

## What a harness is, and is not

A harness is a **subprocess the operator configured**. It prepares work. It does not
hold business credentials, does not call our API, and cannot authorise anything.

```
human-owned task
  └─ durable agent_run (claimed, leased)
       └─ configured harness subprocess        ← the new part
            ├─ artifacts / proposal draft → validated, then the normal review chain
            └─ question → durable gate → process exits, capacity released

draft → requester endorsement → authorised human approval
      → credential-holding executor → independently checked receipt
```

The subprocess is the *only* new thing. Everything downstream is unchanged and
already tested: `CreateProposal` validation, business-key reservation, endorsement,
distinct approval, governed execution, readback.

## Why a generic adapter, not a vendor SDK

The intended use case is several harnesses (Claude Code, Codex, OpenCode, Hermes,
in-house commands) behind one platform contract. Hardcoding one vendor would make the
second harness a rewrite. So the adapter takes an operator-configured argv and speaks
one JSON protocol; Claude Code is the first configured binding, not a special case.

This also means the model provider is an operator concern, not a platform dependency.

## The protocol

Stdin: one JSON run context — org/task/run/agent identifiers, the intent, the scoped
read-only records, the human answers supplied so far, and the explicit output
contract. Stdout: exactly one JSON object.

```
{"status":"succeeded","summary":str,
 "operations":[{integration,action,business_key,target_id,expected_version:int,payload:object}]}

{"status":"waiting","summary":str,
 "gate":{kind:"clarification|selection|missing_information",prompt:str,input_schema:object}}

{"status":"failed","failure_reason":str}
```

Output is **untrusted input**. A harness that invents a field, claims an operation the
connector does not support, or proposes a stale target does not get an effect — the
existing validation rejects it. A harness that wants authorisation asks for it with
`waiting`; it cannot grant it.

## Reading a real harness's output

Real harnesses do not print our protocol directly, so the adapter unwraps the
documented shapes rather than making every operator encode their harness's output
format as configuration:

- **Claude Code** — `claude -p --output-format json` prints one envelope object
  `{type, subtype, is_error, result, session_id, total_cost_usd, ...}`; the agent's
  final text is in `result`.
- **Codex** — `codex exec --json` prints a JSONL event stream; the final message is
  carried by the last event.

Unwrapping accepts the protocol document directly, an envelope wrapping it as an
object, an envelope wrapping it as a string, and a JSONL stream, up to a bounded
nesting depth. A harness that exits 0 while reporting its own failure
(`is_error: true`, or an `error*` subtype) is treated as **failed** — its text is
provider diagnostics and never reaches a user-visible field; the run records the
public code `harness_reported_error` instead.

Output is untrusted input either way. Whatever comes out is still validated by
`CreateProposal`, business-key reservation, endorsement and approval.

The harness's own session store (Claude Code `--resume <session_id>`, Codex
`exec resume`) is **not** the platform's durable state: our gates and run records
are. A paused run is resumed from the persisted answer, not by asking the harness
to remember.

## Execution boundary

**This is reduced exposure, NOT a sandbox.** The harness runs as the same unprivileged
account as the platform, so it can see what that account can see and reach the network
that account can reach. What the adapter removes is the *easy* paths: no inherited
developer environment, no SSH agent, no git credentials, no database URL, a disposable
scratch HOME, a fresh writable directory per run, no shell, and a hard time/output
budget. It does **not** provide filesystem mount isolation, a separate UID, seccomp
confinement or network egress control.

Runs are therefore only acceptable for harnesses the operator already trusts to read
this host. Kernel-level isolation (separate unprivileged UID/namespace, read-only
root, egress allowlist) is a prerequisite before running harnesses that are not
operated by the same team — and before any run against data whose exposure would
matter.

- Operator-configured executable + fixed argv. Never a shell string assembled from
  task data or a registry manifest. No `sh -c` over user input.
- Fresh scratch directory per run as the working root, `0700`, removed after.
- Explicit environment allowlist. **No inherited `os.Environ()`** — no developer
  `HOME`, no SSH agent, no Git credentials, no Docker socket, no database URL.
- Model provider credential comes from the operator's credential mechanism, not from
  the platform's business configuration.
- Process group killed on timeout, cancellation or oversized output (SIGTERM, then
  SIGKILL after a short grace). A child that outlives its parent is a leak.
- Bounded stdout (overflow kills the run) and bounded stderr used for diagnostics
  only — never persisted into a user-visible field.

### Failure codes are public

`harness_unavailable`, `timeout`, `output_overflow`, `invalid_output`,
`nonzero_exit`, `internal_error`. Backend text, stderr and stack traces never reach
the UI or the run record.

## Registry binding

A harness release binds to a runner through `manifest.runner_id`. An `active` release
means *the operator promoted this metadata*; it does not mean the platform installed
or verified executable code. The executable is whatever the operator's environment
provides, and the run fails closed with `harness_unavailable` if it is absent.

## Sources (primary)

- Claude Code CLI reference — headless `-p`, `--output-format json`, `--permission-mode`,
  `--allowedTools`/`--disallowedTools`, `--max-turns`, `--resume`, `claude auth status`:
  https://docs.claude.com/en/docs/claude-code/cli-reference
- Claude Code security — permission-based architecture, sandboxed bash, working-directory
  boundary, prompt-injection protections, managed settings, usage monitoring:
  https://docs.claude.com/en/docs/claude-code/security

Unverified and therefore not relied upon: the Codex `exec` documentation URL returned
404 at the time of writing; no Codex flag set is asserted here.

## Activation prerequisite — not yet met

**No harness binary is installed and no model credential is configured on this host.**
The adapter and its conformance tests can be completed and verified through a real
process boundary, but the model-driven path stays *unverified* until an operator:

1. installs a harness (`claude`, `codex`, …), and
2. provides a model credential through the operator credential mechanism.

Until then, no claim is made that a real agent has run. The simulator remains the
default runner and no regression to existing behaviour is acceptable.

## Isolation on the current host — measured, not assumed

Real kernel isolation is **unavailable** on the development host as it stands:

```
$ bwrap --version                       # bubblewrap 0.9.0 is installed
$ bwrap --ro-bind /usr /usr ... -- python3 -c 'print("ok")'
bwrap: setting up uid map: Permission denied      # exit 1
$ id -u ; sudo -n true
1000                                              # unprivileged
sudo: a password is required                      # cannot escalate
```

`kernel.unprivileged_userns_clone` is `1`, but creating the UID map inside the user
namespace is denied (the usual AppArmor restriction on unprivileged user namespaces).
The same mechanism backs `unshare -U`, so that route is closed too, and
`systemd-run` would need privileges to place the run in a sandbox of its own.

Two consequences, both deliberate:

1. The `isolation_required` gate is the correct behaviour, not a placeholder. A
   configured harness refuses to launch until an operator explicitly accepts that it
   runs unsandboxed as this account — so the risky mode is a chosen one.
2. Nothing here should be described as containment. Until an operator provides an
   execution identity or kernel isolation (a dedicated unprivileged UID with a
   read-only root and an egress allowlist is the smallest useful step), harness runs
   are acceptable only for harnesses already trusted to read this host.

To enable the gate on this host, an operator with privileges would need to permit
unprivileged user namespaces for the runner binary (or provide an equivalent sandbox),
then set `WORKFORCE_RUNNER_ALLOW_UNISOLATED` only if they accept the unsandboxed
trade-off instead.

## Operator setup

Configure runners entirely through the environment — no code change, no compiled-in
executable path, nothing stored in the registry:

```sh
# Which runners this deployment offers.
WORKFORCE_RUNNER_CLI_IDS=claude-code

# The executable plus its FIXED arguments. Split on whitespace with quotes honoured;
# no shell, no variable expansion — so task data can never reach a shell.
WORKFORCE_RUNNER_CLI_CLAUDE_CODE_COMMAND=claude -p --output-format json --permission-mode plan --max-turns 12

# Optional per-runner limits.
WORKFORCE_RUNNER_CLI_CLAUDE_CODE_TIMEOUT=5m          # default 2m, capped at 10m
WORKFORCE_RUNNER_CLI_CLAUDE_CODE_MAXOUTPUT=1048576   # default 1MiB
# Names only — the ONLY host variables copied into the child.
WORKFORCE_RUNNER_CLI_CLAUDE_CODE_ENV=ANTHROPIC_API_KEY

# Root for per-run scratch workspaces (created 0700, removed after each run).
WORKFORCE_RUNNER_WORK_ROOT=/var/lib/workforce/runs

# REQUIRED: explicit acknowledgment that this harness runs UNSANDBOXED as this
# account. Without it the runner fails closed with `isolation_required` and no
# process is launched. This is deliberate: reduced exposure is not containment, so
# the risky mode must be chosen, never inherited by whoever configures a runner.
WORKFORCE_RUNNER_ALLOW_UNISOLATED=1
```

Then bind work to it with an **ACTIVE** harness release whose manifest names the
runner: `{"runner_id": "claude-code"}`. An `active` release means the operator
promoted *metadata* — it does not mean the platform installed or verified any code,
and the executable is whatever the operator's environment provides. The agent definition's `harness` must match
that name. An agent whose harness has no active release is refused; an agent whose
harness this deployment cannot run is refused with a distinct error rather than
silently falling back to the simulator.

`--permission-mode plan` is deliberate: the harness prepares and proposes, and never
edits or executes. That mirrors Anthropic's own guidance that the agent prepares
while a human approves, and the platform — not the agent — decides what becomes real.

## Test dependencies

`internal/runner` conformance tests launch real subprocesses and therefore require
`python3` on PATH. No model credential and no harness binary are needed for them.
