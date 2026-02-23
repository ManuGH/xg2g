import React from 'react';
import { render, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock fetch
const originalFetch = global.fetch;

describe('V3Player Truth Sealing (UI-INV-PLAYER-001)', () => {
  beforeEach(() => {
    const response = (status: number, body: Record<string, unknown> = {}) => ({
      ok: status >= 200 && status < 300,
      status,
      headers: { get: () => null },
      json: async () => body,
      text: async () => JSON.stringify(body)
    });

    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve(
          response(200, {
            mode: 'hlsjs',
            requestId: 'req-live-seal',
            playbackDecisionToken: 'tok-live-seal',
            decision: { selectedOutputUrl: '/live.m3u8' }
          })
        );
      }

      if (url.includes('/intents')) {
        const parsed = init?.body ? JSON.parse(String(init.body)) : {};
        if (parsed?.type === 'stream.start') {
          return Promise.resolve(response(202, { sessionId: '123' }));
        }
        return Promise.resolve(response(200, {}));
      }

      if (url.includes('/sessions/123')) {
        return Promise.resolve(
          response(410, {
            reason: 'SESSION_GONE',
            reason_detail: 'test_stop',
            requestId: 'req-session-seal'
          })
        );
      }

      return Promise.resolve(response(200, {}));
    }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    global.fetch = originalFetch;
  });

  it('gating: does not auto-start if no explicit source is provided', async () => {
    // Render with autostart but NO channel/src/recordingId
    render(<V3Player autoStart={true} />);

    // Deterministic Verification: Flush microtasks to settle effects
    await Promise.resolve();
    await Promise.resolve();

    const fetchCalls = (global.fetch as any).mock.calls;
    const hasIntentsPost = fetchCalls.some((call: any) =>
      call[0].includes('/intents') && call[1]?.method === 'POST'
    );

    expect(hasIntentsPost).toBe(false);
  });

  it('resolution: uses channel truth for stream start', async () => {
    const mockChannel = {
      id: '1:0:1:ABCD',
      serviceRef: '1:0:1:ABCD',
      name: 'Test Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    await waitFor(() => {
      const calls = (global.fetch as any).mock.calls;
      const streamInfoCall = calls.find((call: any[]) => String(call[0]).includes('/live/stream-info'));
      const intentsCall = calls.find((call: any[]) => String(call[0]).includes('/intents'));

      expect(streamInfoCall).toBeDefined();
      expect(streamInfoCall[1]?.method).toBe('POST');
      expect(String(streamInfoCall[1]?.body)).toContain('1:0:1:ABCD');

      expect(intentsCall).toBeDefined();
      expect(intentsCall[1]?.method).toBe('POST');
      expect(String(intentsCall[1]?.body)).toContain('1:0:1:ABCD');
      expect(String(intentsCall[1]?.body)).toContain('playback_decision_token');
    });
  });
});
