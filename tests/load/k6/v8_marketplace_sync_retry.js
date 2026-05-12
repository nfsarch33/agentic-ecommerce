// v8 Pair 1 QA -- marketplace sync retry/load matrix.
//
// This profile exercises the lightweight health and metrics surfaces while
// tagging requests with marketplace-sync retry dimensions. The internal sync
// core is intentionally not exposed as an HTTP endpoint in Pair 1; provider
// adapter pairs can replace the stubbed operations with live sandbox routes.
//
// Manual execution:
//   EC_K6_BASE_URL=http://127.0.0.1:8080 k6 run tests/load/k6/v8_marketplace_sync_retry.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.EC_K6_BASE_URL || 'http://127.0.0.1:8080';
const DURATION = __ENV.EC_K6_SCENARIO_DURATION || '30s';
const RATE = Number(__ENV.EC_K6_MARKETPLACE_SYNC_RATE || '20');

export const marketplaceSyncFailures = new Rate('marketplace_sync_failures');
export const marketplaceSyncDuration = new Trend('marketplace_sync_duration', true);

export const options = {
  scenarios: {
    marketplace_sync_retry_matrix: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 8,
      maxVUs: 32,
      exec: 'marketplaceSyncRetryMatrix',
      tags: { surface: 'marketplace_sync_retry_matrix' },
    },
  },
  thresholds: {
    marketplace_sync_failures: ['rate<0.02'],
    marketplace_sync_duration: ['p(95)<250'],
    'http_req_failed{surface:marketplace_sync_retry_matrix}': ['rate<0.02'],
    'http_req_duration{surface:marketplace_sync_retry_matrix}': ['p(95)<250'],
  },
};

export function marketplaceSyncRetryMatrix() {
  const provider = ['shopify', 'shopee'][__ITER % 2];
  const entityType = 'product';
  const retryClass = ['first_attempt', 'retry', 'replay', 'dedupe'][__ITER % 4];
  const params = {
    tags: {
      surface: 'marketplace_sync_retry_matrix',
      provider,
      entity_type: entityType,
      retry_class: retryClass,
    },
  };

  const health = http.get(`${BASE_URL}/healthz`, params);
  marketplaceSyncDuration.add(health.timings.duration, params.tags);
  const ok = check(health, {
    'healthz 2xx': (r) => r.status >= 200 && r.status < 300,
  });
  marketplaceSyncFailures.add(!ok, params.tags);

  if (__ITER % 10 === 0) {
    const metrics = http.get(`${BASE_URL}/metrics`, params);
    check(metrics, {
      'metrics scrape available': (r) => r.status === 200 || r.status === 404,
    });
  }

  sleep(0.1);
}
