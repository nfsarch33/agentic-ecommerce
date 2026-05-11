// v4.9.0 Story 4: k6 comprehensive load matrix.
// Covers mounted API surfaces added since v4.0.0.
//
// Manual execution (k6 required: brew install k6):
//   k6 run tests/load/k6/v490_comprehensive.js \
//     --out json=tests/load/results/v490_$(date +%s).json \
//     -e BASE_URL=http://localhost:8080 \
//     -e EC_K6_SCENARIO_DURATION=5m
//
// Pass criteria: all endpoints p95 within documented budgets and <1% errors.
// Default rates target 100 HTTP requests/s; increase EC_K6_RATE_SCALE for stress.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const TENANT_ID = __ENV.TENANT_ID || 'load-test-tenant';
const SCENARIO_DURATION = __ENV.EC_K6_SCENARIO_DURATION || '2m';
const RATE_SCALE = Number(__ENV.EC_K6_RATE_SCALE || '1');
const GMV_FROM = __ENV.EC_K6_GMV_FROM || '2026-05-01';
const GMV_TO = __ENV.EC_K6_GMV_TO || '2026-05-31';

function scaledRate(base) {
  return Math.max(1, Math.floor(base * RATE_SCALE));
}

const errorRate = new Rate('errors');
const paymentDuration = new Trend('payment_list_duration', true);
const webhookDuration = new Trend('webhook_registry_duration', true);
const adminDuration = new Trend('admin_mobile_duration', true);
const channelDuration = new Trend('admin_channel_duration', true);
const marketplaceDuration = new Trend('marketplace_plugins_duration', true);
const dashboardDuration = new Trend('tenant_dashboard_duration', true);
const gmvDuration = new Trend('gmv_api_duration', true);

export const options = {
  scenarios: {
    payment_list: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(10),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 20,
      maxVUs: 40,
      exec: 'paymentList',
    },
    webhook_registry: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(15),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 30,
      maxVUs: 60,
      exec: 'webhookRegistry',
    },
    admin_mobile: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(15),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 30,
      maxVUs: 60,
      exec: 'adminMobile',
    },
    admin_channels: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(5),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 10,
      maxVUs: 20,
      exec: 'adminChannels',
    },
    marketplace_plugins: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(10),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 20,
      maxVUs: 40,
      exec: 'marketplacePlugins',
    },
    tenant_dashboard: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(10),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 20,
      maxVUs: 40,
      exec: 'tenantDashboard',
    },
    gmv_api: {
      executor: 'constant-arrival-rate',
      rate: scaledRate(20),
      timeUnit: '1s',
      duration: SCENARIO_DURATION,
      preAllocatedVUs: 40,
      maxVUs: 80,
      exec: 'gmvApi',
    },
  },
  thresholds: {
    'payment_list_duration': ['p(95)<200'],
    'webhook_registry_duration': ['p(95)<100'],
    'admin_mobile_duration': ['p(95)<150'],
    'admin_channel_duration': ['p(95)<100'],
    'marketplace_plugins_duration': ['p(95)<200'],
    'tenant_dashboard_duration': ['p(95)<150'],
    'gmv_api_duration': ['p(95)<35'],
    'errors': ['rate<0.01'],
  },
};

const headers = {
  'Content-Type': 'application/json',
  'X-Tenant-Id': TENANT_ID,
};

const providers = ['stripe', 'alipay', 'paypal'];

export function paymentList() {
  const provider = providers[Math.floor(Math.random() * providers.length)];
  const res = http.get(`${BASE_URL}/api/v1/payments?tenant_id=${TENANT_ID}&provider=${provider}&limit=20`, { headers });
  paymentDuration.add(res.timings.duration);
  const ok = check(res, { 'payment list 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function webhookRegistry() {
  const res = http.get(`${BASE_URL}/api/v1/webhooks`, { headers });
  webhookDuration.add(res.timings.duration);
  const ok = check(res, { 'webhook registry 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function adminMobile() {
  const res = http.get(`${BASE_URL}/api/v1/admin/summary`, { headers });
  adminDuration.add(res.timings.duration);
  const ok = check(res, { 'admin summary 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);

  const ordersRes = http.get(`${BASE_URL}/api/v1/admin/orders?page=1&limit=20`, { headers });
  adminDuration.add(ordersRes.timings.duration);
  const ordersOk = check(ordersRes, { 'admin orders 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ordersOk);
}

export function adminChannels() {
  const res = http.get(`${BASE_URL}/api/v1/admin/channels`, { headers });
  channelDuration.add(res.timings.duration);
  const ok = check(res, { 'admin channels 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function marketplacePlugins() {
  const res = http.get(`${BASE_URL}/api/v1/marketplace/plugins?per_page=20`, { headers });
  marketplaceDuration.add(res.timings.duration);
  const ok = check(res, { 'marketplace plugins 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function tenantDashboard() {
  const res = http.get(`${BASE_URL}/api/v1/tenants/${TENANT_ID}/dashboard`, { headers });
  dashboardDuration.add(res.timings.duration);
  const ok = check(res, { 'dashboard 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function gmvApi() {
  const res = http.get(`${BASE_URL}/api/v1/analytics/gmv?tenant_id=${TENANT_ID}&from=${GMV_FROM}&to=${GMV_TO}`, { headers });
  gmvDuration.add(res.timings.duration);
  const ok = check(res, { 'gmv 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function handleSummary(data) {
  return {
    stdout: JSON.stringify(data, null, 2),
  };
}
