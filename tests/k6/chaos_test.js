import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Trend, Rate } from 'k6/metrics';

const deleteErrors = new Counter('delete_errors');
const replicationLatency = new Trend('replication_latency', true);
const consistencyCheckRate = new Rate('consistency_check_rate');

export const options = {
  stages: [
    { duration: '2m', target: 100 },
    { duration: '5m', target: 100 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.005'],
    delete_errors: ['count<5'],
    consistency_check_rate: ['rate>0.99'],
  },
};

const PRIMARY_URL = __ENV.MOMO_PRIMARY || 'http://localhost:3333';
const REPLICA_URLS = (__ENV.MOMO_REPLICAS || 'http://localhost:3334,http://localhost:3335').split(',');
const AUTH_TOKEN = __ENV.AUTH_TOKEN || 'secret'; // notsecret
const BUCKET = __ENV.BUCKET || 'chaos-bucket';

function generatePayload(size) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  let payload = '';
  for (let i = 0; i < size; i++) {
    payload += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return payload;
}

export default function () {
  const key = `k6-chaos-${Date.now()}-${__VU}-${__ITER}`;
  const payload = generatePayload(1024 * 128);

  group('upload_and_verify_replication', function () {
    const putStart = Date.now();

    const putResponse = http.put(
      `${PRIMARY_URL}/${BUCKET}/${key}`,
      payload,
      {
        headers: {
          'Authorization': `Bearer ${AUTH_TOKEN}`,
          'Content-Type': 'application/octet-stream',
        },
      }
    );

    check(putResponse, {
      'PUT to primary succeeds': (r) => r.status === 200,
    });

    sleep(1);

    let allReplicasConsistent = true;
    for (let i = 0; i < REPLICA_URLS.length; i++) {
      const replicaUrl = REPLICA_URLS[i].trim();
      const getResponse = http.get(
        `${replicaUrl}/${BUCKET}/${key}`,
        {
          headers: { 'Authorization': `Bearer ${AUTH_TOKEN}` },
        }
      );

      const ok = check(getResponse, {
        [`GET from replica ${i} succeeds`]: (r) => r.status === 200,
        [`GET from replica ${i} matches`]: (r) => r.body === payload,
      });

      if (!ok) {
        allReplicasConsistent = false;
      }
    }

    consistencyCheckRate.add(allReplicasConsistent);
    replicationLatency.add(Date.now() - putStart);
  });

  sleep(0.5);

  group('delete_and_verify_tombstone', function () {
    const deleteResponse = http.del(
      `${PRIMARY_URL}/${BUCKET}/${key}`,
      null,
      {
        headers: { 'Authorization': `Bearer ${AUTH_TOKEN}` },
      }
    );

    check(deleteResponse, {
      'DELETE succeeds': (r) => r.status === 200 || r.status === 204,
    }) || deleteErrors.add(1);

    sleep(1);

    for (let i = 0; i < REPLICA_URLS.length; i++) {
      const replicaUrl = REPLICA_URLS[i].trim();
      const getResponse = http.get(
        `${replicaUrl}/${BUCKET}/${key}`,
        {
          headers: { 'Authorization': `Bearer ${AUTH_TOKEN}` },
        }
      );

      check(getResponse, {
        [`GET after delete returns 404 on replica ${i}`]: (r) => r.status === 404,
      });
    }
  });

  sleep(1);
}
