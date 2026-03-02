
import { test, expect, request } from '@playwright/test';
const fs = require('fs');
const path = require('path');

test.describe('E2E Harness / Fixture Wiring', () => {

  // 1. Reset Fixture State
  test.beforeAll(async ({ request }) => {
    // Assume API available at localhost:3001
    const response = await request.post('http://localhost:3001/__admin/scenario', {
      data: { id: 'minimal-boot' }
    });
    expect(response.ok()).toBeTruthy();
  });

  test('Fixture Server serves capabilities', async ({ request }) => {
    const response = await request.get('http://localhost:3001/api/v3/system/capabilities');
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body['contracts.playbackInfoDecision']).toBe('absent');
  });
});
