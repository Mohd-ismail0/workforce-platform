#!/usr/bin/env bash
# MANDATORY real-model pause/resume acceptance.
#
# Why this exists: in a shared organisation the run snapshot exposes historical
# supplier/message details, so a real model can answer immediately and never exercise
# the pause path. An acceptance test that treats that as success qualifies nothing.
# This script therefore runs in a FRESH organisation holding exactly ONE record that
# does not contain the missing fact, and FAILS if the model does not return `waiting`.
#
# A holdout branch also proves that independent work keeps moving while one run waits.
#
# Safety / honesty properties:
#  - fresh org + unique ids: no interference with the deterministic double's records
#  - owned PIDs only (never pattern-kill: a `pkill -f` pattern matches the caller and
#    has killed this shell twice)
#  - bounded spend (--max-budget-usd, --max-turns) and no tools (--bare + disallowed)
#  - credential supplied over HTTPS via the runner's environment allowlist only
#  - the scripted answer is a SIMULATED human input, labelled as such in the output
set -uo pipefail
cd /home/prod/repos/workforce-platform
export PATH="/home/prod/.local/toolchains/go/bin:$PATH"

BIN=/tmp/wf-qual3/workforce
CLAUDE=/home/prod/.local/harness/node_modules/.bin/claude
API_LOG=/tmp/q3_api.log ; W_LOG=/tmp/q3_worker.log
RUNNER=claude-real

[ -x "$CLAUDE" ] || { echo "harness missing"; exit 1; }

# ---- credential + database, from protected files only ----
set -a
. /home/prod/.config/workforce-platform/database.env
. /home/prod/.config/workforce-platform/local-auth.env
. /home/prod/.config/workforce-platform/harness.env
set +a
unset MIGRATION_DATABASE_URL   # the runtime must not hold migration credentials

# HTTPS for any credential-bearing request. The previous run used http://.
if [ -n "${ANTHROPIC_BASE_URL:-}" ]; then
  export ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL/http:\/\//https://}"
  echo "harness base scheme: ${ANTHROPIC_BASE_URL%%://*}"
fi

mkdir -p /tmp/wf-qual3
echo "=== fresh organisation: exactly one record, missing the required fact ==="
FRESH=$(uv run --with 'psycopg[binary]' python3 - "$DATABASE_URL" <<'PY'
import secrets, sys, psycopg
url = sys.argv[1]
org  = "qual" + secrets.token_hex(12)
req  = "qreq" + secrets.token_hex(12)
app  = "qapp" + secrets.token_hex(12)
adm  = "qadm" + secrets.token_hex(12)
rec  = "qmail" + secrets.token_hex(12)
tok  = secrets.token_urlsafe(32)
with psycopg.connect(url, connect_timeout=10) as c:
    with c.transaction():
        # app.org_id must be set for the RLS WITH CHECK on tenant tables.
        c.execute("select set_config('app.org_id', %s, true)", (org,))
        c.execute("INSERT INTO organizations(id,name) VALUES(%s,'real qualification org')", (org,))
        c.execute("INSERT INTO principals(id,org_id,name,role) VALUES"
                  "(%s,%s,'Qual Requester','requester'),"
                  "(%s,%s,'Qual Approver','approver'),"
                  "(%s,%s,'Qual Admin','admin')", (req,org,app,org,adm,org))
        # ONE mail record, deliberately WITHOUT any recipient detail: the destination
        # for this enquiry is genuinely unknown, so the model must ask for it.
        c.execute("INSERT INTO simulator_records(id,org_id,integration,version,data)"
                  " VALUES(%s,%s,'mail',1,'{}'::jsonb)", (rec, org))
        c.execute("select set_config('app.org_id','',true)")
print(f"{org}|{req}|{app}|{adm}|{rec}|{tok}")
PY
)
IFS='|' read -r ORG REQ APP ADM REC TOK <<< "$FRESH"
[ -n "${ORG:-}" ] || { echo "fresh org creation failed"; exit 1; }
echo "  org=${ORG:0:10}… record=${REC:0:10}… (token withheld)"

# The fresh principal is authorized by extending the runtime token map in-memory only;
# no credential file is modified.
export LOCAL_AUTH_TOKENS="${LOCAL_AUTH_TOKENS},${TOK}=${ORG}:${REQ}:requester,${TOK}b=${ORG}:${APP}:approver,${TOK}c=${ORG}:${ADM}:admin"
export HTTP_ADDR=127.0.0.1:8095 APP_ENV=development WORKFORCE_RUNNER_ALLOW_UNISOLATED=1
export WORKFORCE_RUNNER_CLI_IDS="$RUNNER"
export WORKFORCE_RUNNER_CLI_CLAUDE_REAL_COMMAND="$CLAUDE --bare -p \"Follow the instructions in the JSON document supplied on stdin. Output only the single JSON object, nothing else.\" --output-format json --model gpt-5-mini --max-turns 2 --max-budget-usd 0.25 --disallowedTools \"Bash,Read,Write,Edit,Glob,Grep,WebFetch,WebSearch,Task,NotebookEdit\""
export WORKFORCE_RUNNER_CLI_CLAUDE_REAL_ENV="ANTHROPIC_BASE_URL,ANTHROPIC_API_KEY,ANTHROPIC_AUTH_TOKEN"
export WORKFORCE_RUNNER_CLI_CLAUDE_REAL_TIMEOUT=300s
export WORKFORCE_RUNNER_WORK_ROOT=/tmp/wf-qual3-runs
rm -rf /tmp/wf-qual3-runs 2>/dev/null

echo
echo "=== build ==="
go build -o "$BIN" ./cmd/workforce || { echo BUILD_FAILED; exit 1; }
echo "  ok"

APIPID=""; WPID=""
cleanup() {
  for p in "$WPID" "$APIPID"; do [ -n "$p" ] && kill -TERM "$p" 2>/dev/null; done
  sleep 2
  for p in "$WPID" "$APIPID"; do [ -n "$p" ] && kill -0 "$p" 2>/dev/null && kill -9 "$p" 2>/dev/null; done
}
trap cleanup EXIT

echo
echo "=== start api + worker (owned PIDs) ==="
"$BIN" > "$API_LOG" 2>&1 & APIPID=$!
"$BIN" -worker > "$W_LOG" 2>&1 & WPID=$!
echo "  api=$APIPID worker=$WPID"
for _ in $(seq 1 40); do curl -fsS http://127.0.0.1:8095/ready >/dev/null 2>&1 && break; sleep 1; done
curl -fsS http://127.0.0.1:8095/ready || { echo "API not ready"; tail -10 "$API_LOG"; exit 1; }

python3 /home/prod/repos/workforce-platform/scripts/qualify_pause_accept.py "$ORG" "$REQ" "$APP" "$ADM" "$REC" "$TOK" "$RUNNER"
QUAL=$?

echo
echo "=== worker log (harness + recovery activity) ==="
grep -iE "harness|agent.run|reclaim" "$W_LOG" | tail -8 || echo "  (none)"
echo
echo "=== scratch dirs after the run (must be empty) ==="
ls -1 /tmp/wf-qual3-runs 2>/dev/null | head -5 || echo "  (none)"
exit $QUAL
