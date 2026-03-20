import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 10  },  // ramp up to 10 users
    { duration: '30s', target: 50  },  // ramp up to 50 users
    { duration: '30s', target: 100 },  // ramp up to 100 users
    { duration: '30s', target: 200 },  // push to 200 users
    { duration: '30s', target: 0   },  // ramp back down
  ],
};

// Internal cluster DNS — no public internet, requests stay inside the EC2 node
const BASE = 'http://inventory-service.movievault.svc.cluster.local';

export default function () {
  const movies = http.get(`${BASE}/movies`);
  check(movies, { 'movies 200': (r) => r.status === 200 });

  const health = http.get(`${BASE}/health`);
  check(health, { 'health ok': (r) => r.status === 200 });

  sleep(0.1);
}