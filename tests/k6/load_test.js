import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const putErrors = new Counter('put_errors');
const uploadLatency = new Trend('upload_latency', true);

export const options = {
  stages: [
    { duration: '10s', target: 10 },
    { duration: '30s', target: 20 },
    { duration: '10s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<10000'],
    put_errors: ['count<100'],
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
  const payload = generatePayload(1024 * 16);

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

  sleep(0.2);
}
