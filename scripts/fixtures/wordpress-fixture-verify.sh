#!/usr/bin/env bash
# Acceptance probe for the fixture: with the env file produced by
# wordpress-fixture-init.sh, the product list answers 200 with the valid
# secret and 401 with a wrong one. Exit 0 only on that exact pair.
set -euo pipefail
ENV_IN="${1:-.local/fixture-woo.env}"
# shellcheck disable=SC1090
. "$ENV_IN"
BASE="${WOO_BASE_URL:?}"
# The secret never touches argv: curl credentials come from a stdin config.
# The secret never touches argv: curl credentials come from a stdin config.
# The -o/-w flags live INSIDE the invocation (after -K -) so curl reads
# them as options, not extra URLs.
VALID_CODE=$(printf 'user = "%s:%s"\n' "$WOO_CONSUMER_KEY" "$WOO_CONSUMER_SECRET" | \
  curl -s -K - --max-time 10 -o /tmp/fv-ok.json -w '%{http_code}' "$BASE/wp-json/wc/v3/products")
WRONG_SECRET="cs_${WOO_CONSUMER_SECRET#cs_}x-wrong"  # same shape, wrong value
WRONG_CODE=$(printf 'user = "%s:%s"\n' "$WOO_CONSUMER_KEY" "$WRONG_SECRET" | \
  curl -s -K - --max-time 10 -o /dev/null -w '%{http_code}' "$BASE/wp-json/wc/v3/products")
echo "valid=$VALID_CODE wrong=$WRONG_CODE"
N=0
[ "$VALID_CODE" = "200" ] && N=$(python3 -c "import json;d=json.load(open('/tmp/fv-ok.json'));print(len(d) if isinstance(d,list) else -1)" 2>/dev/null || echo 0)
[ "$VALID_CODE" = "200" ] && [ "$WRONG_CODE" = "401" ] && [ "${N:-0}" -ge 1 ] && { echo "FIXTURE-VERIFY-OK ($N products)"; exit 0; }
echo "FIXTURE-VERIFY-FAILED"; exit 1
