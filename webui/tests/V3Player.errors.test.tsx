import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as sdk from '../src/client-ts/sdk.gen';

vi.mock('../src/client-ts/sdk.gen', async () => {
  const actual = await vi.importActual<any>('../src/client-ts/sdk.gen');
  return {
    ...actual,
    getRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player Error Semantics (UI-ERR-PLAYER-001)', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = vi.fn();
    vi.clearAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  const flushMicrotasks = async () => {
    await Promise.resolve();
    await Promise.resolve();
  };

  it('handles 409 LEASE_BUSY with Retry-After hint', async () => {
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
      error: { code: 'LEASE_BUSY' },
      response: {
        status: 409,
        headers: new Map([['Retry-After', '30']])
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-1" />);

    await waitFor(() => {
      expect(screen.getByText(/player.leaseBusy/i)).toBeInTheDocument();
      expect(screen.getByText(/player.retryAfter/i)).toBeInTheDocument();
    });
  });

  it('handles 401/403 Authentication Failure', async () => {
    (globalThis.fetch as any).mockImplementation((url: string) => {
      if (url.includes('/sessions/ping')) {
        return Promise.resolve({
          ok: false,
          status: 401,
          headers: new Map(),
          json: async () => ({ detail: 'Invalid token' })
        });
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
    });

    render(<V3Player autoStart={true} recordingId="rec-1" />);

    await waitFor(() => {
      expect(screen.getByText(/player.authFailed/i)).toBeInTheDocument();
    });
  });

  it('tears down on 410 GONE (Session Expired) during heartbeat', async () => {
    let heartbeatCount = 0;

    (globalThis.fetch as any).mockImplementation((url: string) => {
      if (url.includes('/intents')) return Promise.resolve({ ok: true, status: 200, json: async () => ({ sessionId: 'sess-123' }) });
      if (url.includes('/sessions/sess-123') && !url.includes('/heartbeat')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            state: 'READY',
            playbackUrl: '/live.m3u8',
            heartbeat_interval: 1
          })
        });
      }
      if (url.includes('/heartbeat')) {
        heartbeatCount++;
        if (heartbeatCount === 1) return Promise.resolve({ ok: true, status: 200, json: async () => ({ lease_expires_at: '...' }) });
        return Promise.resolve({ ok: false, status: 410, json: async () => ({ detail: 'Expired' }) });
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => ({}) });
    });

    vi.useFakeTimers();

    // Render with channel to trigger LIVE path which has heartbeats
    const mockChannel = { id: 'ch-1', serviceRef: '1:0:1:...' };
    render(<V3Player autoStart={true} channel={mockChannel as any} />);

    // Wait for READY state (Live path)
    await waitFor(() => {
      expect(screen.getByText(/player.statusStates.ready/i)).toBeInTheDocument();
    });

    // Advance time to trigger first heartbeat (success)
    await vi.advanceTimersByTimeAsync(1100);
    // Trigger second (failing) heartbeat
    await vi.advanceTimersByTimeAsync(1100);

    await waitFor(() => {
      expect(screen.getByText(/player.sessionExpired/i)).toBeInTheDocument();
    });

    vi.useRealTimers();
  });
});
