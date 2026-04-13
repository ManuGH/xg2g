import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/features/player/components/V3Player';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(false);
  return { default: HlsMock };
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

describe('V3Player native Safari recovery', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => (
      contentType === 'application/vnd.apple.mpegurl' ? 'probably' : ''
    ));
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });

  it('reattaches the session after native video error code 4', async () => {
    let sessionStatusCalls = 0;
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-native-recovery/hls/index.m3u8`;

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'native_hls',
          requestId: 'live-decision-native-recovery',
          playbackDecisionToken: 'live-token-native-recovery',
          decision: { reasons: ['direct_stream_match'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-native-recovery' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-native-recovery/feedback')) {
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-native-recovery') && !url.includes('/heartbeat')) {
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
          sessionId: 'sess-native-recovery',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    const { container } = render(<V3Player autoStart={true} channel={{ id: 'ch-native-recovery', serviceRef: '1:0:1:native...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    (video as HTMLVideoElement).setAttribute('src', playbackUrl);
    Object.defineProperty(video as HTMLVideoElement, 'currentSrc', {
      configurable: true,
      get: () => playbackUrl,
    });

    Object.defineProperty(video as HTMLVideoElement, 'error', {
      configurable: true,
      value: { code: 4, message: 'native decode failure' },
    });

    await act(async () => {
      fireEvent.error(video as HTMLVideoElement);
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1200);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(sessionStatusCalls).toBeGreaterThanOrEqual(3);
    expect(
      (globalThis.fetch as any).mock.calls.some((call: any[]) => String(call[0]).includes('/sessions/sess-native-recovery/feedback'))
    ).toBe(true);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('reattaches the session after a persistent native stall', async () => {
    let sessionStatusCalls = 0;
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-native-stall/hls/index.m3u8`;

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'native_hls',
          requestId: 'live-decision-native-stall',
          playbackDecisionToken: 'live-token-native-stall',
          decision: { reasons: ['direct_stream_match'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-native-stall' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-native-stall/feedback')) {
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-native-stall') && !url.includes('/heartbeat')) {
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
          sessionId: 'sess-native-stall',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    const { container } = render(<V3Player autoStart={true} channel={{ id: 'ch-native-stall', serviceRef: '1:0:1:stall...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();

    Object.defineProperty(video as HTMLVideoElement, 'paused', {
      configurable: true,
      get: () => false,
    });
    Object.defineProperty(video as HTMLVideoElement, 'currentTime', {
      configurable: true,
      get: () => 12,
    });
    Object.defineProperty(video as HTMLVideoElement, 'readyState', {
      configurable: true,
      get: () => 2,
    });
    Object.defineProperty(video as HTMLVideoElement, 'buffered', {
      configurable: true,
      get: () => ({
        length: 0,
      }),
    });

    await act(async () => {
      fireEvent.stalled(video as HTMLVideoElement);
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3800);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(sessionStatusCalls).toBeGreaterThanOrEqual(3);
    expect(
      (globalThis.fetch as any).mock.calls.some((call: any[]) => String(call[0]).includes('/sessions/sess-native-stall/feedback'))
    ).toBe(true);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('treats native rebuffering as buffering without injecting a synthetic pause', async () => {
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-native-buffering/hls/index.m3u8`;
    const pauseSpy = vi.mocked(HTMLMediaElement.prototype.pause);
    const playSpy = vi.mocked(HTMLMediaElement.prototype.play);
    let currentTime = 1;
    let readyState = 4;
    const paused = false;
    let bufferedRanges = [{ start: 0, end: 4 }];

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'native_hls',
          requestId: 'live-decision-native-buffering',
          playbackDecisionToken: 'live-token-native-buffering',
          decision: { reasons: ['direct_stream_match'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-native-buffering' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-native-buffering') && !url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeatIntervalSeconds: 600
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          sessionId: 'sess-native-buffering',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      if (url.includes('/feedback')) {
        return jsonResponse(url, 202, {});
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    const { container } = render(<V3Player autoStart={true} channel={{ id: 'ch-native-buffering', serviceRef: '1:0:1:buffer...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      return;
    }

    Object.defineProperty(video, 'currentSrc', {
      configurable: true,
      get: () => playbackUrl,
    });
    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => paused,
    });
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: bufferedRanges.length,
        start: (index: number) => bufferedRanges[index].start,
        end: (index: number) => bufferedRanges[index].end,
      }),
    });

    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(800);
      await Promise.resolve();
    });

    const pausesBeforeRebuffer = pauseSpy.mock.calls.length;
    const playsBeforeRebuffer = playSpy.mock.calls.length;

    await act(async () => {
      currentTime = 8;
      readyState = 2;
      bufferedRanges = [];
      fireEvent.waiting(video);

      currentTime = 8.5;
      readyState = 4;
      bufferedRanges = [{ start: 8, end: 10 }];
      fireEvent.playing(video);
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(500);
      await Promise.resolve();
    });

    expect(pauseSpy.mock.calls.length).toBe(pausesBeforeRebuffer);
    expect(playSpy.mock.calls.length).toBe(playsBeforeRebuffer);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('does not reattach the session for a waiting-only native rebuffer', async () => {
    let sessionStatusCalls = 0;
    const playbackUrl = `${window.location.origin}/api/v3/sessions/sess-native-waiting/hls/index.m3u8`;
    const feedbackCalls: string[] = [];

    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return jsonResponse(url, 200, {
          mode: 'native_hls',
          requestId: 'live-decision-native-waiting',
          playbackDecisionToken: 'live-token-native-waiting',
          decision: { reasons: ['direct_stream_match'] }
        });
      }

      if (url.includes('/intents')) {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        if (body?.type === 'stream.start') {
          return jsonResponse(url, 200, { sessionId: 'sess-native-waiting' });
        }
        return jsonResponse(url, 200, {});
      }

      if (url.includes('/sessions/sess-native-waiting/feedback')) {
        feedbackCalls.push(url);
        return jsonResponse(url, 202, {});
      }

      if (url.includes('/sessions/sess-native-waiting') && !url.includes('/heartbeat')) {
        sessionStatusCalls++;
        return jsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeatIntervalSeconds: 600
        });
      }

      if (url.includes('/heartbeat')) {
        return jsonResponse(url, 200, {
          sessionId: 'sess-native-waiting',
          acknowledged: true,
          leaseExpiresAt: 'later'
        });
      }

      return jsonResponse(url, 200, {});
    }) as unknown as typeof globalThis.fetch);

    const { container } = render(<V3Player autoStart={true} channel={{ id: 'ch-native-waiting', serviceRef: '1:0:1:waiting...' } as any} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      return;
    }

    let currentTime = 8;
    const readyState = 2;
    const paused = false;
    const bufferedRanges: Array<{ start: number; end: number }> = [];

    Object.defineProperty(video, 'currentSrc', {
      configurable: true,
      get: () => playbackUrl,
    });
    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => paused,
    });
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: bufferedRanges.length,
        start: (index: number) => bufferedRanges[index].start,
        end: (index: number) => bufferedRanges[index].end,
      }),
    });

    await act(async () => {
      fireEvent.waiting(video);
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(3100);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(sessionStatusCalls).toBe(1);
    expect(feedbackCalls).toHaveLength(0);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
