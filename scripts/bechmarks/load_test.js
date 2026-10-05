import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const rideRequestDuration = new Trend('ride_request_duration');
const rideRequestSuccessRate = new Rate('ride_request_success_rate');

export const options = {
  scenarios: {
    // Ramp-up test
    rampup: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 100 },
        { duration: '5m', target: 500 },
        { duration: '2m', target: 1000 },
        { duration: '5m', target: 2000 },
        { duration: '2m', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    ride_request_duration: ['p(95)<300'],
    ride_request_success_rate: ['rate>0.99'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  // Login
  const loginRes = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
    email: 'test@nidaw.com',
    password: 'TestPass123!',
  }), { headers: { 'Content-Type': 'application/json' } });

  check(loginRes, {
    'login status is 200': (r) => r.status === 200,
  });

  const token = JSON.parse(loginRes.body).access_token;

  // Request ride
  const start = Date.now();
  const rideRes = http.post(`${BASE_URL}/api/v1/nidus/rides`, JSON.stringify({
    pickup_lat: 40.7128,
    pickup_lng: -74.0060,
    dropoff_lat: 40.7589,
    dropoff_lng: -73.9851,
    ride_type: 'standard',
  }), {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });

  const duration = Date.now() - start;
  rideRequestDuration.add(duration);

  const success = check(rideRes, {
    'ride request status is 201': (r) => r.status === 201,
  });

  rideRequestSuccessRate.add(success ? 1 : 0);

  sleep(1);
}