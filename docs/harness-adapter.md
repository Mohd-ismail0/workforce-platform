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
- **Codex** — documented as a JSONL event stream whose final event carries the
  message. **The event names used by our test double are synthetic**: they were not
  taken from a real Codex invocation, and no Codex binary is installed here. The
  JSONL-handling *mechanism* is tested; compatibility with Codex's actual event
  schema is unverified and must not be claimed.

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

## Qualification status — what is installed, authenticated, and still blocked

Recorded from real invocations of the pinned binary, not from documentation alone.

| Stage | State | Evidence |
|---|---|---|
| Binary installed | **yes** | `@anthropic-ai/claude-code@2.1.270`, platform package `-linux-x64`; the vendor's own `install.cjs` placed the native binary (the npm postinstall was blocked by npm 12's script policy, so the platform package was installed explicitly and the audited placement step run directly) |
| Executable runs | **yes** | `claude --version` reports the version; it returns real vendor envelopes |
| Authenticated | **reaches the provider** | a call returns `api_error_status: 400` with `result: "Credit balance is too low"` — a **billing** response, not an authentication failure |
| Model response verified | **no — blocked on gateway credit** | the unified gateway advertises 24 models including `claude-sonnet-5` and `claude-opus-5`, and rejects calls for want of balance |
| Platform loop qualified | **mechanism only** | 55/55 through a real process boundary using a deterministic double; no model-backed run yet |
| Tool-enabled production readiness | **no** | see the isolation section: no kernel containment on this host |

The remaining blocker is an operator spend decision on the gateway the user already
uses. No credential is scraped from another service, and none is needed from the
chat: the harness reads `ANTHROPIC_BASE_URL` / `ANTHROPIC_API_KEY` through the
runner's explicit environment allowlist from a mode-600 operator file.

### The vendor envelope differs from a tidy guess

Captured verbatim from the pinned binary. The notable detail is that a failure does
**not** arrive with an error-shaped subtype:

```
type: "result"   subtype: "success"   is_error: true
terminal_reason: "api_error"          api_error_status: 400
num_turns: 1     total_cost_usd: 0    result: "Credit balance is too low"
```

Two consequences, both now enforced in code and tests:

- Only the documented **terminal** field (`result`) is unwrapped. Generic names such as
  `message`, `text` and `content` are rejected outright: a harness that runs tools emits
  them constantly, so accepting them risks publishing quoted tool output or an early
  partial message as the authoritative final answer.
- A failure anywhere in a stream **invalidates** an earlier apparent success, so a run
  cannot publish work the harness itself rejected.

A separate base-URL defect was found and fixed the same way: the operator's
`OPENAI_BASE_URL` already ends in `/v1`, and Claude Code appends its own path, so the
harness was calling `/v1/v1/messages` and receiving 404. The base is now normalised.

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

Isolation is layered, and **none of these layers is a sandbox**:

- `--permission-mode plan` is requested so the harness proposes rather than acts. Treat
  this as a *reduction in what the harness will attempt*, never as proof it cannot
  invoke a tool. The vendor's own documentation describes plan mode as a permission
  posture, and permission modes are the harness's own control surface — it is not a
  kernel boundary, and we do not rely on it as one.
- The adapter additionally disallows tools explicitly (`--disallowedTools`) and runs
  `--bare`, so no inherited hooks, plugins, MCP servers or project config are loaded.
  That is what actually keeps a qualification run from touching anything: the harness
  is given synthetic context on stdin and no capability to act on it.
- What the kernel does *not* provide here is measured and documented below (bwrap and
  unshare are unusable on this host).

For a real model-backed qualification run the guarantee is the union of those two
controls plus the synthetic task context, not the permission flag alone.

## Test dependencies

`internal/runner` conformance tests launch real subprocesses and therefore require
`python3` on PATH. No model credential and no harness binary are needed for them.
