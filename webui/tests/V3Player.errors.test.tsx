import React from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach, afterAll } from 'vitest';
import * as sdk from '../src/client-ts';
import { suppressExpectedConsoleNoise } from './helpers/consoleNoise';

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    postRecordingPlaybackInfo: vi.fn(),
    postLivePlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player Error Semantics (UI-ERR-PLAYER-001)', () => {
  const originalFetch = globalThis.fetch;
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      // Expected negative-path diagnostics asserted by this suite.
      error: [
        /PlayerError: player\.sessionFailed: SESSION_GONE: recording_deleted/i,
        /\[V3Player\]\[Heartbeat\] Session expired \(410\)/i
      ],
      warn: [
        /Failed to stop v3 session/i,
        /Failed to parse URL from \/api\/v3\/intents/i
      ]
    });
  });

  beforeEach(() => {
    globalThis.fetch = vi.fn();
    vi.clearAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => {
      if (contentType === 'application/vnd.apple.mpegurl') {
        return 'probably';
      }
      return '';
    });
    (sdk.postLivePlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'native_hls',
        requestId: 'live-decision-errors-1',
        playbackDecisionToken: 'live-token-errors-1',
        decision: { reasons: ['direct_stream_match'] },
      },
      response: { status: 200, headers: new Map() }
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
  });

  const flushMicrotasks = async () => {
    await Promise.resolve();
    await Promise.resolve();
  };

  it('handles 409 LEASE_BUSY with Retry-After hint', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
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
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      error: { title: 'Unauthorized' },
      response: {
        status: 401,
        headers: new Map(),
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-1" />);

    await waitFor(() => {
      expect(screen.getByText(/player.authFailed/i)).toBeInTheDocument();
    });
  });

  it('does not retry readiness loop after 410 Gone and enters terminal error state', async () => {
    let readinessCalls = 0;
    const mockChannel = { id: 'ch-410', serviceRef: '1:0:1:...' };

    const response = (
      status: number,
      body: Record<string, unknown> = {},
      headers: Record<string, string> = {}
    ) => ({
      ok: status >= 200 && status < 300,
      status,
      url: 'http://localhost/api/v3/sessions/sess-410',
      headers: {
        get: (key: string) => headers[key] ?? headers[key.toLowerCase()] ?? null
      },
      json: async () => body,
      text: async () => JSON.stringify(body)
    });

    (globalThis.fetch as any).mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve(
          response(200, {
            mode: 'native_hls',
            requestId: 'live-decision-errors-410',
            playbackDecisionToken: 'live-token-errors-410',
            decision: { reasons: ['direct_stream_match'] }
          })
        );
      }

      if (url.includes('/intents')) {
        const parsed = init?.body ? JSON.parse(String(init.body)) : {};
        if (parsed?.type === 'stream.start') {
          return Promise.resolve(response(200, { sessionId: 'sess-410' }));
        }
        return Promise.resolve(response(200, {})); // stream.stop
      }

      if (url.includes('/sessions/sess-410') && !url.includes('/heartbeat')) {
        readinessCalls++;
        return Promise.resolve(
          response(410, {
            reason: 'SESSION_GONE',
            reason_detail: 'recording_deleted',
            requestId: 'req-410'
          })
        );
      }

      return Promise.resolve(response(200, {}));
    });

    vi.useFakeTimers();
    try {
      render(<V3Player autoStart={true} channel={mockChannel as any} />);

      await act(async () => {
        await flushMicrotasks();
        await flushMicrotasks();
        await vi.advanceTimersByTimeAsync(0);
        await flushMicrotasks();
      });

      const alert = screen.getByRole('alert');
      expect(alert).toHaveTextContent(/player\.sessionFailed/i);
      expect(readinessCalls).toBe(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
        await flushMicrotasks();
      });

      expect(readinessCalls).toBe(1);
      expect(screen.getByText(/common\.retry/i)).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('retries readiness loop on 503 and recovers without terminal error state', async () => {
    let readinessCalls = 0;
    const mockChannel = { id: 'ch-503', serviceRef: '1:0:1:...' };

    const response = (
      status: number,
      body: Record<string, unknown> = {},
      headers: Record<string, string> = {}
    ) => ({
      ok: status >= 200 && status < 300,
      status,
      url: 'http://localhost/api/v3/sessions/sess-503',
      headers: {
        get: (key: string) => headers[key] ?? headers[key.toLowerCase()] ?? null
      },
      json: async () => body,
      text: async () => JSON.stringify(body)
    });

    (globalThis.fetch as any).mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve(
          response(200, {
            mode: 'native_hls',
            requestId: 'live-decision-errors-503',
            playbackDecisionToken: 'live-token-errors-503',
            decision: { reasons: ['direct_stream_match'] }
          })
        );
      }

      if (url.includes('/intents')) {
        const parsed = init?.body ? JSON.parse(String(init.body)) : {};
        if (parsed?.type === 'stream.start') {
          return Promise.resolve(response(200, { sessionId: 'sess-503' }));
        }
        return Promise.resolve(response(200, {})); // stream.stop
      }

      if (url.includes('/sessions/sess-503') && !url.includes('/heartbeat')) {
        readinessCalls++;
        if (readinessCalls === 1) {
          return Promise.resolve(response(503, { detail: 'upstream_warming' }));
        }
        return Promise.resolve(
          response(200, {
            state: 'READY',
            playbackUrl: '/live.m3u8',
            heartbeat_interval: 1
          })
        );
      }

      return Promise.resolve(response(200, {}));
    });

    vi.useFakeTimers();
    try {
      render(<V3Player autoStart={true} channel={mockChannel as any} />);

      await act(async () => {
        await flushMicrotasks();
        await flushMicrotasks();
        await vi.advanceTimersByTimeAsync(0);
        await flushMicrotasks();
      });

      expect(readinessCalls).toBe(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(499);
        await flushMicrotasks();
      });
      expect(readinessCalls).toBe(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
        await flushMicrotasks();
        await flushMicrotasks();
      });

      expect(readinessCalls).toBe(2);
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      expect(screen.queryByText(/player\.sessionFailed/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/player\.sessionExpired/i)).not.toBeInTheDocument();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(3000);
        await flushMicrotasks();
      });
      expect(readinessCalls).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('tears down on 410 GONE (Session Expired) during heartbeat', async () => {
    let heartbeatCount = 0;

    (globalThis.fetch as any).mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            mode: 'native_hls',
            requestId: 'live-decision-errors-heartbeat',
            playbackDecisionToken: 'live-token-errors-heartbeat',
            decision: { reasons: ['direct_stream_match'] }
          })
        });
      }

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

    try {
      // Render with channel to trigger LIVE path which has heartbeats
      const mockChannel = { id: 'ch-1', serviceRef: '1:0:1:...' };
      render(<V3Player autoStart={true} channel={mockChannel as any} />);

      // Let the async autostart path progress far enough to create + poll the session.
      await act(async () => {
        await flushMicrotasks();
        await flushMicrotasks();
      });
      const calls = (globalThis.fetch as any).mock.calls.map((c: any[]) => String(c[0]));
      expect(calls.some((u: string) => u.includes('/sessions/sess-123') && !u.includes('/heartbeat'))).toBe(true);

      // Trigger first heartbeat (success) + flush the async interval callback.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1100);
        await flushMicrotasks();
      });

      // Trigger second heartbeat (410) + flush the async interval callback.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1100);
        await flushMicrotasks();
      });

      // With fake timers enabled, avoid waitFor here (it schedules timeouts).
      expect(screen.getByText(/player.sessionExpired/i)).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});
