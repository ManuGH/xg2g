import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi, afterEach } from 'vitest';
import V3Player from '../../src/components/V3Player';
import * as sdk from '../../src/client-ts/sdk.gen';

vi.mock('../../src/client-ts/sdk.gen', async () => {
  return {
    createSession: vi.fn(),
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player RFC7807 Contract', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
    (sdk.createSession as any).mockResolvedValue({
      data: {},
      response: { status: 200, headers: new Map() }
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('renders live error from RFC7807 problem fields (code/title/type)', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: false,
          status: 503,
          headers: {
            get: (key: string) => {
              const k = key.toLowerCase();
              if (k === 'content-type') return 'application/problem+json';
              if (k === 'x-request-id') return 'req-problem-1';
              return null;
            }
          },
          json: async () => ({
            type: '/problems/playback/denied',
            title: 'Backend denied playback',
            status: 503,
            requestId: 'req-problem-1',
            code: 'PLAYBACK_DENIED',
            detail: 'no_compatible_playback_path'
          })
        });
      }

      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          json: async () => ({ sessionId: 'should-not-start' })
        });
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        headers: { get: () => null },
        json: async () => ({})
      });
    });

    render(<V3Player autoStart={true} channel={{ id: 'ch-problem-1', serviceRef: '1:0:1:AA:BB:CC:0:0:0:0:' } as any} />);

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toMatch(/PLAYBACK_DENIED/i);
      expect(alert.textContent).toMatch(/Backend denied playback/i);
      expect(alert.textContent).toMatch(/\/problems\/playback\/denied/i);
    });

    const calls = (globalThis.fetch as any).mock.calls.map((c: any[]) => String(c[0]));
    expect(calls.some((url: string) => url.includes('/intents'))).toBe(false);
  });
});
