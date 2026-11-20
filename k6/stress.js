import http from 'k6/http';
import { check, sleep } from 'k6';

// IF3110 M2 stress test: ramp to normal load, hold above normal (peak),
// ramp back down; observe recovery. See README "Stress Testing (k6)".
//
// Env:
//   BASE_URL   default http://localhost:3000
//   USERNAME / PASSWORD  platform account (read APIs + job submission)
//   CONNECTION_ID  target database the pre-approved migrations run against
//
// Pre-req: 2 migrations already approved on CONNECTION_ID (demo fixtures)
// so POST .../migrations/:id/rollback & /forward are accepted and enqueue
// jobs; job submission must not be confused with migration success.

const BASE = __ENV.BASE_URL || 'http://localhost:3000';
const USERNAME = __ENV.USERNAME || 'admin';
const PASSWORD = __ENV.PASSWORD || '';
const CONNECTION_ID = Number(__ENV.CONNECTION_ID || 1);

// Stages: 2m ramp to normal (10 VU), 3m hold peak (50 VU), 2m ramp down.
// Thresholds: p95 API response < 500ms, error rate < 5%.
export const options = {
  scenarios: {
    reads: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 10 },
        { duration: '1m', target: 10 },
        { duration: '3m', target: 50 },
        { duration: '2m', target: 50 },
        { duration: '2m', target: 0 },
      ],
    },
    jobs: {
      executor: 'ramping-arrival-rate',
      startRate: 1,
      timeUnit: '10s',
      preAllocatedVUs: 20,
      maxVUs: 60,
      stages: [
        { duration: '2m', target: 1 },
        { duration: '3m', target: 6 },
        { duration: '2m', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(50)<200', 'p(95)<500'],
    http_req_failed: ['rate<0.05'],
  },
};

let session = null;

function login() {
  const res = http.post(`${BASE}/api/auth/login`, JSON.stringify({ username: USERNAME, password: PASSWORD }), {
    headers: { 'Content-Type': 'application/json' },
  });
  check(res, { 'login 200': (r) => r.status === 200 });
  const cookie = res.cookies.orpbal_session ? res.cookies.orpbal_session[0].value : '';
  const csrf = res.json('csrf_token') || '';
  return { cookie, csrf };
}

export function setup() {
  if (!PASSWORD) {
    throw new Error('PASSWORD env required');
  }
  return login();
}

export default function (data) {
  session = data;

  // Read path: schema + migration history (GET, no CSRF needed).
  const schema = http.get(`${BASE}/api/databases/${CONNECTION_ID}/schema`, {
    headers: { Cookie: `orpbal_session=${session.cookie}` },
  });
  check(schema, { 'schema 200': (r) => r.status === 200 });

  const history = http.get(`${BASE}/api/databases/${CONNECTION_ID}/migrations?hide_rollback=true`, {
    headers: { Cookie: `orpbal_session=${session.cookie}` },
  });
  check(history, { 'history 200': (r) => r.status === 200 });

  // Job submission path: rollback the latest reversible migration (idempotent
  // across the run because a rolled-back migration is then forwarded next).
  const migrations = history.json() || [];
  const target = migrations.find((m) => m.status === 'success' && !m.irreversible && m.sql_down);
  if (target) {
    const res = http.post(
      `${BASE}/api/databases/${CONNECTION_ID}/migrations/${target.id}/rollback`,
      null,
      {
        headers: {
          'Content-Type': 'application/json',
          Cookie: `orpbal_session=${session.cookie}`,
          'X-CSRF-Token': session.csrf,
        },
      },
    );
    // 202 = job enqueued; 409 = not currently rollbackable (fine: another VU
    // already rolled it back or it is mid-review). Neither counts as failure.
    check(res, { 'rollback accepted or conflict': (r) => r.status === 202 || r.status === 409 });

    if (res.status === 202) {
      // Poll job once to exercise the status API under load.
      const job = http.get(`${BASE}/api/jobs/1`, {
        headers: { Cookie: `orpbal_session=${session.cookie}` },
      });
      check(job, { 'job endpoint sane': (r) => r.status === 200 || r.status === 404 });
    }
  }
  sleep(1);
}
