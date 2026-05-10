// v4.9.0 Story 4: k6 comprehensive load matrix.
// Covers all major API surfaces added since v4.0.0.
//
// Manual execution (k6 required: brew install k6):
//   k6 run tests/load/k6/v490_comprehensive.js \
//     --out json=tests/load/results/v490_$(date +%s).json \
//     -e BASE_URL=http://localhost:8080
//
// Pass criteria: all endpoints p95 within documented budgets.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const TENANT_ID = __ENV.TENANT_ID || 'load-test-tenant';

const errorRate = new Rate('errors');
const paymentDuration = new Trend('payment_charge_duration', true);
const webhookDuration = new Trend('webhook_normaliser_duration', true);
const adminDuration = new Trend('admin_mobile_duration', true);
const coachingDuration = new Trend('coaching_tip_duration', true);
const commissionDuration = new Trend('commission_report_duration', true);
const dashboardDuration = new Trend('tenant_dashboard_duration', true);
const gmvDuration = new Trend('gmv_api_duration', true);

export const options = {
  scenarios: {
    payment_charge: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 60,
      maxVUs: 120,
      exec: 'paymentCharge',
    },
    webhook_normaliser: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 120,
      maxVUs: 200,
      exec: 'webhookNormaliser',
    },
    admin_mobile: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 120,
      maxVUs: 200,
      exec: 'adminMobile',
    },
    coaching_tip: {
      executor: 'constant-arrival-rate',
      rate: 20,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 30,
      maxVUs: 50,
      exec: 'coachingTip',
    },
    commission_report: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 60,
      maxVUs: 120,
      exec: 'commissionReport',
    },
    tenant_dashboard: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 60,
      maxVUs: 120,
      exec: 'tenantDashboard',
    },
    gmv_api: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 120,
      maxVUs: 200,
      exec: 'gmvApi',
    },
  },
  thresholds: {
    'payment_charge_duration': ['p(95)<200'],
    'webhook_normaliser_duration': ['p(95)<100'],
    'admin_mobile_duration': ['p(95)<150'],
    'coaching_tip_duration': ['p(95)<100'],
    'commission_report_duration': ['p(95)<200'],
    'tenant_dashboard_duration': ['p(95)<150'],
    'gmv_api_duration': ['p(95)<35'],
    'errors': ['rate<0.01'],
  },
};

const headers = {
  'Content-Type': 'application/json',
  'X-Tenant-Id': TENANT_ID,
};

const providers = ['stripe', 'alipay', 'wechat', 'paypal'];

export function paymentCharge() {
  const provider = providers[Math.floor(Math.random() * providers.length)];
  const payload = JSON.stringify({
    provider: provider,
    amount_cents: Math.floor(Math.random() * 100000) + 100,
    currency: 'AUD',
    customer_id: `cust-${Math.floor(Math.random() * 1000)}`,
  });
  const res = http.post(`${BASE_URL}/api/v1/payments/charge`, payload, { headers });
  paymentDuration.add(res.timings.duration);
  const ok = check(res, { 'payment 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function webhookNormaliser() {
  const payload = JSON.stringify({
    event_type: 'payment.completed',
    provider: 'stripe',
    payload: { charge_id: `ch_${Date.now()}`, amount: 5000 },
  });
  const res = http.post(`${BASE_URL}/api/v1/payments/webhook`, payload, { headers });
  webhookDuration.add(res.timings.duration);
  const ok = check(res, { 'webhook 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function adminMobile() {
  const res = http.get(`${BASE_URL}/api/v1/admin/mobile/summary`, { headers });
  adminDuration.add(res.timings.duration);
  const ok = check(res, { 'admin summary 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);

  const ordersRes = http.get(`${BASE_URL}/api/v1/admin/mobile/orders?limit=20`, { headers });
  adminDuration.add(ordersRes.timings.duration);
  const ordersOk = check(ordersRes, { 'admin orders 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ordersOk);
}

export function coachingTip() {
  const res = http.get(`${BASE_URL}/api/v1/coaching/tip`, { headers });
  coachingDuration.add(res.timings.duration);
  const ok = check(res, { 'coaching 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function commissionReport() {
  const res = http.get(`${BASE_URL}/api/v1/marketplace/commissions/report`, { headers });
  commissionDuration.add(res.timings.duration);
  const ok = check(res, { 'commission 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function tenantDashboard() {
  const res = http.get(`${BASE_URL}/api/v1/tenants/${TENANT_ID}/dashboard`, { headers });
  dashboardDuration.add(res.timings.duration);
  const ok = check(res, { 'dashboard 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function gmvApi() {
  const res = http.get(`${BASE_URL}/api/v1/analytics/gmv/daily?range=30d`, { headers });
  gmvDuration.add(res.timings.duration);
  const ok = check(res, { 'gmv 2xx': (r) => r.status >= 200 && r.status < 300 });
  errorRate.add(!ok);
}

export function handleSummary(data) {
  return {
    stdout: JSON.stringify(data, null, 2),
  };
}
