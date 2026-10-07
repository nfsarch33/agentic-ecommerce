#!/usr/bin/env bash
# pilot-isolation-tests.sh — the v18870-4 acceptance: pilot A's
# credentials cannot read pilot B's data.
#
# Two layers: shape rows (committed files say what they must) and, when
# podman is available and HLXN_PILOT_LIVE=1, the LIVE two-instance proof
# from scripts/pilot-instance.sh isolation-test — its output belongs on
# the ticket as the acceptance evidence.
#
# MUTANT: delete the per-pilot COMPOSE_PROJECT_NAME from example.env and
# the project-scoping row goes red (both pilots would share one project);
# delete the overlay's mem_limit block and the caps row goes red; point
# POSTGRES_HOST_PORT at one shared value and the distinct-ports row goes
# red (the isolation test itself then fails arm 1 or hangs on the port).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || { echo "FAIL: cannot cd"; exit 1; }
PASS=0; FAIL=0; FAILED=()
check() { local n="$1" c="$2"; if eval "$c" >/dev/null 2>&1; then PASS=$((PASS+1)); echo "  [PASS] $n"; else FAIL=$((FAIL+1)); FAILED+=("$n"); fi; }

# Shape rows.
check "example.env stamps a per-pilot COMPOSE_PROJECT_NAME (own project = own volumes/networks)" \
  'grep -q "^COMPOSE_PROJECT_NAME=pilot-NAME$" pilots/example.env'
check "example.env carries NO default password (the template word stays, and create refuses it)" \
  'grep -q "generate-one-per-pilot" pilots/example.env && grep -q "generate-one-per-pilot" scripts/pilot-instance.sh'
check "every pilot gets its own host port pair (loopback-bound)" \
  'grep -q "^BIND_HOST=127.0.0.1$" pilots/example.env && grep -q "^POSTGRES_HOST_PORT=" pilots/example.env'
check "pilots/*.env and backups/ are gitignored; the template is not" \
  'grep -q "^pilots/\*.env$" .gitignore && grep -q "^!pilots/example.env$" .gitignore && grep -q "^backups/$" .gitignore'
check "the overlay caps the data-bearing services (postgres, redis, mc-api)" \
  'for s in postgres redis mc-api; do grep -A2 "^  $s:" docker-compose.pilot.yml | grep -q mem_limit || exit 1; done'
check "the runner is podman-only (the raw container word appears on no executable line — only in the prohibition comment)" \
  '! grep -vE "^[[:space:]]*#" scripts/pilot-instance.sh | grep -qiE "(^|[^a-z-])doc?ker?([[:space:]]| compose |-compose |.*prohibited)" '
check "isolation-test has both arms and an always-teardown trap" \
  'grep -q "ARM 1 FAILED" scripts/pilot-instance.sh && grep -q "ARM 2 FAILED" scripts/pilot-instance.sh && grep -q "trap cleanup EXIT" scripts/pilot-instance.sh'
check "backup writes per-pilot gz dumps under backups/" \
  'grep -q "pg_dump" scripts/pilot-instance.sh && grep -qE "backups_dir/\\\$name-" scripts/pilot-instance.sh'

# Live rows (the acceptance): only when podman is usable and the operator
# asked for them — CI runners have no podman socket.
if [ "${HLXN_PILOT_LIVE:-0}" = 1 ] && command -v podman >/dev/null 2>&1; then
  out=$(bash scripts/pilot-instance.sh isolation-test 2>&1); rc=$?
  echo "$out" | sed 's/^/    /'
  check "LIVE ACCEPTANCE: pilot A credentials rejected by pilot B; networks isolated" \
    '[ "$rc" = 0 ] && printf "%s" "$out" | grep -q "ISOLATION TEST PASSED"'
else
  echo "  [SKIP] live isolation test (set HLXN_PILOT_LIVE=1 with a podman socket to run it)"
fi

echo
if [ "$FAIL" -gt 0 ]; then echo "PASS: $PASS, FAIL: $FAIL"; printf '  [FAIL] %s\n' "${FAILED[@]}"; exit 1; fi
echo "PASS: $PASS, FAIL: 0"
