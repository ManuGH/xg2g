import React from 'react';
import { render, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import V3Player from '../../src/components/V3Player';

describe('V3Player Intent Keys Contract', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => 'application/json' },
          text: async () => JSON.stringify({
            mode: 'hlsjs',
            requestId: 'req-intent-keys-1',
            playbackDecisionToken: 'token-intent-keys-1',
            decision: { reasons: ['direct_stream_match'] },
          })
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          json: async () => ({ sessionId: 'sess-intent-keys-1' })
        });
      }
      if (url.includes('/sessions/sess-intent-keys-1') && !url.includes('/heartbeat')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          json: async () => ({ state: 'READY', playbackUrl: '/live.m3u8', heartbeat_interval: 1 })
        });
      }
      if (url.includes('/heartbeat')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          json: async () => ({ lease_expires_at: 'next' })
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: async () => ({})
      });
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('sends canonical playback_decision_token and not deprecated playback_decision_id', async () => {
    render(<V3Player autoStart={true} channel={{ id: 'ch-keys-1', serviceRef: '1:0:1:AA:BB:CC:0:0:0:0:' } as any} />);

    await waitFor(() => {
      expect((globalThis.fetch as any).mock.calls.some((c: any[]) => String(c[0]).includes('/intents'))).toBe(true);
    });

    const intentCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/intents'));
    expect(intentCall).toBeDefined();

    const body = JSON.parse(String(intentCall[1]?.body ?? '{}'));
    expect(body.params.playback_decision_token).toBe('token-intent-keys-1');
    expect(body.params.playback_decision_id).toBeUndefined();
  });
});
