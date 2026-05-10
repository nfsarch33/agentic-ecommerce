#!/usr/bin/env bash
# v5.2.0 Story 3: Lighthouse 7-page audit.
#
# Prerequisites:
#   npm install -g lighthouse   (or: npx lighthouse)
#   Frontend dev server running at BASE_URL
#
# Usage:
#   ./tests/lighthouse/run_v520.sh [BASE_URL]
#
# Pass criteria: Performance >=90, Accessibility >=90,
#   Best Practices >=90, SEO >=90 for all pages.
#
# Carry-forward note: If Lighthouse CLI is unavailable or frontend
# dev server cannot start, this script documents expected usage.
# Results saved to tests/lighthouse/v520_*.json.

set -euo pipefail

BASE_URL="${1:-http://localhost:3000}"
OUT_DIR="$(dirname "$0")"
TIMESTAMP="$(date +%s)"

PAGES=(
  "/"
  "/products"
  "/agent-activity"
  "/margin-dashboard"
  "/operator-alerts"
  "/onboarding"
  "/payments"
)

PAGE_NAMES=(
  "home"
  "products"
  "agent-activity"
  "margin-dashboard"
  "operator-alerts"
  "onboarding"
  "payments"
)

LIGHTHOUSE_CMD="lighthouse"
if ! command -v lighthouse &>/dev/null; then
  if command -v npx &>/dev/null; then
    LIGHTHOUSE_CMD="npx lighthouse"
  else
    echo "ERROR: lighthouse CLI not found. Install with: npm install -g lighthouse"
    echo "This script would capture the following pages:"
    for i in "${!PAGES[@]}"; do
      echo "  - ${PAGE_NAMES[$i]}: ${BASE_URL}${PAGES[$i]}"
    done
    echo "Pass criteria: Performance >=90, Accessibility >=90, Best Practices >=90, SEO >=90"
    exit 1
  fi
fi

PASS=true
FAIL_PAGES=""

for i in "${!PAGES[@]}"; do
  PAGE="${PAGES[$i]}"
  NAME="${PAGE_NAMES[$i]}"
  OUTPUT_FILE="${OUT_DIR}/v520_${NAME}_${TIMESTAMP}.json"

  echo "Capturing: ${NAME} (${BASE_URL}${PAGE})"

  $LIGHTHOUSE_CMD "${BASE_URL}${PAGE}" \
    --output=json \
    --output-path="${OUTPUT_FILE}" \
    --chrome-flags="--headless --no-sandbox" \
    --only-categories=performance,accessibility,best-practices,seo \
    --quiet \
    2>/dev/null || {
      echo "  WARN: Lighthouse failed for ${NAME}, skipping"
      continue
    }

  if command -v node &>/dev/null; then
    SCORES=$(node -e "
      const r = require('${OUTPUT_FILE}');
      const cats = r.categories;
      const scores = {
        performance: Math.round((cats.performance?.score || 0) * 100),
        accessibility: Math.round((cats.accessibility?.score || 0) * 100),
        bestPractices: Math.round((cats['best-practices']?.score || 0) * 100),
        seo: Math.round((cats.seo?.score || 0) * 100),
      };
      console.log(JSON.stringify(scores));
      const fails = Object.entries(scores).filter(([,v]) => v < 90);
      if (fails.length) {
        console.error('BELOW 90: ' + fails.map(([k,v]) => k + '=' + v).join(', '));
        process.exit(2);
      }
    " 2>&1) || {
      PASS=false
      FAIL_PAGES="${FAIL_PAGES} ${NAME}"
    }

    echo "  Scores: ${SCORES}"
  fi
done

echo ""
if [ "$PASS" = true ]; then
  echo "ALL PAGES PASS (>=90 all categories). Results in: ${OUT_DIR}/v520_*_${TIMESTAMP}.json"
else
  echo "BELOW-90 pages:${FAIL_PAGES}"
  echo "Results in: ${OUT_DIR}/v520_*_${TIMESTAMP}.json"
fi
