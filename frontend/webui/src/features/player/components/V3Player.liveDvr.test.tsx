/// <reference types="@testing-library/jest-dom" />
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../../../types/v3-player';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
      startLoad: vi.fn(),
      currentLevel: -1,
      levels: [],
    };
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    LEVEL_LOADED: 'hlsLevelLoaded',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError',
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError', MEDIA_ERROR: 'mediaError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

describe('V3Player live DVR semantics', () => {
  let originalFetch: typeof globalThis.fetch;
  let maxTouchPointsDescriptor: PropertyDescriptor | undefined;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;
  const webkitEnterFullscreen = vi.fn();

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    vi.clearAllMocks();
    maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen,
    });
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return '';
    });

    (globalThis as any).fetch = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : input.url;
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-dvr-1',
            playbackDecisionToken: 'live-token-dvr-1',
            decision: { reasons: ['hls'] },
          })),
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-dvr-1',
            requestId: 'intent-live-dvr-1',
          }),
        });
      }
      if (url.includes('/sessions/sid-live-dvr-1') && !url.includes('/heartbeat') && !url.includes('/feedback')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-dvr-1',
            state: 'READY',
            mode: 'LIVE',
            windowKind: 'live-dvr',
            playbackUrl: 'http://example.com/live-dvr.m3u8',
            heartbeatIntervalSeconds: 600,
            durationSeconds: 30,
            seekableStartSeconds: 90,
            seekableEndSeconds: 120,
            liveEdgeSeconds: 120,
          }),
        });
      }
      if (url.includes('/services/now-next')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            items: [{
              serviceRef: '1:0:1:777:666:55AA:0:0:0:0:',
              now: {
                title: 'Wetter',
                start: 100,
                end: 160,
              },
            }],
          }),
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({}),
      });
    });
  });

  afterEach(() => {
    (globalThis as any).fetch = originalFetch;
    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }
    if (maxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
    }
    vi.restoreAllMocks();
  });

  it('marks a seekable live session as live-dvr while staying in the LIVE path', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    const { container, unmount } = render(<V3Player {...props} />);

    fireEvent.click(screen.getByRole('button', { name: /Stats/i }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:777:666:55AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/ready/i);
      const root = container.querySelector('[data-xg2g-player-root="true"]');
      expect(root).toHaveAttribute('data-playback-window', 'live-dvr');
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();

    let currentTime = 120;
    Object.defineProperty(video as HTMLVideoElement, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });

    fireEvent.loadedMetadata(video as HTMLVideoElement);
    fireEvent.timeUpdate(video as HTMLVideoElement);

    await waitFor(() => {
      expect(screen.getByText(/Live with DVR window/i)).toBeInTheDocument();
      expect(screen.getByText(/At live edge/i)).toBeInTheDocument();
    });

    unmount();
  });

  it('shows fullscreen guidance instead of free inline DVR scrubbing on touch devices', async () => {
    const props = { autoStart: false, onClose: vi.fn() } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:777:666:55AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    const slider = await screen.findByRole('slider', {
      name: /fullscreen on iphone for dvr scrubbing|vollbild wechseln/i,
    });
    expect(slider).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByText(/vollbild wechseln|fullscreen on iphone/i)).toBeInTheDocument();
  });

});
