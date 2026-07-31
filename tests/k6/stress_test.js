import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate } from 'k6/metrics';

const failedUploads = new Counter('failed_uploads');
const successfulUploads = new Counter('successful_uploads');
const slowlorisConnections = new Counter('slowloris_connections');
const healthyConnectionRate = new Rate('healthy_connection_rate');

export const options = {
  scenarios: {
    high_throughput: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 200 },
        { duration: '30s', target: 500 },
        { duration: '60s', target: 1000 },
        { duration: '30s', target: 1000 },
        { duration: '10s', target: 0 },
      ],
      exec: 'highThroughput',
    },
    slowloris_trickle: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 50 },
        { duration: '60s', target: 200 },
        { duration: '30s', target: 0 },
      ],
      exec: 'slowlorisTrickle',
    },
  },
  thresholds: {
    failed_uploads: ['count<100'],
    healthy_connection_rate: ['rate>0.95'],
    http_req_duration: ['p(99)<15000'],
  },
};

const BASE_URL = __ENV.MOMO_URL || 'http://localhost:3333';
const AUTH_TOKEN = __ENV.AUTH_TOKEN || 'secret'; // notsecret
const BUCKET = __ENV.BUCKET || 'stress-bucket';

function generatePayload(size) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let payload = '';
  for (let i = 0; i < size; i++) {
    payload += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return payload;
}

export function highThroughput() {
  const key = `k6-stress-${Date.now()}-${__VU}-${__ITER}`;
  const payload = generatePayload(1024 * 256);

  const response = http.put(
    `${BASE_URL}/${BUCKET}/${key}`,
    payload,
    {
      headers: {
        'Authorization': `Bearer ${AUTH_TOKEN}`,
        'Content-Type': 'application/octet-stream',
      },
      timeout: '15s',
    }
  );

  const ok = check(response, {
    'PUT status is 200': (r) => r.status === 200,
  });

  if (ok) {
    successfulUploads.add(1);
    healthyConnectionRate.add(true);
  } else {
    failedUploads.add(1);
    healthyConnectionRate.add(false);
  }

  sleep(0.05);
}

export function slowlorisTrickle() {
  const key = `k6-slow-${Date.now()}-${__VU}-${__ITER}`;
  const payload = generatePayload(1024 * 16);

  slowlorisConnections.add(1);

  const response = http.put(
    `${BASE_URL}/${BUCKET}/${key}`,
    payload,
    {
      headers: {
        'Authorization': `Bearer ${AUTH_TOKEN}`,
        'Content-Type': 'application/octet-stream',
      },
      timeout: '30s',
    }
  );

  const ok = check(response, {
    'Slowloris PUT handled': (r) => r.status === 200 || r.status === 408 || r.status === 429,
  });

  if (!ok) {
    failedUploads.add(1);
  }

  sleep(Math.random() * 2 + 1);
}
