
import React from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import V3Player from '../../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../../src/client-ts/sdk.gen';

// Mock SDK
vi.mock('../../src/client-ts/sdk.gen', async () => {
  return {
    getRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('User-Facing Error Matrix (ERROR_MAP.md)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const scenarios = [
    {
      status: 401,
      name: '401 UNAUTHORIZED',
      expectText: new RegExp('player.authFailed|Authentication failed', 'i'),
    },
    {
      status: 403,
      name: '403 FORBIDDEN',
      expectText: new RegExp('player.authFailed|Authentication failed', 'i'),
    },
    {
      status: 409,
      name: '409 CONFLICT (Busy)',
      headers: { 'Retry-After': '10' },
      expectText: new RegExp('player.retryAfter|retry in 10', 'i'),
    },
    // 410 Gone logic is harder to test via initial fetch as fetch failure results in error screen directly.
    // But let's verify it maps to a "Gone" message or similar if implemented, or general error.
    // The ERROR_MAP.md likely specifies a specific message for 410 on heartbeat, 
    // but for initial load 410 usually means "Recording Deleted" or "Session Expired".
    // Let's check generally that it errors.
    {
      status: 410,
      name: '410 GONE',
      expectText: new RegExp('player.notAvailable|Playback not available', 'i'), // Assuming map
    }
  ];

  scenarios.forEach((scenario) => {
    it(`maps ${scenario.name} to correct UI state`, async () => {
      const error = {
        body: { title: 'Test Error', detail: 'Simulated failure' },
        code: scenario.status, // hey-api uses 'code' or 'status'? usually checking error.status
      };

      // Hey-api throws errors that have 'status' property usually? or client-fetch?
      // Simulation: reject with an object that has status.

      // Wait, V3Player logic checks `error.status` or `response.status`.
      // In `V3Player.tsx`, catch block: `if (err.status === 409) ...`

      // V3Player expects { data, error, response } structure from sdk call
      (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
        data: undefined,
        error: error,
        response: {
          status: scenario.status,
          headers: {
            get: (key: string) => (scenario.headers as any)?.[key]
          }
        }
      });

      render(<V3Player autoStart={true} recordingId="rec-matrix-1" />);

      await waitFor(() => {
        expect(screen.getByText(scenario.expectText)).toBeInTheDocument();
      });
    });
  });
});
