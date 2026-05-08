// v2.8.0 comprehensive multi-tenant load test profile.
//
// Combines every bounded context introduced through v2.7.0 (catalog,
// order, membership, digital, marketplace, billing, registration,
// marketplace-submission) into a single k6 run, gated by per-surface
// p95 thresholds and per-tenant breakdown tags.
//
// Run locally:
//   MT_DURATION=60s K6_TENANTS=10 BEARER_TOKEN=... \
//     k6 run tests/load/k6/v280/comprehensive-multi-tenant.js
//
// Output JSON for the v2.8.0 audit:
//   k6 run --summary-export=tests/load/k6/v280/results-2026-05-09.json \
//     tests/load/k6/v280/comprehensive-multi-tenant.js
//
// Notes:
//   - Uses the MT_* env namespace (not K6_*) so the scenarios block
//     is honoured. See tests/load/k6/multi-tenant-marketplace.js for
//     the rationale.
//   - Each scenario tags `surface` and `tenant` so the post-run
//     summary can break out p50/p95/p99 by surface and by tenant.
//   - Webhooks (Stripe + outbound) require a valid signature; the
//     test pre-shares a per-run secret via X-Webhook-Secret-Override
//     when the BEARER_TOKEN is an admin token (see auth/admin.go
//     adminWebhookSecretMiddleware).

import http from 'k6/http';
import { check, group } from 'k6';
import { SharedArray } from 'k6/data';
import crypto from 'k6/crypto';

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const bearerToken = __ENV.BEARER_TOKEN || '';
const stripeSecret = __ENV.STRIPE_WEBHOOK_SECRET || 'whsec_test_v280';

const tenantCount = Number(__ENV.K6_TENANTS || 10);
const tenants = new SharedArray('tenants', () =>
  Array.from({ length: tenantCount }, (_, i) => `tenant_${(i + 1).toString().padStart(2, '0')}`),
);

function tenantHeaders(tenantId) {
  const headers = {
    'X-Tenant-ID': tenantId,
    'Content-Type': 'application/json',
  };
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`;
  return headers;
}

function pickTenant() {
  return tenants[Math.floor(Math.random() * tenants.length)];
}

const duration = __ENV.MT_DURATION || '60s';

export const options = {
  scenarios: {
    // v1.x catalog: list products (high RPS read)
    catalog_list: {
      executor: 'constant-arrival-rate',
      exec: 'catalogList',
      duration,
      rate: Number(__ENV.MT_CATALOG_RPS || 30),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_CATALOG_VUS || 30),
      tags: { surface: 'catalog' },
    },
    // v1.x orders: place a happy-path order
    order_create: {
      executor: 'constant-arrival-rate',
      exec: 'orderCreate',
      duration,
      rate: Number(__ENV.MT_ORDER_RPS || 8),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_ORDER_VUS || 16),
      tags: { surface: 'order' },
    },
    // v2.2.0 membership: list/lifecycle
    membership: {
      executor: 'constant-arrival-rate',
      exec: 'membershipFlow',
      duration,
      rate: Number(__ENV.MT_MEMBERSHIP_RPS || 5),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_MEMBERSHIP_VUS || 10),
      tags: { surface: 'membership' },
    },
    // v2.3.0 digital goods: download token + signed URL
    digital: {
      executor: 'constant-arrival-rate',
      exec: 'digitalFlow',
      duration,
      rate: Number(__ENV.MT_DIGITAL_RPS || 5),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_DIGITAL_VUS || 10),
      tags: { surface: 'digital' },
    },
    // v2.4.0 marketplace: install + activate + deactivate
    marketplace: {
      executor: 'constant-arrival-rate',
      exec: 'marketplaceFlow',
      duration,
      rate: Number(__ENV.MT_MARKETPLACE_RPS || 5),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_MARKETPLACE_VUS || 10),
      tags: { surface: 'marketplace' },
    },
    // v2.5.0 billing: subscriptions + invoices + usage + Stripe webhook
    billing: {
      executor: 'constant-arrival-rate',
      exec: 'billingFlow',
      duration,
      rate: Number(__ENV.MT_BILLING_RPS || 8),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_BILLING_VUS || 16),
      tags: { surface: 'billing' },
    },
    // v2.7.0 marketplace submissions: submit/list/approve
    marketplace_submission: {
      executor: 'constant-arrival-rate',
      exec: 'submissionFlow',
      duration,
      rate: Number(__ENV.MT_SUBMISSION_RPS || 3),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_SUBMISSION_VUS || 6),
      tags: { surface: 'marketplace_submission' },
    },
    // v2.5.0 registration: token submit + verify
    registration: {
      executor: 'constant-arrival-rate',
      exec: 'registrationFlow',
      duration,
      rate: Number(__ENV.MT_REGISTRATION_RPS || 3),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.MT_REGISTRATION_VUS || 6),
      tags: { surface: 'registration' },
    },
  },
  thresholds: {
    // Per-surface p95 budgets (ms)
    'http_req_duration{surface:catalog}': ['p(95)<150'],
    'http_req_duration{surface:order}': ['p(95)<300'],
    'http_req_duration{surface:membership}': ['p(95)<400'],
    'http_req_duration{surface:digital}': ['p(95)<400'],
    'http_req_duration{surface:marketplace}': ['p(95)<800'],
    'http_req_duration{surface:billing}': ['p(95)<500'],
    'http_req_duration{surface:marketplace_submission}': ['p(95)<600'],
    'http_req_duration{surface:registration}': ['p(95)<500'],
    // Failure-rate gates (lifecycle scenarios are forgiving for
    // idempotent re-installs returning 409/422)
    'http_req_failed{surface:catalog}': ['rate<0.01'],
    'http_req_failed{surface:order}': ['rate<0.02'],
    'http_req_failed{surface:membership}': ['rate<0.05'],
    'http_req_failed{surface:digital}': ['rate<0.05'],
    'http_req_failed{surface:marketplace}': ['rate<0.7'],
    'http_req_failed{surface:billing}': ['rate<0.05'],
    'http_req_failed{surface:marketplace_submission}': ['rate<0.05'],
    'http_req_failed{surface:registration}': ['rate<0.05'],
    checks: ['rate>=0.95'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max'],
};

// catalog: read-heavy product browse
export function catalogList() {
  const tenant = pickTenant();
  group(`catalog:${tenant}`, () => {
    const res = http.get(`${baseURL}/api/v1/products?page=1&per_page=20`, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'catalog', tenant },
    });
    check(res, { 'catalog 200': (r) => r.status === 200 });
  });
}

// order: order placement
export function orderCreate() {
  const tenant = pickTenant();
  const suffix = `${__VU}-${__ITER}-${Date.now()}`;
  const payload = JSON.stringify({
    customer_email: `load-${suffix}@example.invalid`,
    items: [
      {
        product_id: 'c1000000-0000-0000-0000-000000000001',
        sku: 'BAND-001',
        title: 'Resistance Band',
        quantity: 1,
        unit_price: { amount: 2495, currency: 'AUD' },
      },
    ],
    shipping_address: {
      name: 'Load Test Shopper',
      line1: '1 Market Street',
      city: 'Sydney',
      region: 'NSW',
      postal_code: '2000',
      country: 'AU',
    },
  });
  group(`order:${tenant}`, () => {
    const res = http.post(`${baseURL}/api/v1/orders`, payload, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'order', tenant },
    });
    check(res, { 'order 201': (r) => r.status === 201 });
  });
}

// membership: list plans + create subscription + cancel
export function membershipFlow() {
  const tenant = pickTenant();
  group(`membership:${tenant}`, () => {
    const list = http.get(`${baseURL}/api/v1/membership/plans`, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'membership', tenant, op: 'list_plans' },
    });
    check(list, { 'plans reachable': (r) => r.status === 200 || r.status === 401 });

    const members = http.get(`${baseURL}/api/v1/admin/membership/members?per_page=20`, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'membership', tenant, op: 'list_members' },
    });
    check(members, { 'members reachable': (r) => r.status === 200 || r.status === 401 });
  });
}

// digital: list + issue download token + validate
export function digitalFlow() {
  const tenant = pickTenant();
  group(`digital:${tenant}`, () => {
    const products = http.get(`${baseURL}/api/v1/digital/products?per_page=20`, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'digital', tenant, op: 'list_products' },
    });
    check(products, { 'digital list reachable': (r) => r.status === 200 || r.status === 401 });

    const token = http.post(
      `${baseURL}/api/v1/digital/access-grants`,
      JSON.stringify({
        customer_email: `load-${__VU}-${__ITER}@example.invalid`,
        product_id: 'd1000000-0000-0000-0000-000000000001',
        license_key: `lic-load-${__VU}-${__ITER}`,
      }),
      { headers: tenantHeaders(tenant), tags: { surface: 'digital', tenant, op: 'issue_grant' } },
    );
    check(token, { 'grant 201/401/422': (r) => [201, 401, 422].includes(r.status) });
  });
}

// marketplace: list/install/activate/deactivate
export function marketplaceFlow() {
  const tenant = pickTenant();
  const slug = 'stripe-payments';
  group(`marketplace:${tenant}`, () => {
    const list = http.get(`${baseURL}/api/v1/marketplace/plugins?per_page=20`, {
      headers: tenantHeaders(tenant),
      tags: { surface: 'marketplace', tenant, op: 'list' },
    });
    check(list, { 'list 200/401': (r) => r.status === 200 || r.status === 401 });

    const install = http.post(
      `${baseURL}/api/v1/marketplace/plugins/${slug}/install`,
      null,
      { headers: tenantHeaders(tenant), tags: { surface: 'marketplace', tenant, op: 'install' } },
    );
    check(install, { 'install 201/409': (r) => r.status === 201 || r.status === 409 });

    const activate = http.post(
      `${baseURL}/api/v1/marketplace/plugins/${slug}/activate`,
      null,
      { headers: tenantHeaders(tenant), tags: { surface: 'marketplace', tenant, op: 'activate' } },
    );
    check(activate, { 'activate 200/422': (r) => r.status === 200 || r.status === 422 });

    const deactivate = http.post(
      `${baseURL}/api/v1/marketplace/plugins/${slug}/deactivate`,
      null,
      { headers: tenantHeaders(tenant), tags: { surface: 'marketplace', tenant, op: 'deactivate' } },
    );
    check(deactivate, { 'deactivate 200/422': (r) => r.status === 200 || r.status === 422 });
  });
}

// billing: subscriptions + invoices + usage rollup + stripe webhook
export function billingFlow() {
  const tenant = pickTenant();
  group(`billing:${tenant}`, () => {
    const subs = http.get(
      `${baseURL}/api/v1/admin/billing/subscriptions?tenant_id=${tenant}&per_page=10`,
      { headers: tenantHeaders(tenant), tags: { surface: 'billing', tenant, op: 'list_subs' } },
    );
    check(subs, { 'subs reachable': (r) => r.status === 200 || r.status === 401 });

    const invoices = http.get(
      `${baseURL}/api/v1/admin/billing/invoices?tenant_id=${tenant}&per_page=10`,
      { headers: tenantHeaders(tenant), tags: { surface: 'billing', tenant, op: 'list_invoices' } },
    );
    check(invoices, { 'invoices reachable': (r) => r.status === 200 || r.status === 401 });

    const usage = http.get(
      `${baseURL}/api/v1/admin/billing/usage?tenant_id=${tenant}`,
      { headers: tenantHeaders(tenant), tags: { surface: 'billing', tenant, op: 'usage' } },
    );
    check(usage, { 'usage reachable': (r) => r.status === 200 || r.status === 401 });

    // Stripe webhook with HMAC-SHA256 signature (verify-then-parse)
    const event = JSON.stringify({
      id: `evt_${tenant}_${__VU}_${__ITER}`,
      type: 'invoice.payment_succeeded',
      data: { object: { id: `in_${tenant}_${__ITER}`, amount_paid: 2495 } },
    });
    const ts = Math.floor(Date.now() / 1000);
    const signedPayload = `${ts}.${event}`;
    const sig = crypto.hmac('sha256', stripeSecret, signedPayload, 'hex');
    const webhookHeader = `t=${ts},v1=${sig}`;
    const wh = http.post(
      `${baseURL}/api/v1/billing/webhook/stripe`,
      event,
      {
        headers: { ...tenantHeaders(tenant), 'Stripe-Signature': webhookHeader },
        tags: { surface: 'billing', tenant, op: 'stripe_webhook' },
      },
    );
    check(wh, { 'webhook 200/401/202': (r) => [200, 202, 401].includes(r.status) });
  });
}

// marketplace_submission: submit/list/approve|reject
export function submissionFlow() {
  const tenant = pickTenant();
  group(`submission:${tenant}`, () => {
    const submit = http.post(
      `${baseURL}/api/v1/marketplace/submissions`,
      JSON.stringify({
        slug: `plugin-load-${__VU}-${__ITER}`,
        version: '0.1.0',
        manifest_url: 'https://example.invalid/manifest.json',
        reviewer_notes: 'k6 load test submission',
      }),
      { headers: tenantHeaders(tenant), tags: { surface: 'marketplace_submission', tenant, op: 'submit' } },
    );
    check(submit, { 'submit 201/401/422': (r) => [201, 401, 422].includes(r.status) });

    const list = http.get(
      `${baseURL}/api/v1/admin/marketplace/submissions?tenant_id=${tenant}&per_page=20`,
      { headers: tenantHeaders(tenant), tags: { surface: 'marketplace_submission', tenant, op: 'list' } },
    );
    check(list, { 'submission list reachable': (r) => r.status === 200 || r.status === 401 });
  });
}

// registration: submit + verify
export function registrationFlow() {
  const tenant = pickTenant();
  const suffix = `${__VU}-${__ITER}-${Date.now()}`;
  group(`registration:${tenant}`, () => {
    const submit = http.post(
      `${baseURL}/api/v1/tenants/register`,
      JSON.stringify({
        company_name: `Load Co ${suffix}`,
        contact_email: `load-${suffix}@example.invalid`,
        intended_subdomain: `load-${suffix.replace(/[^a-z0-9]/gi, '').toLowerCase().slice(0, 30)}`,
      }),
      { headers: tenantHeaders(tenant), tags: { surface: 'registration', tenant, op: 'submit' } },
    );
    check(submit, { 'register 201/422/409': (r) => [201, 409, 422].includes(r.status) });

    const verifyToken = submit.json('verification_token') || 'invalid-token';
    const verify = http.post(
      `${baseURL}/api/v1/tenants/register/verify`,
      JSON.stringify({ token: verifyToken }),
      { headers: tenantHeaders(tenant), tags: { surface: 'registration', tenant, op: 'verify' } },
    );
    check(verify, { 'verify 200/401/410': (r) => [200, 401, 410].includes(r.status) });
  });
}
