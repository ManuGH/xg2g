import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
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
            heartbeatIntervalSeconds: 1
          });
        }
        if (sessionStatusCalls === 2) {
          return jsonResponse(url, 200, { state: 'STARTING' });
        }
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeatIntervalSeconds: 1
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          sessionId: 'sess-hls-recovery',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
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
    const decodeWarningCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      if (!String(call[0]).includes('/sessions/sess-hls-recovery/feedback')) {
        return false;
      }
      try {
        const body = JSON.parse(String(call[1]?.body ?? '{}'));
        return body?.event === 'warning' && body?.code === 103;
      } catch {
        return false;
      }
    });
    expect(decodeWarningCall).toBeTruthy();
    expect(JSON.parse(String(decodeWarningCall[1].body))).toMatchObject({
      event: 'warning',
      code: 103,
      message: 'hlsjs_media_recovery',
    });

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

  it('nudges hls.js loading again after a non-fatal live stall', async () => {
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-hls-stall/hls/index.m3u8`;

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'hlsjs',
          requestId: 'live-decision-hls-stall',
          playbackDecisionToken: 'live-token-hls-stall',
          decision: { reasons: ['transcode_audio'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-hls-stall' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-hls-stall/feedback')) {
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-hls-stall') && !url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeatIntervalSeconds: 1
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          sessionId: 'sess-hls-stall',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    const { container } = render(<V3Player autoStart={true} channel={{ id: 'ch-hls-stall', serviceRef: '1:0:1:stall...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(hlsInstances).toHaveLength(1);
    const hls = hlsInstances[0];
    const video = container.querySelector('video') as HTMLVideoElement;
    expect(video).toBeTruthy();

    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => false,
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 2,
    });
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      writable: true,
      value: 12,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: 0,
        start: () => 0,
        end: () => 0,
      }),
    });

    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
    });

    await act(async () => {
      fireEvent.waiting(video);
      await vi.advanceTimersByTimeAsync(2400);
    });

    const warningCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      const url = String(call[0]);
      if (!url.includes('/sessions/sess-hls-stall/feedback')) {
        return false;
      }
      const body = call[1]?.body ? JSON.parse(String(call[1].body)) : null;
      return body?.event === 'warning';
    });
    expect(warningCall).toBeTruthy();
    expect(JSON.parse(String(warningCall[1].body))).toMatchObject({
      event: 'warning',
      code: 101,
      message: 'waiting',
    });

    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
    });

    const recoveryInfoCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      const url = String(call[0]);
      if (!url.includes('/sessions/sess-hls-stall/feedback')) {
        return false;
      }
      const body = call[1]?.body ? JSON.parse(String(call[1].body)) : null;
      return body?.event === 'info' && body?.code === 211;
    });
    expect(recoveryInfoCall).toBeTruthy();
    expect(JSON.parse(String(recoveryInfoCall[1].body))).toMatchObject({
      event: 'info',
      code: 211,
      message: 'recovered_buffering',
    });

    expect(hls.startLoad).toHaveBeenCalledTimes(1);
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled();
  });

  it('retries hls.js loading after a fatal network error and reports a network warning', async () => {
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-hls-network/hls/index.m3u8`;

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'hlsjs',
          requestId: 'live-decision-hls-network',
          playbackDecisionToken: 'live-token-hls-network',
          decision: { reasons: ['transcode_audio'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-hls-network' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-hls-network/feedback')) {
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-hls-network') && !url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeatIntervalSeconds: 1
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          sessionId: 'sess-hls-network',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    render(<V3Player autoStart={true} channel={{ id: 'ch-hls-network', serviceRef: '1:0:1:network...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(hlsInstances).toHaveLength(1);
    const hls = hlsInstances[0];
    const errorHandler = hls.handlers.get('hlsError');
    expect(errorHandler).toBeDefined();

    await act(async () => {
      fireEvent.playing(document.querySelector('video') as HTMLVideoElement);
      await Promise.resolve();
    });

    await act(async () => {
      errorHandler?.('hlsError', {
        fatal: true,
        type: 'networkError',
        details: 'manifestLoadError'
      });
      await Promise.resolve();
    });

    const networkWarningCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      if (!String(call[0]).includes('/sessions/sess-hls-network/feedback')) {
        return false;
      }
      try {
        const body = JSON.parse(String(call[1]?.body ?? '{}'));
        return body?.event === 'warning' && body?.code === 104;
      } catch {
        return false;
      }
    });
    expect(networkWarningCall).toBeTruthy();
    expect(JSON.parse(String(networkWarningCall[1].body))).toMatchObject({
      event: 'warning',
      code: 104,
      message: 'hlsjs_network_retry',
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
      await Promise.resolve();
    });

    await act(async () => {
      fireEvent.playing(document.querySelector('video') as HTMLVideoElement);
      await Promise.resolve();
    });

    const networkRecoveryInfoCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      if (!String(call[0]).includes('/sessions/sess-hls-network/feedback')) {
        return false;
      }
      try {
        const body = JSON.parse(String(call[1]?.body ?? '{}'));
        return body?.event === 'info' && body?.code === 212;
      } catch {
        return false;
      }
    });
    expect(networkRecoveryInfoCall).toBeTruthy();
    expect(JSON.parse(String(networkRecoveryInfoCall[1].body))).toMatchObject({
      event: 'info',
      code: 212,
      message: 'recovered_network',
    });

    expect(hls.startLoad).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
