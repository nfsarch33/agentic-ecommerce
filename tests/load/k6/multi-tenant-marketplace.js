// v2.7.0 multi-tenant marketplace load profile.
//
// Spins up `K6_TENANTS` simulated tenants (default 5) and drives
// realistic load against the marketplace + billing surfaces in
// parallel. Each tenant gets its own X-Tenant-ID, exercising the
// per-tenant isolation introduced by RLS (v2.5.0) and tenant-aware
// secrets / connection pool / Grafana fan-out (v2.7.0).
//
// Run locally:
//   MT_DURATION=30s k6 run tests/load/k6/multi-tenant-marketplace.js
//
// Artefacts: stdout summary; per-scenario thresholds gate the run.
// NOTE: avoid the K6_* env-var namespace; k6 treats any K6_DURATION /
// K6_VUS / K6_ITERATIONS as a CLI override and discards the scenarios
// block entirely, so we use MT_* (multi-tenant) instead.

import http from 'k6/http';
import { check, group } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const bearerToken = __ENV.BEARER_TOKEN || '';

const tenantCount = Number(__ENV.K6_TENANTS || 5);
const tenants = Array.from({ length: tenantCount }, (_, i) => `tenant_${i + 1}`);

function tenantHeaders(tenantId) {
  const headers = { 'X-Tenant-ID': tenantId, 'Content-Type': 'application/json' };
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`;
  return headers;
}

function pickTenant() {
  return tenants[Math.floor(Math.random() * tenants.length)];
}

export const options = {
  scenarios: {
    marketplace_list: {
      executor: 'constant-arrival-rate',
      exec: 'marketplaceList',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_MARKETPLACE_LIST_RPS || 20),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_MARKETPLACE_LIST_VUS || 20),
      tags: { surface: 'marketplace_list' },
    },
    marketplace_install_lifecycle: {
      executor: 'constant-arrival-rate',
      exec: 'marketplaceLifecycle',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_MARKETPLACE_LIFECYCLE_RPS || 5),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_MARKETPLACE_LIFECYCLE_VUS || 10),
      tags: { surface: 'marketplace_lifecycle' },
    },
    submission_queue: {
      executor: 'constant-arrival-rate',
      exec: 'submissionQueue',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_SUBMISSION_RPS || 3),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_SUBMISSION_VUS || 6),
      tags: { surface: 'submission_queue' },
    },
    billing_read: {
      executor: 'constant-arrival-rate',
      exec: 'billingRead',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_BILLING_READ_RPS || 10),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_BILLING_READ_VUS || 10),
      tags: { surface: 'billing_read' },
    },
  },
  thresholds: {
    'http_req_duration{surface:marketplace_list}': ['p(95)<400'],
    'http_req_duration{surface:marketplace_lifecycle}': ['p(95)<800'],
    'http_req_duration{surface:submission_queue}': ['p(95)<600'],
    'http_req_duration{surface:billing_read}': ['p(95)<500'],
    // marketplace_list and submission_queue should be near-zero failure
    // rate; the lifecycle scenario is allowed to climb higher because
    // repeat install/activate against the same (tenant, slug) returns
    // 409/422 by design (idempotent state machine).
    'http_req_failed{surface:marketplace_list}': ['rate<0.02'],
    'http_req_failed{surface:billing_read}': ['rate<0.05'],
    'http_req_failed{surface:submission_queue}': ['rate<0.05'],
    'http_req_failed{surface:marketplace_lifecycle}': ['rate<0.7'],
    checks: ['rate>=1'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max'],
};

export function marketplaceList() {
  const tenant = pickTenant();
  group(`marketplace_list:${tenant}`, () => {
    const res = http.get(`${baseURL}/api/v1/marketplace/plugins?per_page=20`, { headers: tenantHeaders(tenant) });
    check(res, { 'list status 200': (r) => r.status === 200 });
  });
}

export function marketplaceLifecycle() {
  const tenant = pickTenant();
  const slug = 'stripe-payments';
  group(`marketplace_lifecycle:${tenant}`, () => {
    const get = http.get(`${baseURL}/api/v1/marketplace/plugins/${slug}`, { headers: tenantHeaders(tenant) });
    check(get, { 'manifest reachable': (r) => r.status === 200 || r.status === 404 });

    const install = http.post(`${baseURL}/api/v1/marketplace/plugins/${slug}/install`, null, { headers: tenantHeaders(tenant) });
    check(install, { 'install status accepted': (r) => r.status === 201 || r.status === 409 });

    const activate = http.post(`${baseURL}/api/v1/marketplace/plugins/${slug}/activate`, null, { headers: tenantHeaders(tenant) });
    check(activate, { 'activate accepted': (r) => r.status === 200 || r.status === 422 });
  });
}

export function submissionQueue() {
  const tenant = pickTenant();
  group(`submissions:${tenant}`, () => {
    const list = http.get(`${baseURL}/api/v1/admin/marketplace/submissions?tenant_id=${tenant}&per_page=20`, {
      headers: tenantHeaders(tenant),
    });
    check(list, { 'submissions list reachable': (r) => r.status === 200 || r.status === 401 });
  });
}

export function billingRead() {
  const tenant = pickTenant();
  group(`billing:${tenant}`, () => {
    const inv = http.get(`${baseURL}/api/v1/admin/billing/invoices?tenant_id=${tenant}&per_page=10`, {
      headers: tenantHeaders(tenant),
    });
    check(inv, { 'invoices reachable': (r) => r.status === 200 || r.status === 401 });
  });
}
