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
    const warningCall = (globalThis.fetch as any).mock.calls.find((call: any[]) => {
      const url = String(call[0]);
      if (!url.includes('/sessions/sess-native-stall/feedback')) {
        return false;
      }
      const body = call[1]?.body ? JSON.parse(String(call[1].body)) : null;
      return body?.event === 'warning';
    });
    expect(warningCall).toBeTruthy();
    expect(JSON.parse(String(warningCall[1].body))).toMatchObject({
      event: 'warning',
      code: 102,
      message: 'stalled',
    });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps native playback running during veil-based rebuffer recovery', async () => {
    let paused = false;
    let currentTime = 12;
    let readyState = 4;
    let bufferedLength = 1;
    let bufferedEnd = 14;
    const playbackUrl = 'http://example.com/live-native-veil.m3u8';

    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(async function () {
      paused = false;
    });
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {
      paused = true;
    });

    render(<V3Player autoStart={true} src={playbackUrl} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = document.querySelector('video') as HTMLVideoElement | null;
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
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: bufferedLength,
        start: () => 0,
        end: () => bufferedEnd,
      }),
    });

    await act(async () => {
      fireEvent.loadedMetadata(video);
      await Promise.resolve();
    });
    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
    });

    readyState = 2;
    bufferedLength = 0;

    await act(async () => {
      fireEvent.waiting(video);
      await Promise.resolve();
    });

    currentTime = 12.5;
    readyState = 4;
    bufferedLength = 1;
    bufferedEnd = 16;

    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(playSpy).toHaveBeenCalled();
    expect(pauseSpy).not.toHaveBeenCalled();
    expect(paused).toBe(false);
  });

  it('drops the startup overlay once native playback is visibly renderable', async () => {
    let paused = false;
    let currentTime = 0;
    let readyState = 4;
    let bufferedLength = 1;
    let bufferedEnd = 1.5;
    let videoWidth = 1280;
    let videoHeight = 720;
    let decodedFrameCount = 8;
    const playbackUrl = 'http://example.com/live-native-startup-veil.m3u8';

    render(<V3Player autoStart={true} src={playbackUrl} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = document.querySelector('video') as HTMLVideoElement | null;
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
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: bufferedLength,
        start: () => 0,
        end: () => bufferedEnd,
      }),
    });
    Object.defineProperty(video, 'videoWidth', {
      configurable: true,
      get: () => videoWidth,
    });
    Object.defineProperty(video, 'videoHeight', {
      configurable: true,
      get: () => videoHeight,
    });
    Object.defineProperty(video, 'webkitDecodedFrameCount', {
      configurable: true,
      get: () => decodedFrameCount,
    });

    await act(async () => {
      fireEvent.loadedMetadata(video);
      await Promise.resolve();
    });

    await act(async () => {
      fireEvent.playing(video);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(document.querySelector('[aria-live="polite"]')).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(video.className).not.toContain('videoElementHidden');
    expect(video.muted).toBe(false);
  });

  it('reveals native video again when buffering media becomes renderable before another playing event', async () => {
    let paused = false;
    let currentTime = 12;
    let readyState = 2;
    let bufferedLength = 0;
    let bufferedEnd = 12;
    let videoWidth = 0;
    let videoHeight = 0;
    let decodedFrameCount = 0;
    const playbackUrl = 'http://example.com/live-native-buffering-reveal.m3u8';

    render(<V3Player autoStart={true} src={playbackUrl} />);

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(0);
      await Promise.resolve();
      await Promise.resolve();
    });

    const video = document.querySelector('video') as HTMLVideoElement | null;
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
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'buffered', {
      configurable: true,
      get: () => ({
        length: bufferedLength,
        start: () => 0,
        end: () => bufferedEnd,
      }),
    });
    Object.defineProperty(video, 'videoWidth', {
      configurable: true,
      get: () => videoWidth,
    });
    Object.defineProperty(video, 'videoHeight', {
      configurable: true,
      get: () => videoHeight,
    });
    Object.defineProperty(video, 'webkitDecodedFrameCount', {
      configurable: true,
      get: () => decodedFrameCount,
    });

    await act(async () => {
      fireEvent.loadedMetadata(video);
      fireEvent.waiting(video);
      await Promise.resolve();
      await Promise.resolve();
    });

    readyState = 4;
    bufferedLength = 1;
    bufferedEnd = 18;
    videoWidth = 1280;
    videoHeight = 720;
    decodedFrameCount = 6;
    currentTime = 12.24;

    await act(async () => {
      fireEvent.canPlay(video);
      await Promise.resolve();
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(document.querySelector('[aria-live="polite"]')).toBeNull();
    expect(video.className).not.toContain('videoElementHidden');
    expect(video.muted).toBe(false);
  });
});
