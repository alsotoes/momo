import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const putErrors = new Counter('put_errors');
const getErrors = new Counter('get_errors');
const uploadLatency = new Trend('upload_latency', true);

export const options = {
  stages: [
    { duration: '30s', target: 50 },
    { duration: '1m', target: 100 },
    { duration: '30s', target: 200 },
    { duration: '1m', target: 200 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<5000', 'p(99)<10000'],
    put_errors: ['count<50'],
    get_errors: ['count<10'],
  },
};

const BASE_URL = __ENV.MOMO_URL || 'http://localhost:3333';
const AUTH_TOKEN = __ENV.AUTH_TOKEN || 'secret';
const BUCKET = __ENV.BUCKET || 'test-bucket';

function generatePayload(size) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let payload = '';
  for (let i = 0; i < size; i++) {
    payload += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return payload;
}

export default function () {
  const key = `k6-load-${Date.now()}-${__VU}-${__ITER}`;
  const payload = generatePayload(1024 * 64);

  const putResponse = http.put(
    `${BASE_URL}/${BUCKET}/${key}`,
    payload,
    {
      headers: {
        'Authorization': `Bearer ${AUTH_TOKEN}`,
        'Content-Type': 'application/octet-stream',
      },
      tags: { operation: 'PUT' },
    }
  );

  check(putResponse, {
    'PUT status is 200': (r) => r.status === 200,
  }) || putErrors.add(1);

  uploadLatency.add(putResponse.timings.duration);

  sleep(0.1);

  const getResponse = http.get(
    `${BASE_URL}/${BUCKET}/${key}`,
    {
      headers: { 'Authorization': `Bearer ${AUTH_TOKEN}` },
      tags: { operation: 'GET' },
    }
  );

  check(getResponse, {
    'GET status is 200': (r) => r.status === 200,
    'GET body matches': (r) => r.body === payload,
  }) || getErrors.add(1);

  sleep(0.5);
}
