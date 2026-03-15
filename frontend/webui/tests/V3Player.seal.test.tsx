import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeAll, beforeEach, afterAll } from 'vitest';
import { suppressExpectedConsoleNoise } from './helpers/consoleNoise';
import { findFetchCall, mockLiveFlowFetch } from './helpers/liveFlow';

// Mock fetch
const originalFetch = global.fetch;

describe('V3Player Truth Sealing (UI-INV-PLAYER-001)', () => {
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      error: [/HLS playback engine not available/i],
      warn: [/Failed to stop v3 session/i, /Failed to parse URL from \/api\/v3\/intents/i]
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => {
      if (contentType === 'application/vnd.apple.mpegurl') {
        return 'probably';
      }
      return '';
    });

    mockLiveFlowFetch({
      mode: 'native_hls',
      requestId: 'live-decision-seal-1',
      playbackDecisionToken: 'live-token-seal-1',
      sessionId: 'sess-live-seal-1',
      playbackUrl: '/live-seal.m3u8'
    });
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
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
    const hasLiveStreamInfoPost = fetchCalls.some((call: any) =>
      String(call[0]).includes('/live/stream-info') && call[1]?.method === 'POST'
    );
    const hasIntentsPost = fetchCalls.some((call: any) =>
      String(call[0]).includes('/intents') && call[1]?.method === 'POST'
    );

    expect(hasLiveStreamInfoPost).toBe(false);
    expect(hasIntentsPost).toBe(false);
  });

  it('resolution: uses channel truth for stream start', async () => {
    const mockChannel = {
      id: '1:0:1:ABCD',
      serviceRef: '1:0:1:ABCD',
      name: 'Test Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    await waitFor(() => expect(findFetchCall((global.fetch as any), '/live/stream-info')).toBeDefined());
    await waitFor(() => expect(findFetchCall((global.fetch as any), '/intents')).toBeDefined());

    const streamInfoCall = findFetchCall((global.fetch as any), '/live/stream-info');
    const streamInfoBody = JSON.parse(String(streamInfoCall?.[1]?.body ?? '{}'));
    expect(streamInfoBody.serviceRef).toBe('1:0:1:ABCD');

    const intentsCall = findFetchCall((global.fetch as any), '/intents');
    const intentsBody = JSON.parse(String(intentsCall?.[1]?.body ?? '{}'));
    expect(intentsBody.serviceRef).toBe('1:0:1:ABCD');
    expect(intentsBody.params.playback_mode).toBe('native_hls');
    expect(intentsBody.params.playback_decision_token).toBe('live-token-seal-1');
    expect(intentsBody.params.playback_decision_id).toBeUndefined();
  });

  it('preserves ac3 in live capability probes when the browser reports support', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => {
      if (contentType === 'application/vnd.apple.mpegurl') {
        return 'probably';
      }
      if (contentType === 'audio/mp4; codecs="ac-3"') {
        return 'probably';
      }
      return '';
    });

    const mockChannel = {
      id: '1:0:1:AC3LIVE',
      serviceRef: '1:0:1:AC3LIVE',
      name: 'AC3 Test Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    await waitFor(() => expect(findFetchCall((global.fetch as any), '/live/stream-info')).toBeDefined());

    const streamInfoCall = findFetchCall((global.fetch as any), '/live/stream-info');
    const streamInfoBody = JSON.parse(String(streamInfoCall?.[1]?.body ?? '{}'));
    expect(streamInfoBody.capabilities.capabilitiesVersion).toBe(2);
    expect(streamInfoBody.capabilities.audioCodecs).toContain('ac3');
    expect(streamInfoBody.capabilities.hlsEngines).toContain('native');
    expect(streamInfoBody.capabilities.preferredHlsEngine).toBe('native');
  });

  it('surfaces session trace telemetry in the live stats overlay', async () => {
    mockLiveFlowFetch({
      mode: 'native_hls',
      requestId: 'live-telemetry-1',
      playbackDecisionToken: 'live-token-telemetry-1',
      sessionId: 'sess-live-telemetry-1',
      playbackUrl: '/live-telemetry.m3u8',
      sessionTrace: {
        requestId: 'live-telemetry-1-session',
        sessionId: 'sess-live-telemetry-1',
        source: {
          container: 'mpegts',
          videoCodec: 'h264',
          audioCodec: 'aac',
          width: 1920,
          height: 1080,
          fps: 25,
          audioChannels: 2,
          audioBitrateKbps: 256,
        },
        clientPath: 'hlsjs',
        requestProfile: 'compatible',
        requestedIntent: 'quality',
        resolvedIntent: 'compatible',
        qualityRung: 'compatible_audio_aac_256_stereo',
        degradedFrom: 'quality',
        inputKind: 'tuner',
        targetProfileHash: 'trace-live-hash-1',
        targetProfile: {
          container: 'mpegts',
          packaging: 'ts',
          hwAccel: 'none',
          video: { mode: 'copy', codec: 'h264', width: 1920, height: 1080, fps: 25 },
          audio: { mode: 'transcode', codec: 'aac', channels: 2, bitrateKbps: 256, sampleRate: 48000 },
          hls: { enabled: true, segmentContainer: 'mpegts', segmentSeconds: 6 }
        },
        ffmpegPlan: {
          inputKind: 'tuner',
          container: 'mpegts',
          packaging: 'ts',
          hwAccel: 'none',
          videoMode: 'copy',
          videoCodec: 'h264',
          audioMode: 'transcode',
          audioCodec: 'aac'
        },
        firstFrameAtMs: 1700000000000,
        fallbackCount: 1,
        lastFallbackReason: 'client_report:code=3'
      }
    });

    const mockChannel = {
      id: '1:0:1:TRACE',
      serviceRef: '1:0:1:TRACE',
      name: 'Trace Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    await waitFor(() => expect(findFetchCall((global.fetch as any), '/sessions/sess-live-telemetry-1')).toBeDefined());

    const statsButton = screen.getAllByRole('button').find((button) =>
      button.textContent?.includes('📊')
    );
    expect(statsButton).toBeDefined();
    fireEvent.click(statsButton!);

    expect(await screen.findByText(/mpegts · h264 · 1920x1080 · 25fps · a:aac\/2ch\/@256k/i)).toBeInTheDocument();
    expect(screen.getAllByText('quality').length).toBeGreaterThan(0);
    expect(screen.getAllByText('compatible').length).toBeGreaterThan(0);
    expect(screen.getByText(/compatible audio aac 256 stereo/i)).toBeInTheDocument();
    expect(screen.getByText(/tuner · ts · v:copy\/h264 · a:transcode\/aac · none/i)).toBeInTheDocument();
    expect(screen.getByText('trace-live-hash-1')).toBeInTheDocument();
    expect(screen.getByText('1 · client_report:code=3')).toBeInTheDocument();
  });

  it('surfaces terminal stop telemetry in the player error toast', async () => {
    const liveInfoBody = {
      mode: 'native_hls',
      requestId: 'live-stop-1',
      playbackDecisionToken: 'live-stop-token-1',
      decision: { reasons: ['direct_stream_match'] },
    };
    const intentBody = {
      sessionId: 'sess-live-stop-1',
      requestId: 'live-stop-1-intent',
    };
    const terminalBody = {
      type: 'urn:xg2g:error:session:gone',
      title: 'Session Gone',
      status: 410,
      code: 'session_gone',
      requestId: 'live-stop-1-session',
      session: 'sess-live-stop-1',
      state: 'FAILED',
      reason: 'R_PACKAGER_FAILED',
      reason_detail: 'playlist not ready timeout',
      trace: {
        requestId: 'live-stop-1-session',
        sessionId: 'sess-live-stop-1',
        stopClass: 'packager',
        stopReason: 'R_PACKAGER_FAILED',
        fallbackCount: 1,
        lastFallbackReason: 'client_report:code=3',
        ffmpegPlan: {
          inputKind: 'tuner',
          packaging: 'fmp4',
          videoMode: 'copy',
          videoCodec: 'h264',
          audioMode: 'transcode',
          audioCodec: 'aac',
          hwAccel: 'none'
        }
      }
    };

    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);

      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          url,
          headers: { get: () => 'application/json' },
          json: async () => liveInfoBody,
          text: async () => JSON.stringify(liveInfoBody),
        });
      }

      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          url,
          headers: { get: () => 'application/json' },
          json: async () => intentBody,
          text: async () => JSON.stringify(intentBody),
        });
      }

      if (url.includes('/sessions/sess-live-stop-1') && !url.includes('/heartbeat') && !url.includes('/feedback')) {
        return Promise.resolve({
          ok: false,
          status: 410,
          url,
          headers: {
            get: (name: string) => {
              if (name.toLowerCase() === 'content-type') return 'application/problem+json';
              if (name.toLowerCase() === 'x-request-id') return 'live-stop-1-session';
              return null;
            }
          },
          json: async () => terminalBody,
          text: async () => JSON.stringify(terminalBody),
        });
      }

      if (url.includes('/heartbeat')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          url,
          headers: { get: () => 'application/json' },
          json: async () => ({ lease_expires_at: 'next' }),
          text: async () => '',
        });
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        url,
        headers: { get: () => 'application/json' },
        json: async () => ({}),
        text: async () => '',
      });
    });

    vi.stubGlobal('fetch', fetchMock as unknown as typeof globalThis.fetch);

    const mockChannel = {
      id: '1:0:1:STOP',
      serviceRef: '1:0:1:STOP',
      name: 'Stop Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('packager');
    expect(alert.textContent).toContain('R_PACKAGER_FAILED');
    expect(alert.textContent).toContain('client_report:code=3');
  });
});
