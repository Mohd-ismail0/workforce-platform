#!/usr/bin/env bash
# Live end-to-end proof that a REAL subprocess harness drives the governed loop.
#
# Design notes (learned the hard way):
#  - Runs a PRE-BUILT binary, not `go run`, so there is exactly one process per role
#    and no orphaned child to hunt down later.
#  - Kills ONLY by explicit PID. Never `pkill -f`: a pattern can match the shell
#    running the script, which kills the caller instead of the target.
#
# The harness here is a test double, so this proves the execution boundary and the
# pause/resume loop WITHOUT needing a model credential. It does not prove model quality.
set -uo pipefail
cd /home/prod/repos/workforce-platform
export PATH="/home/prod/.local/toolchains/go/bin:$PATH"

BIN=/tmp/wf-e2e-bin/workforce
HARNESS=/home/prod/repos/workforce-platform/internal/runner/testdata/harness.py

# --- operator-configured harness runner (also proves config comes from env only) ---
export WORKFORCE_RUNNER_CLI_IDS=test-harness
export WORKFORCE_RUNNER_CLI_TEST_HARNESS_COMMAND="python3 ${HARNESS}"
export WORKFORCE_RUNNER_CLI_TEST_HARNESS_ENV=HARNESS_MODE
export WORKFORCE_RUNNER_CLI_TEST_HARNESS_TIMEOUT=30s
export HARNESS_MODE=auto

# Deliberate acknowledgment that this harness runs UNSANDBOXED as this account. The
# runner fails closed without it, so the risky mode is always a conscious choice.
export WORKFORCE_RUNNER_ALLOW_UNISOLATED=1

# --- credentials/config come from protected files, never from this script ---
set -a
. /home/prod/.config/workforce-platform/database.env
. /home/prod/.config/workforce-platform/local-auth.env
set +a
export HTTP_ADDR=127.0.0.1:8095
export APP_ENV=development
unset MIGRATION_DATABASE_URL   # the runtime must not hold migration credentials

APIPID=""; WPID=""
cleanup() {
  # SIGTERM so buffered logs flush, then SIGKILL only if it is still alive.
  for p in "$WPID" "$APIPID"; do
    [ -n "$p" ] || continue
    kill -TERM "$p" 2>/dev/null
  done
  sleep 1
  for p in "$WPID" "$APIPID"; do
    [ -n "$p" ] || continue
    if kill -0 "$p" 2>/dev/null; then kill -9 "$p" 2>/dev/null; fi
  done
}
trap cleanup EXIT

echo "=== build binary (one process per role) ==="
mkdir -p /tmp/wf-e2e-bin
go build -o "$BIN" ./cmd/workforce || { echo "build failed"; exit 1; }
echo "built $BIN"

echo "=== start API ==="
"$BIN" > /tmp/e2e_api.log 2>&1 &
APIPID=$!

echo "=== start worker (with the CLI harness configured) ==="
"$BIN" -worker > /tmp/e2e_worker.log 2>&1 &
WPID=$!

echo "=== wait for readiness (pid api=$APIPID worker=$WPID) ==="
for _ in $(seq 1 40); do
  curl -fsS http://127.0.0.1:8095/ready >/dev/null 2>&1 && break
  sleep 1
done
if ! curl -fsS http://127.0.0.1:8095/ready; then
  echo "API never became ready"; tail -20 /tmp/e2e_api.log; exit 1
fi
echo

echo "=== LIVE CHECK (includes the real-subprocess harness section) ==="
python3 scripts/live_check.py
LIVE=$?
echo "LIVE_EXIT=$LIVE"

echo
echo "=== worker log: harness diagnostics ==="
tail -12 /tmp/e2e_worker.log 2>/dev/null || echo "(empty)"
echo
echo "=== scratch dirs (stale ones are swept on the next run) ==="
ls -1 "${TMPDIR:-/tmp}/workforce-runs" 2>/dev/null || echo "(none)"

exit $LIVE
