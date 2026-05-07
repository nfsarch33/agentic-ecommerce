import http from 'k6/http';
import { check, group } from 'k6';

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const bearerToken = __ENV.BEARER_TOKEN || '';
const productID = __ENV.PRODUCT_ID || 'b1000000-0000-0000-0000-000000000001';
const aiProductID = __ENV.AI_PRODUCT_ID || productID;
const workflowProductID = __ENV.WORKFLOW_PRODUCT_ID || productID;

const authHeaders = bearerToken
  ? { Authorization: `Bearer ${bearerToken}`, 'Content-Type': 'application/json' }
  : { 'Content-Type': 'application/json' };

export const options = {
  scenarios: {
    product_catalog: {
      executor: 'constant-arrival-rate',
      exec: 'productCatalog',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_CATALOG_RPS || 25),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_CATALOG_VUS || 20),
      tags: { endpoint: 'product_catalog' },
    },
    order_creation: {
      executor: 'constant-arrival-rate',
      exec: 'orderCreation',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_ORDER_RPS || 10),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_ORDER_VUS || 15),
      tags: { endpoint: 'order_creation' },
    },
    ai_generation_mocked: {
      executor: 'constant-arrival-rate',
      exec: 'aiGeneration',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_AI_RPS || 3),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_AI_VUS || 6),
      tags: { endpoint: 'ai_generation_mocked' },
    },
    temporal_workflow_start: {
      executor: 'constant-arrival-rate',
      exec: 'temporalWorkflowStart',
      duration: __ENV.K6_DURATION || '30s',
      rate: Number(__ENV.K6_WORKFLOW_RPS || 5),
      timeUnit: '1s',
      preAllocatedVUs: Number(__ENV.K6_WORKFLOW_VUS || 8),
      tags: { endpoint: 'temporal_workflow_start' },
    },
  },
  thresholds: {
    'http_req_failed{endpoint:product_catalog}': ['rate<0.01'],
    'http_req_duration{endpoint:product_catalog}': ['p(95)<100'],
    'http_req_failed{endpoint:order_creation}': ['rate<0.01'],
    'http_req_duration{endpoint:order_creation}': ['p(95)<200'],
    'http_req_failed{endpoint:ai_generation_mocked}': ['rate<0.01'],
    'http_req_duration{endpoint:ai_generation_mocked}': ['p(95)<2000'],
    'http_req_failed{endpoint:temporal_workflow_start}': ['rate<0.01'],
    'http_req_duration{endpoint:temporal_workflow_start}': ['p(95)<500'],
  },
};

export function productCatalog() {
  group('product catalog', () => {
    const res = http.get(`${baseURL}/api/v1/products?page=1&per_page=20`, {
      tags: { endpoint: 'product_catalog' },
    });
    check(res, {
      'catalog status is 200': (r) => r.status === 200,
      'catalog has products array': (r) => Array.isArray(r.json('products')),
    });
  });
}

export function orderCreation() {
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
  const res = http.post(`${baseURL}/api/v1/orders`, payload, {
    headers: { 'Content-Type': 'application/json' },
    tags: { endpoint: 'order_creation' },
  });
  check(res, {
    'order status is 201': (r) => r.status === 201,
    'order id present': (r) => Boolean(r.json('id')),
  });
}

export function aiGeneration() {
  const payload = JSON.stringify({
    style: 'professional',
    max_words: 120,
    keywords: ['resistance band set', 'home workouts'],
  });
  const res = http.post(`${baseURL}/api/v1/products/${aiProductID}/generate-description`, payload, {
    headers: authHeaders,
    tags: { endpoint: 'ai_generation_mocked' },
  });
  check(res, {
    'ai generation status is 200': (r) => r.status === 200,
    'ai description present': (r) => Boolean(r.json('description')),
  });
}

export function temporalWorkflowStart() {
  const payload = JSON.stringify({
    product_id: workflowProductID,
    requested_by: 'k6-load-test',
  });
  const res = http.post(`${baseURL}/api/v1/workflows/product-publish`, payload, {
    headers: authHeaders,
    tags: { endpoint: 'temporal_workflow_start' },
  });
  check(res, {
    'workflow start status is 202': (r) => r.status === 202,
    'workflow id present': (r) => Boolean(r.json('workflow_id')),
  });
}
