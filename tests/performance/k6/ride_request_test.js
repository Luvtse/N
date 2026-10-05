import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { randomIntBetween, randomString } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// Custom metrics
const rideRequestDuration = new Trend('ride_request_duration');
const rideRequestSuccessRate = new Rate('ride_request_success_rate');
const matchingDuration = new Trend('matching_duration');

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
    // Spike test
    spike: {
      executor: 'ramping-arrival-rate',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 500,
      maxVUs: 2000,
      stages: [
        { duration: '1m', target: 50 },
        { duration: '30s', target: 500 },
        { duration: '30s', target: 2000 },
        { duration: '30s', target: 500 },
        { duration: '1m', target: 0 },
      ],
      startTime: '20m',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    ride_request_duration: ['p(95)<300'],
    ride_request_success_rate: ['rate>0.99'],
    matching_duration: ['p(95)<200'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Pre-generate auth tokens
const users = [];
for (let i = 0; i < 2000; i++) {
  users.push({
    email: `rider${i}@loadtest.nidaw.com`,
    password: 'TestPass123!',
  });
}

export function setup() {
  // Authenticate users and cache tokens
  const tokens = [];
  for (let i = 0; i < Math.min(users.length, 500); i++) {
    const res = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
      email: users[i].email,
      password: users[i].password,
    }), { headers: { 'Content-Type': 'application/json' } });
    
    if (res.status === 200) {
      const body = JSON.parse(res.body);
      tokens.push(body.access_token);
    }
  }
  return { tokens };
}

export default function (data) {
  const token = data.tokens[randomIntBetween(0, data.tokens.length - 1)];
  
  // Simulate realistic user flow
  const scenario = randomIntBetween(1, 100);
  
  if (scenario <= 60) {
    // 60% - Request ride
    requestRide(token);
  } else if (scenario <= 80) {
    // 20% - Check ride status
    checkRideStatus(token);
  } else if (scenario <= 90) {
    // 10% - Search hotels
    searchHotels(token);
  } else {
    // 10% - Order food
    orderFood(token);
  }
  
  sleep(randomIntBetween(1, 3));
}

function requestRide(token) {
  const start = Date.now();
  
  const payload = JSON.stringify({
    pickup_lat: 40.7128 + (Math.random() - 0.5) * 0.1,
    pickup_lng: -74.0060 + (Math.random() - 0.5) * 0.1,
    dropoff_lat: 40.7589 + (Math.random() - 0.5) * 0.1,
    dropoff_lng: -73.9851 + (Math.random() - 0.5) * 0.1,
    ride_type: ['standard', 'premium', 'electric'][randomIntBetween(0, 2)],
    payment_method_id: 'pm_test_123',
  });
  
  const res = http.post(`${BASE_URL}/api/v1/nidus/rides`, payload, {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    tags: { name: 'POST /rides' },
  });
  
  const duration = Date.now() - start;
  rideRequestDuration.add(duration);
  
  const success = check(res, {
    'ride request status is 201': (r) => r.status === 201,
    'response has ride_id': (r) => {
      try {
        return JSON.parse(r.body).ride_id !== undefined;
      } catch {
        return false;
      }
    },
  });
  
  rideRequestSuccessRate.add(success ? 1 : 0);
  
  if (success && res.status === 201) {
    const body = JSON.parse(res.body);
    // Simulate checking status
    sleep(randomIntBetween(2, 5));
    http.get(`${BASE_URL}/api/v1/nidus/rides/${body.ride_id}`, {
      headers: { 'Authorization': `Bearer ${token}` },
      tags: { name: 'GET /rides/:id' },
    });
  }
}

function checkRideStatus(token) {
  const rideId = `ride_${randomString(10)}`;
  http.get(`${BASE_URL}/api/v1/nidus/rides/${rideId}`, {
    headers: { 'Authorization': `Bearer ${token}` },
    tags: { name: 'GET /rides/:id' },
  });
  sleep(1);
}

function searchHotels(token) {
  const params = new URLSearchParams({
    city: ['New York', 'London', 'Tokyo', 'Paris'][randomIntBetween(0, 3)],
    check_in: '2026-08-01',
    check_out: '2026-08-05',
    guests: randomIntBetween(1, 4).toString(),
    limit: '20',
  });
  
  http.get(`${BASE_URL}/api/v1/haven/hotels?${params}`, {
    headers: { 'Authorization': `Bearer ${token}` },
    tags: { name: 'GET /hotels' },
  });
}

function orderFood(token) {
  const payload = JSON.stringify({
    restaurant_id: `rest_${randomString(8)}`,
    items: [
      { menu_item_id: `item_${randomString(8)}`, quantity: randomIntBetween(1, 3) },
    ],
    delivery_address: '123 Test St, New York, NY',
    delivery_lat: 40.7128,
    delivery_lng: -74.0060,
    payment_method_id: 'pm_test_123',
  });
  
  http.post(`${BASE_URL}/api/v1/vorax/orders`, payload, {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    tags: { name: 'POST /orders' },
  });
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'results/load_test_report.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data, opts) {
  return JSON.stringify(data.metrics, null, 2);
}