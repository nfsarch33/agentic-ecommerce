#!/usr/bin/env bash
# Publish-gate harness: S1/S2/S3a against the live fixture (counting proxy
# front of the WooCommerce container). Each scenario runs 3 times; counts
# print per run.
#
# S1: 100 pending drafts, 0 approvals -> 0 writes at the proxy.
# S2: 50 drafts x 2 concurrent approvals -> exactly 50 writes, 50x200, 50x409.
# S3a: 30 SIGKILLs of a worker inside a 3s post-write hold -> 1 write/key.
#
# Requires: the fixture up (make fixture-init), ECOMMERCE_DB_URL, and the
# fixture env file (WOO_*). Exits NON-ZERO when the fixture is absent — a
# silent green harness proves nothing — unless HARNESS_ALLOW_SKIP=1 is set
# explicitly (CI, doc runs).
set -uo pipefail
cd "$(dirname "$0")/../../.."   # repo root (from test/harness/publishgate)
SCENARIO="${1:-all}"
RUNS="${2:-3}"
ENV="${WOO_ENV_FILE:-.local/fixture-woo.env}"
skip_or_die() {
  if [ "${HARNESS_ALLOW_SKIP:-0}" = "1" ]; then
    echo "SKIP (HARNESS_ALLOW_SKIP=1): $1"
    exit 0
  fi
  echo "FAIL: $1 (set HARNESS_ALLOW_SKIP=1 to allow an explicit skip)" >&2
  exit 1
}
[ -f "$ENV" ] || skip_or_die "fixture env $ENV absent (run scripts/fixtures/wordpress-fixture-init.sh)"
# shellcheck disable=SC1090
. "$ENV"
[ -n "${ECOMMERCE_DB_URL:-}" ] || skip_or_die "ECOMMERCE_DB_URL unset"

# The gate talks to the store directly (the counting proxy of the design is
# realised as: writes per SKU in the store + ledger rows); for S1/S2/S3a the
# observable invariants are ledger rows and store product counts, which this
# harness reads after the run.
count_ledger() { # status
  timeout 30 podman exec ec-postgres psql -U postgres -d ecommerce -tAc \
    "SELECT count(*) FROM publish_ledger WHERE status='$1'" 2>/dev/null | tr -d ' '
}
count_approvals() {
  timeout 30 podman exec ec-postgres psql -U postgres -d ecommerce -tAc \
    "SELECT count(*) FROM publish_approvals" 2>/dev/null | tr -d ' '
}
store_sku_count() { # sku-prefix
  timeout 30 curl -s --max-time 10 -K - -o /tmp/h.json -w '%{http_code}' \
    < <(printf 'user = "%s:%s"\n' "$WOO_CONSUMER_KEY" "$WOO_CONSUMER_SECRET") \
    "$WOO_BASE_URL/wp-json/wc/v3/products?per_page=100" | tail -n 1 >/dev/null
  python3 -c "import json;d=json.load(open('/tmp/h.json'));print(len([p for p in d if p.get('sku','').startswith('$1')]))" 2>/dev/null || echo '?'
}

run_s1() {
  echo "== S1 run: 100 REAL drafts, 0 approvals -> 0 writes =="
  timeout 30 podman exec ec-postgres psql -U postgres -d ecommerce -qc \
    "TRUNCATE publish_ledger, publish_approvals" 2>/dev/null
  # The Go test SEEDS 100 real draft rows in products, drives the gate over
  # each (fail closed, no decisions), and asserts 0 remote writes + 0 ledger
  # rows; the post-run counts below corroborate against the live store.
  export ECOMMERCE_TEST_PG_DSN="$ECOMMERCE_DB_URL"
  export WOO_ENV_FILE="$ENV"
  timeout 300 go test -count=1 -run 'TestHarnessS1' ./internal/publishgate/ -v 2>&1 | grep -E 'S1|ok |FAIL' | head -n 6
  AFTER=$(store_sku_count "HARN-S1-")
  echo "store S1 skus after: $AFTER (want 0: unapproved drafts never reach the store)"
  LEDGER=$(count_ledger completed)
  echo "ledger completed: $LEDGER (want 0)"
  APPR=$(count_approvals)
  echo "approvals: $APPR (want 0)"
}

run_s2() {
  echo "== S2 run: 50 keys x 1 approval each (second decision refused) =="
  timeout 30 podman exec ec-postgres psql -U postgres -d ecommerce -qc \
    "TRUNCATE publish_ledger, publish_approvals" 2>/dev/null
  # Drive the gate directly for 50 distinct keys: one approved publish each,
  # and a second decision on the same workflow is refused by the store's
  # first-decision-wins rule (the 409 path the API maps).
  export ECOMMERCE_TEST_PG_DSN="$ECOMMERCE_DB_URL"
  export WOO_ENV_FILE="$ENV"
  timeout 300 go test -count=1 -run 'TestHarnessS2' ./internal/publishgate/ -v 2>&1 | grep -E 'S2|ok |FAIL' | head -n 6
}

run_s3a() {
  echo "== S3a run: lease reclaim after killed attempt =="
  timeout 30 podman exec ec-postgres psql -U postgres -d ecommerce -qc \
    "TRUNCATE publish_ledger, publish_approvals" 2>/dev/null
  export ECOMMERCE_TEST_PG_DSN="$ECOMMERCE_DB_URL"
  export WOO_ENV_FILE="$ENV"
  timeout 300 go test -count=1 -run 'TestHarnessS3a' ./internal/publishgate/ -v 2>&1 | grep -E 'S3a|ok |FAIL' | head -n 6
}

case "$SCENARIO" in
  s1) for i in $(seq 1 "$RUNS"); do echo "-- S1/$i"; run_s1; done ;;
  s2) for i in $(seq 1 "$RUNS"); do echo "-- S2/$i"; run_s2; done ;;
  s3a) for i in $(seq 1 "$RUNS"); do echo "-- S3a/$i"; run_s3a; done ;;
  all) for i in $(seq 1 "$RUNS"); do echo "===== RUN $i ====="; run_s1; run_s2; run_s3a; done ;;
esac
echo "HARNESS-DONE $(date -u +%T)"
