import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/components/V3Player';

vi.mock('../src/lib/hlsRuntime', () => {
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
});
