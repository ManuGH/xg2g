/// <reference types="@testing-library/jest-dom" />
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../../../types/v3-player';

const { createSessionMock, postRecordingPlaybackInfoMock } = vi.hoisted(() => ({
  createSessionMock: vi.fn(),
  postRecordingPlaybackInfoMock: vi.fn(),
}));

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
    ERROR: 'hlsError'
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError', MEDIA_ERROR: 'mediaError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

vi.mock('../../../client-ts', () => ({
  createSession: createSessionMock,
  postRecordingPlaybackInfo: postRecordingPlaybackInfoMock,
}));

describe('V3Player ServiceRef Input', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    createSessionMock.mockReset();
    postRecordingPlaybackInfoMock.mockReset();
    createSessionMock.mockResolvedValue({
      data: {},
      response: { status: 200 }
    });
    originalFetch = globalThis.fetch;
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'direct_stream',
            requestId: 'live-decision-1',
            playbackDecisionToken: 'live-token-1',
            decision: { reasons: ['direct_stream_match'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          status: 503,
          ok: false,
          headers: { get: vi.fn().mockImplementation((name: string) => name === 'content-type' ? 'application/problem+json' : null) },
          json: vi.fn().mockResolvedValue({ type: '/problems/admission/state-unknown', title: 'Unavailable', status: 503, code: 'ADMISSION_STATE_UNKNOWN', requestId: 'test' })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });
  });

  afterEach(() => {
    (globalThis as any).fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('uses edited serviceRef when starting a live stream via Enter', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    const newRef = '1:0:1:1234:567:89AB:0:0:0:0:';
    fireEvent.change(input, { target: { value: newRef } });

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe(newRef);
    });

    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalled();
    });

    const intentCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/intents'));
    expect(intentCall).toBeDefined();
    const [url, options] = intentCall;
    expect(String(url)).toContain('/intents');
    const body = JSON.parse(options.body);
    expect(body.serviceRef).toBe(newRef);
    expect(body.playbackDecisionToken).toBe('live-token-1');
    expect(body.client?.capabilitiesVersion).toBe(3);
    expect(body.params?.playback_decision_token).toBeUndefined();
    expect(body.params?.playback_decision_id).toBeUndefined();
  });

  it('uses edited serviceRef when starting a live stream via Start button', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    const newRef = '1:0:1:9999:888:77AA:0:0:0:0:';
    fireEvent.change(input, { target: { value: newRef } });

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe(newRef);
    });

    const startButton = screen.getByRole('button', { name: /Start Stream/i });
    fireEvent.click(startButton);

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalled();
    });

    const intentCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/intents'));
    expect(intentCall).toBeDefined();
    const [url, options] = intentCall;
    expect(String(url)).toContain('/intents');
    const body = JSON.parse(options.body);
    expect(body.serviceRef).toBe(newRef);
    expect(body.playbackDecisionToken).toBe('live-token-1');
    expect(body.client?.capabilitiesVersion).toBe(3);
    expect(body.params?.playback_decision_token).toBeUndefined();
    expect(body.params?.playback_decision_id).toBeUndefined();
  });

  it('prefers native HLS for desktop Safari live playback when runtime capabilities prefer native', async () => {
    const maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    const webkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(
      HTMLVideoElement.prototype,
      'webkitSupportsPresentationMode'
    );

    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn()
    });

    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return '';
    });

    try {
      const props = { autoStart: false } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      const input = screen.getByRole('textbox');
      fireEvent.change(input, { target: { value: '1:0:1:7777:888:999:0:0:0:0:' } });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await waitFor(() => {
        expect(globalThis.fetch).toHaveBeenCalled();
      });

      const streamInfoCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/live/stream-info'));
      expect(streamInfoCall).toBeDefined();
      const [, streamInfoOptions] = streamInfoCall;
      const streamInfoBody = JSON.parse(streamInfoOptions.body);
      expect(streamInfoBody.capabilities?.capabilitiesVersion).toBe(3);
      expect(streamInfoBody.capabilities?.preferredHlsEngine).toBe('hlsjs');
      expect(streamInfoBody.capabilities?.videoCodecSignals).toEqual([
        { codec: 'av1', supported: false },
        { codec: 'hevc', supported: false },
        { codec: 'h264', supported: false },
      ]);

      const intentCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/intents'));
      expect(intentCall).toBeDefined();
      const [, options] = intentCall;
      const body = JSON.parse(options.body);
      expect(body.params?.playback_mode).toBe('hlsjs');
      expect(body.params?.codecs).toBe('h264');
    } finally {
      if (webkitSupportsPresentationModeDescriptor) {
        Object.defineProperty(
          HTMLVideoElement.prototype,
          'webkitSupportsPresentationMode',
          webkitSupportsPresentationModeDescriptor
        );
      } else {
        delete (HTMLVideoElement.prototype as any).webkitSupportsPresentationMode;
      }

      if (maxTouchPointsDescriptor) {
        Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
      }
    }
  });

  it('tears down the previous native live stream before starting the next one', async () => {
    const maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    const webkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(
      HTMLVideoElement.prototype,
      'webkitSupportsPresentationMode'
    );
    const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
    const playMock = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    const pauseMock = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});

    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn()
    });
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return originalCanPlayType.call(this, type);
    });

    let streamStartCount = 0;
    const response = (status: number, body: Record<string, unknown> = {}) => ({
      ok: status >= 200 && status < 300,
      status,
      headers: { get: vi.fn().mockReturnValue('application/json') },
      json: vi.fn().mockResolvedValue(body),
      text: vi.fn().mockResolvedValue(JSON.stringify(body))
    });

    (globalThis as any).fetch = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve(response(200, {
          mode: 'native_hls',
          requestId: `live-decision-${streamStartCount + 1}`,
          playbackDecisionToken: `live-token-${streamStartCount + 1}`,
          decision: { reasons: ['native_hls'] }
        }));
      }
      if (url.includes('/intents')) {
        const parsed = init?.body ? JSON.parse(String(init.body)) : {};
        if (parsed?.type === 'stream.start') {
          streamStartCount += 1;
          return Promise.resolve(response(200, { sessionId: `sid-live-${streamStartCount}` }));
        }
        return Promise.resolve(response(200, {}));
      }
      if (url.includes('/sessions/sid-live-1') && !url.includes('/heartbeat')) {
        return Promise.resolve(response(200, {
          id: 'sid-live-1',
          state: 'READY',
          mode: 'LIVE',
          playbackUrl: 'http://example.com/live-1.m3u8',
          heartbeatIntervalSeconds: 600
        }));
      }
      if (url.includes('/sessions/sid-live-2') && !url.includes('/heartbeat')) {
        return Promise.resolve(response(200, {
          id: 'sid-live-2',
          state: 'READY',
          mode: 'LIVE',
          playbackUrl: 'http://example.com/live-2.m3u8',
          heartbeatIntervalSeconds: 600
        }));
      }
      return Promise.resolve(response(200, {}));
    });

    try {
      const props = { autoStart: false } as unknown as V3PlayerProps;
      const { container } = render(<V3Player {...props} />);

      const input = screen.getByRole('textbox');
      const startButton = screen.getByRole('button', { name: /Start Stream/i });

      fireEvent.change(input, { target: { value: '1:0:1:1111:222:333:0:0:0:0:' } });
      fireEvent.click(startButton);

      await waitFor(() => {
        expect(
          (globalThis.fetch as any).mock.calls.some((call: any[]) => String(call[0]).includes('/sessions/sid-live-1'))
        ).toBe(true);
      });

      const pausesBeforeRestart = pauseMock.mock.calls.length;
      fireEvent.change(input, { target: { value: '1:0:1:4444:555:666:0:0:0:0:' } });
      fireEvent.click(startButton);

      await waitFor(() => {
        const stopCalls = (globalThis.fetch as any).mock.calls.filter((call: any[]) => {
          if (!String(call[0]).includes('/intents')) return false;
          const body = JSON.parse(String(call[1]?.body ?? '{}'));
          return body.type === 'stream.stop' && body.sessionId === 'sid-live-1';
        });
        expect(stopCalls.length).toBeGreaterThan(0);
      });
      await waitFor(() => {
        expect(
          (globalThis.fetch as any).mock.calls.some((call: any[]) => String(call[0]).includes('/sessions/sid-live-2'))
        ).toBe(true);
      });

      expect(pauseMock.mock.calls.length).toBeGreaterThan(pausesBeforeRestart);

      const video = container.querySelector('video') as HTMLVideoElement | null;
      expect(video).toBeTruthy();
      if (!video) return;

      fireEvent.loadedMetadata(video);
      expect(playMock).toHaveBeenCalledTimes(1);
    } finally {
      if (webkitSupportsPresentationModeDescriptor) {
        Object.defineProperty(
          HTMLVideoElement.prototype,
          'webkitSupportsPresentationMode',
          webkitSupportsPresentationModeDescriptor
        );
      } else {
        delete (HTMLVideoElement.prototype as any).webkitSupportsPresentationMode;
      }

      if (maxTouchPointsDescriptor) {
        Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
      }
    }
  });

  it('does not call live APIs when serviceRef is empty after trimming', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: '   ' } });

    const startButton = screen.getByRole('button', { name: /Start Stream/i });
    fireEvent.click(startButton);

    await screen.findByText(/Service Ref is required/i);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('passes the current API token into native playback requests on Android hosts', async () => {
    const originalHost = window.__XG2G_HOST__;
    const originalBridge = window.Xg2gHost;
    const startNativePlayback = vi.fn();

    window.__XG2G_HOST__ = {
      platform: 'android-tv',
      isTv: true,
      supportsKeepScreenAwake: true,
      supportsHostMediaKeys: true,
      supportsInputFocus: true,
      supportsNativePlayback: true,
    };
    window.Xg2gHost = {
      startNativePlayback,
      stopNativePlayback: vi.fn(),
      setPlaybackActive: vi.fn(),
      requestInputFocus: vi.fn(),
      getNativePlaybackStateJson: vi.fn().mockReturnValue('null'),
    };

    try {
      const props = { autoStart: false, token: 'dev-token' } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: '1:0:1:123:456:789:0:0:0:0:' }
      });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await waitFor(() => {
        expect(startNativePlayback).toHaveBeenCalledTimes(1);
      });

      const payload = startNativePlayback.mock.calls[0]?.[0];
      expect(payload).toBeDefined();
      expect(JSON.parse(String(payload))).toMatchObject({
        kind: 'live',
        serviceRef: '1:0:1:123:456:789:0:0:0:0:',
        authToken: 'dev-token'
      });
      expect(globalThis.fetch).not.toHaveBeenCalled();
    } finally {
      window.__XG2G_HOST__ = originalHost;
      window.Xg2gHost = originalBridge;
    }
  });

  it('dispatches auth-required when /intents returns 401', async () => {
    const authRequiredHandler = vi.fn();
    window.addEventListener('auth-required', authRequiredHandler);

    try {
      (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: vi.fn().mockReturnValue('application/json') },
            text: vi.fn().mockResolvedValue(JSON.stringify({
              mode: 'direct_stream',
              requestId: 'live-decision-auth-1',
              playbackDecisionToken: 'live-token-auth-1',
              decision: { reasons: ['direct_stream_match'] },
            }))
          });
        }
        if (url.includes('/intents')) {
          return Promise.resolve({
            status: 401,
            ok: false,
            headers: {
              get: vi.fn((name: string | null) => name === 'content-type' ? 'application/problem+json' : null)
            },
            json: vi.fn().mockResolvedValue({
              title: 'Authentication required',
              code: 'AUTH_REQUIRED',
              detail: 'Token expired',
              requestId: 'req-401-1',
            }),
          });
        }
        return Promise.resolve({
          status: 200,
          ok: true,
          headers: { get: vi.fn().mockReturnValue(null) },
          json: vi.fn().mockResolvedValue({})
        });
      });

      const props = { autoStart: false } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: '1:0:1:123:456:789:0:0:0:0:' }
      });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await waitFor(() => {
        expect(authRequiredHandler).toHaveBeenCalledTimes(1);
      });
      await screen.findByText(/Authentication required/i);
    } finally {
      window.removeEventListener('auth-required', authRequiredHandler);
    }
  });

  it('re-mints the session cookie when session readiness returns 401 after restart', async () => {
    const authRequiredHandler = vi.fn();
    window.addEventListener('auth-required', authRequiredHandler);

    let sessionPollCount = 0;
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-recover-1',
            playbackDecisionToken: 'live-token-recover-1',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-recover-1',
            requestId: 'intent-req-recover-1'
          })
        });
      }
      if (url.includes('/sessions/sid-live-recover-1') && !url.includes('/heartbeat')) {
        sessionPollCount += 1;
        if (sessionPollCount === 1) {
          return Promise.resolve({
            ok: false,
            status: 401,
            url: String(url),
            headers: { get: vi.fn().mockReturnValue('req-session-401') },
            json: vi.fn().mockResolvedValue({
              title: 'Authentication required',
              code: 'AUTH_REQUIRED',
              requestId: 'req-session-401'
            }),
            text: vi.fn().mockResolvedValue('{"title":"Authentication required"}')
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          url: String(url),
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-recover-1',
            state: 'READY',
            mode: 'LIVE',
            playbackUrl: 'http://example.com/live-recover.m3u8',
            heartbeatIntervalSeconds: 600
          })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    try {
      const props = { autoStart: false, token: 'dev-token' } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: '1:0:1:555:444:333:0:0:0:0:' }
      });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await waitFor(() => {
        expect(createSessionMock).toHaveBeenCalledTimes(2);
        expect(sessionPollCount).toBe(2);
      });

      expect(authRequiredHandler).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('auth-required', authRequiredHandler);
    }
  });

  it('shows session expired instead of auth failed when recovery succeeds but the session is gone', async () => {
    const authRequiredHandler = vi.fn();
    window.addEventListener('auth-required', authRequiredHandler);

    let sessionPollCount = 0;
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-expired-1',
            playbackDecisionToken: 'live-token-expired-1',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-expired-1',
            requestId: 'intent-req-expired-1'
          })
        });
      }
      if (url.includes('/sessions/sid-live-expired-1') && !url.includes('/heartbeat')) {
        sessionPollCount += 1;
        if (sessionPollCount === 1) {
          return Promise.resolve({
            ok: false,
            status: 401,
            url: String(url),
            headers: { get: vi.fn().mockReturnValue('req-session-expired-401') },
            json: vi.fn().mockResolvedValue({
              title: 'Authentication required',
              code: 'AUTH_REQUIRED',
              requestId: 'req-session-expired-401'
            }),
            text: vi.fn().mockResolvedValue('{"title":"Authentication required"}')
          });
        }
        return Promise.resolve({
          ok: false,
          status: 410,
          url: String(url),
          headers: { get: vi.fn().mockImplementation((name: string) => name === 'X-Request-ID' ? 'req-session-expired-410' : 'application/json') },
          json: vi.fn().mockResolvedValue({
            title: 'Session expired',
            status: 410,
            requestId: 'req-session-expired-410',
            state: 'FAILED',
            reason: 'SESSION_GONE',
            reason_detail: 'server restarted',
            session: 'sid-live-expired-1'
          }),
          text: vi.fn().mockResolvedValue(JSON.stringify({
            title: 'Session expired',
            status: 410,
            requestId: 'req-session-expired-410',
            state: 'FAILED',
            reason: 'SESSION_GONE',
            reason_detail: 'server restarted',
            session: 'sid-live-expired-1'
          }))
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    try {
      const props = { autoStart: false, token: 'dev-token' } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: '1:0:1:666:555:444:0:0:0:0:' }
      });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await waitFor(() => {
        expect(createSessionMock).toHaveBeenCalledTimes(2);
        expect(sessionPollCount).toBe(2);
      });

      await screen.findByText(/Session expired\. Please restart\./i);
      expect(screen.queryByText(/Authentication required/i)).not.toBeInTheDocument();
      expect(authRequiredHandler).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('auth-required', authRequiredHandler);
    }
  });

  it('keeps 403 intent failures local without dispatching auth-required', async () => {
    const authRequiredHandler = vi.fn();
    window.addEventListener('auth-required', authRequiredHandler);

    try {
      (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: { get: vi.fn().mockReturnValue('application/json') },
            text: vi.fn().mockResolvedValue(JSON.stringify({
              mode: 'direct_stream',
              requestId: 'live-decision-auth-2',
              playbackDecisionToken: 'live-token-auth-2',
              decision: { reasons: ['direct_stream_match'] },
            }))
          });
        }
        if (url.includes('/intents')) {
          return Promise.resolve({
            status: 403,
            ok: false,
            headers: {
              get: vi.fn((name: string | null) => name === 'content-type' ? 'application/problem+json' : null)
            },
            json: vi.fn().mockResolvedValue({
              code: 'FORBIDDEN',
              detail: 'Missing scope',
              requestId: 'req-403-1',
            }),
          });
        }
        return Promise.resolve({
          status: 200,
          ok: true,
          headers: { get: vi.fn().mockReturnValue(null) },
          json: vi.fn().mockResolvedValue({})
        });
      });

      const props = { autoStart: false } as unknown as V3PlayerProps;
      render(<V3Player {...props} />);

      fireEvent.change(screen.getByRole('textbox'), {
        target: { value: '1:0:1:123:456:789:0:0:0:0:' }
      });
      fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

      await screen.findByText(/Access denied/i);
      expect(authRequiredHandler).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('auth-required', authRequiredHandler);
    }
  });

  it('surfaces problem details from /intents 400 responses', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'direct_stream',
            requestId: 'live-decision-2',
            playbackDecisionToken: 'live-token-2',
            decision: { reasons: ['direct_stream_match'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          status: 400,
          ok: false,
          headers: { get: vi.fn().mockReturnValue('application/problem+json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            title: 'Invalid Request',
            code: 'INVALID_INPUT',
            detail: 'serviceRef is required',
            requestId: 'req-400-1',
          })),
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: '1:0:1:123:456:789:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await screen.findByText(/Invalid Request/i);
    fireEvent.click(screen.getByRole('button', { name: /Show Details/i }));
    await screen.findByText(/INVALID_INPUT/i);
    await screen.findByText(/req-400-1/i);
  });

  it('keeps live pause without user intent in paused state', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-3',
            playbackDecisionToken: 'live-token-3',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-1',
            requestId: 'intent-req-1'
          })
        });
      }
      if (url.includes('/sessions/sid-live-1')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-1',
            state: 'READY',
            mode: 'LIVE',
            playbackUrl: 'http://example.com/live.m3u8',
            heartbeatIntervalSeconds: 600
          })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    const props = { autoStart: false } as unknown as V3PlayerProps;
    const { container, unmount } = render(<V3Player {...props} />);

    fireEvent.click(screen.getByRole('button', { name: /Stats/i }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:777:666:55AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/ready/i);
    });

    const video = container.querySelector('video');
    expect(video).toBeTruthy();
    fireEvent.pause(video as HTMLVideoElement);

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/paused/i);
    });
    expect(screen.getByRole('status')).not.toHaveTextContent(/buffering/i);
    unmount();
  });

  it('keeps pause in native WebKit fullscreen instead of auto-resume buffering', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-4',
            playbackDecisionToken: 'live-token-4',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-2',
            requestId: 'intent-req-2'
          })
        });
      }
      if (url.includes('/sessions/sid-live-2')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-2',
            state: 'READY',
            mode: 'LIVE',
            playbackUrl: 'http://example.com/live2.m3u8',
            heartbeatIntervalSeconds: 600
          })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    const props = { autoStart: false } as unknown as V3PlayerProps;
    const { container, unmount } = render(<V3Player {...props} />);

    fireEvent.click(screen.getByRole('button', { name: /Stats/i }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:123:222:33AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/ready/i);
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    Object.defineProperty(video as HTMLVideoElement, 'webkitDisplayingFullscreen', {
      configurable: true,
      value: true
    });

    fireEvent.pause(video as HTMLVideoElement);

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/paused/i);
    });
    expect(screen.getByRole('status')).not.toHaveTextContent(/buffering/i);
    unmount();
  });

  it('recovers live playback state through waiting and playing events', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-5',
            playbackDecisionToken: 'live-token-5',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-3',
            requestId: 'intent-req-3'
          })
        });
      }
      if (url.includes('/sessions/sid-live-3')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-3',
            state: 'READY',
            mode: 'LIVE',
            playbackUrl: 'http://example.com/live3.m3u8',
            heartbeatIntervalSeconds: 600
          })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    const props = { autoStart: false } as unknown as V3PlayerProps;
    const { container, unmount } = render(<V3Player {...props} />);

    fireEvent.click(screen.getByRole('button', { name: /Stats/i }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:321:222:33AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/ready/i);
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();

    fireEvent.waiting(video as HTMLVideoElement);
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/buffering/i);
    });

    fireEvent.playing(video as HTMLVideoElement);
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/playing/i);
    });

    unmount();
  });

  it('recovers live playback state through stalled and playing events', async () => {
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hls',
            requestId: 'live-decision-6',
            playbackDecisionToken: 'live-token-6',
            decision: { reasons: ['hls'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            sessionId: 'sid-live-4',
            requestId: 'intent-req-4'
          })
        });
      }
      if (url.includes('/sessions/sid-live-4')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          json: vi.fn().mockResolvedValue({
            id: 'sid-live-4',
            state: 'READY',
            mode: 'LIVE',
            playbackUrl: 'http://example.com/live4.m3u8',
            heartbeatIntervalSeconds: 600
          })
        });
      }
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { get: vi.fn().mockReturnValue(null) },
        json: vi.fn().mockResolvedValue({})
      });
    });

    const props = { autoStart: false } as unknown as V3PlayerProps;
    const { container, unmount } = render(<V3Player {...props} />);

    fireEvent.click(screen.getByRole('button', { name: /Stats/i }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '1:0:1:654:222:33AA:0:0:0:0:' } });
    fireEvent.click(screen.getByRole('button', { name: /Start Stream/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/ready/i);
    });

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();

    fireEvent.stalled(video as HTMLVideoElement);
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/buffering/i);
    });

    fireEvent.playing(video as HTMLVideoElement);
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/playing/i);
    });

    unmount();
  });
});
