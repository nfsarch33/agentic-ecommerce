// v6.3.0 Pair 3 MVP — Story 5 k6 smoke baseline.
//
// Lightweight k6 scenario hitting the unauthenticated `/healthz`
// endpoint of a locally-running mc-api binary. Establishes the
// floor latency baseline (request framing, http server, atomic
// counter increment) without requiring the full Docker Compose
// stack (postgres + redis + temporal + minio + mc-api).
//
// The full v490_comprehensive.js sweep (7 scenarios x ~50-100 RPS)
// remains the authoritative source of truth for endpoint-level p95
// numbers and is executed in v6.3.1 QA against the full stack.
//
// Manual execution:
//   k6 run tests/load/k6/v630_smoke.js \
//     -e BASE_URL=http://127.0.0.1:8080 \
//     --summary-export tests/load/results/v630_smoke_summary.json
//
// Pass criteria (per plan): p95 < 500ms under 100 RPS sustained.

import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://127.0.0.1:8080';

const errorRate = new Rate('errors');
const healthDuration = new Trend('healthz_duration', true);

export const options = {
  scenarios: {
    healthz_smoke: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '60s',
      preAllocatedVUs: 60,
      maxVUs: 120,
      exec: 'healthz',
    },
  },
  thresholds: {
    'healthz_duration': ['p(95)<500'],
    'errors': ['rate<0.01'],
  },
};

export function healthz() {
  const res = http.get(`${BASE_URL}/healthz`);
  healthDuration.add(res.timings.duration);
  const ok = check(res, { 'healthz 200': (r) => r.status === 200 });
  errorRate.add(!ok);
}

export function handleSummary(data) {
  return {
    stdout: JSON.stringify(data, null, 2),
  };
}
