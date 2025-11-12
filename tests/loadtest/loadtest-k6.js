// xg2g Load Test using k6
// Comprehensive load testing with multiple scenarios

import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const healthCheckDuration = new Trend('health_check_duration');
const apiDuration = new Trend('api_duration');
const playlistRequests = new Counter('playlist_requests');

// Configuration
const BASE_URL = __ENV.XG2G_URL || 'http://localhost:8080';

// Test configuration
export const options = {
  stages: [
    { duration: '1m', target: 10 },   // Ramp up to 10 users
    { duration: '3m', target: 50 },   // Stay at 50 users
    { duration: '1m', target: 100 },  // Spike to 100 users
    { duration: '2m', target: 50 },   // Scale back to 50
    { duration: '1m', target: 0 },    // Ramp down to 0
  ],
  thresholds: {
    // HTTP errors should be less than 1%
    'errors': ['rate<0.01'],

    // 95% of requests should be below 200ms
    'http_req_duration': ['p(95)<200'],

    // Health checks should be fast
    'health_check_duration': ['p(95)<50'],

    // API calls should be reasonably fast
    'api_duration': ['p(95)<300'],

    // Request rate should be maintained
    'http_reqs': ['rate>10'],
  },
};

// Test setup
export function setup() {
  // Verify service is up
  const res = http.get(`${BASE_URL}/healthz`);
  if (res.status !== 200) {
    throw new Error(`Service not available: ${res.status}`);
  }

  console.log('✓ Service is up and running');
  return { startTime: new Date() };
}

// Main test function
export default function() {
  // Scenario 1: Health Check (fast, frequent)
  group('Health Checks', function() {
    const res = http.get(`${BASE_URL}/healthz`);

    const success = check(res, {
      'health check status is 200': (r) => r.status === 200,
      'health check response time < 100ms': (r) => r.timings.duration < 100,
    });

    errorRate.add(!success);
    healthCheckDuration.add(res.timings.duration);
  });

  sleep(1);

  // Scenario 2: Metrics Endpoint
  group('Metrics', function() {
    const res = http.get(`${BASE_URL}/metrics`);

    const success = check(res, {
      'metrics status is 200': (r) => r.status === 200,
      'metrics contain xg2g namespace': (r) => r.body.includes('xg2g_'),
    });

    errorRate.add(!success);
  });

  sleep(1);

  // Scenario 3: API Status
  group('API Status', function() {
    const res = http.get(`${BASE_URL}/api/status`);

    const success = check(res, {
      'api status is 200': (r) => r.status === 200,
      'api response time < 200ms': (r) => r.timings.duration < 200,
    });

    errorRate.add(!success);
    apiDuration.add(res.timings.duration);
  });

  sleep(1);

  // Scenario 4: M3U Playlist (if configured)
  group('M3U Playlist', function() {
    const res = http.get(`${BASE_URL}/playlist.m3u`, {
      tags: { endpoint: 'playlist' },
    });

    if (res.status === 200) {
      playlistRequests.add(1);

      check(res, {
        'playlist is valid m3u': (r) => r.body.includes('#EXTM3U'),
        'playlist has channels': (r) => r.body.includes('#EXTINF'),
      });
    } else if (res.status !== 404) {
      // Only count as error if not 404 (unconfigured is ok)
      errorRate.add(true);
    }
  });

  sleep(2);

  // Scenario 5: Readiness Probe
  group('Readiness', function() {
    const res = http.get(`${BASE_URL}/readyz`);

    check(res, {
      'readiness status is 200': (r) => r.status === 200,
    });
  });

  sleep(1);
}

// Test teardown
export function teardown(data) {
  const duration = (new Date() - data.startTime) / 1000;
  console.log(`\n✓ Load test completed in ${duration.toFixed(1)}s`);
}

// Handle test summary
export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'summary.json': JSON.stringify(data),
  };
}

// Helper function for text summary
function textSummary(data, options) {
  const { indent = '', enableColors = false } = options || {};

  let output = `\n${indent}Test Summary\n`;
  output += `${indent}============\n\n`;

  // Metrics
  if (data.metrics) {
    output += `${indent}Metrics:\n`;

    for (const [name, metric] of Object.entries(data.metrics)) {
      if (metric.type === 'trend') {
        output += `${indent}  ${name}:\n`;
        output += `${indent}    avg: ${metric.values.avg.toFixed(2)}ms\n`;
        output += `${indent}    min: ${metric.values.min.toFixed(2)}ms\n`;
        output += `${indent}    p50: ${metric.values['p(50)'].toFixed(2)}ms\n`;
        output += `${indent}    p95: ${metric.values['p(95)'].toFixed(2)}ms\n`;
        output += `${indent}    p99: ${metric.values['p(99)'].toFixed(2)}ms\n`;
        output += `${indent}    max: ${metric.values.max.toFixed(2)}ms\n`;
      } else if (metric.type === 'rate') {
        output += `${indent}  ${name}: ${(metric.values.rate * 100).toFixed(2)}%\n`;
      } else if (metric.type === 'counter') {
        output += `${indent}  ${name}: ${metric.values.count}\n`;
      }
    }
  }

  return output;
}
