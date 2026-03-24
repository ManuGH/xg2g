import React from 'react';
import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/features/player/components/V3Player';

type HlsHandler = (event: string, data: any) => void;
type HlsInstance = {
  handlers: Map<string, HlsHandler>;
  destroy: ReturnType<typeof vi.fn>;
  recoverMediaError: ReturnType<typeof vi.fn>;
  loadSource: ReturnType<typeof vi.fn>;
  attachMedia: ReturnType<typeof vi.fn>;
  startLoad: ReturnType<typeof vi.fn>;
  currentLevel: number;
  levels: any[];
};

const hlsInstances: HlsInstance[] = [];

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: HlsInstance) {
    this.handlers = new Map<string, HlsHandler>();
    this.destroy = vi.fn();
    this.recoverMediaError = vi.fn();
    this.loadSource = vi.fn();
    this.attachMedia = vi.fn();
    this.startLoad = vi.fn();
    this.currentLevel = -1;
    this.levels = [];
    this.on = vi.fn((event: string, handler: HlsHandler) => {
      this.handlers.set(event, handler);
    });
    hlsInstances.push(this);
    return this;
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    LEVEL_LOADED: 'hlsLevelLoaded',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError',
  };
  (HlsMock as any).ErrorTypes = {
    NETWORK_ERROR: 'networkError',
    MEDIA_ERROR: 'mediaError',
  };

  return { default: HlsMock };
});

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    postLivePlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

function jsonResponse(
  url: string,
  status: number,
  body: Record<string, unknown> = {},
  headers: Record<string, string> = {}
) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    url,
    headers: {
      get: (key: string) => headers[key] ?? headers[key.toLowerCase()] ?? null,
    },
    json: async () => body,
    text: async () => JSON.stringify(body),
  });
}

describe('V3Player hls.js decode recovery', () => {
  beforeEach(() => {
    hlsInstances.length = 0;
    vi.useFakeTimers();
    vi.clearAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });

  it('reattaches the same session after a second fatal hls.js media error', async () => {
    let sessionStatusCalls = 0;
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-hls-recovery/hls/index.m3u8`;

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'hlsjs',
          requestId: 'live-decision-hls-recovery',
          playbackDecisionToken: 'live-token-hls-recovery',
          decision: { reasons: ['direct_stream_match'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-hls-recovery' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-hls-recovery/feedback')) {
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-hls-recovery') && !url.includes('/heartbeat')) {
        sessionStatusCalls++;
        if (sessionStatusCalls === 1) {
          return jsonResponse(url, 200, {
            state: 'READY',
            playbackUrl,
            heartbeat_interval: 1
          });
        }
        if (sessionStatusCalls === 2) {
          return jsonResponse(url, 200, { state: 'STARTING' });
        }
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeat_interval: 1
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, { lease_expires_at: 'later' });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    render(<V3Player autoStart={true} channel={{ id: 'ch-hls-recovery', serviceRef: '1:0:1:recover...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(hlsInstances).toHaveLength(1);
    const firstInstance = hlsInstances[0];
    const errorHandler = firstInstance.handlers.get('hlsError');
    expect(errorHandler).toBeDefined();

    await act(async () => {
      errorHandler?.('hlsError', {
        fatal: true,
        type: 'mediaError',
        details: 'bufferAppendError'
      });
      await Promise.resolve();
    });

    expect(firstInstance.recoverMediaError).toHaveBeenCalledTimes(1);
    expect(
      (globalThis.fetch as any).mock.calls.some((call: any[]) => String(call[0]).includes('/sessions/sess-hls-recovery/feedback'))
    ).toBe(true);

    await act(async () => {
      errorHandler?.('hlsError', {
        fatal: true,
        type: 'mediaError',
        details: 'bufferAppendError'
      });
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1200);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(sessionStatusCalls).toBeGreaterThanOrEqual(3);
    expect(hlsInstances.length).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText(/media recovery failed/i)).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
