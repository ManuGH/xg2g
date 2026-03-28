import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/features/player/components/V3Player';
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach, afterAll } from 'vitest';
import * as sdk from '../src/client-ts';
import { suppressExpectedConsoleNoise } from './helpers/consoleNoise';

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    postRecordingPlaybackInfo: vi.fn(),
  };
});

describe('V3Player Contract Consumption (UI-CON-PLAYER-001)', () => {
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      // jsdom test environment does not provide a usable HLS playback engine.
      error: [/HLS playback engine not available/i]
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
  });

  it('fails loudly if decision exists but selectedOutputUrl is missing (governance violation)', async () => {
    // Mock a response that has forbidden 'outputs' but missing 'selectedOutputUrl'
    const mockInfo: any = {
      mode: 'hlsjs',
      decision: {
        mode: 'direct_play',
        outputs: [{ kind: 'file', url: '/forbidden/path.mp4' }]
        // missing selectedOutputUrl
      },
      requestId: 'req-bad-contract'
    };

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    render(<V3Player autoStart={true} recordingId="rec-1" />);

    await waitFor(async () => {
      // Backend must fail closed if selected output URL is missing.
      const errorToast = await screen.findByRole('alert');
      expect(errorToast).toHaveTextContent(/Backend decision missing selectedOutputUrl|player\.serverError|Server error/i);
    });
  });

  it('prefers normative selectedOutputUrl over legacy url', async () => {
    const mockInfo: any = {
      url: '/legacy/url.m3u8',
      mode: 'transcode',
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/normative/url.m3u8',
        selectedOutputKind: 'hls'
      },
      requestId: 'req-good-contract'
    };

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    // Mock fetch to capture which URL is probed
    const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Map(),
      json: async () => ({})
    } as any);

    render(<V3Player autoStart={true} recordingId="rec-2" />);

    await waitFor(() => {
      // Ensure the normative URL was the one fetched
      const intentsCall = fetchSpy.mock.calls.find((call: any[]) => call[0].toString().includes('/normative/url.m3u8'));
      expect(intentsCall).toBeDefined();

      const legacyCall = fetchSpy.mock.calls.find((call: any[]) => call[0].toString().includes('/legacy/url.m3u8'));
      expect(legacyCall).toBeUndefined();
    });
  });

  it('does not advertise ac3 for recording playback capability probes', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => {
      if (contentType === 'application/vnd.apple.mpegurl') {
        return 'probably';
      }
      if (contentType === 'audio/mp4; codecs="ac-3"') {
        return 'probably';
      }
      return '';
    });

    const mockInfo: any = {
      mode: 'transcode',
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/recordings/rec-audio/index.m3u8',
        selectedOutputKind: 'hls'
      },
      requestId: 'req-recording-audio-contract'
    };

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Map(),
      json: async () => ({})
    } as any);

    render(<V3Player autoStart={true} recordingId="rec-audio" />);

    await waitFor(() => {
      expect(sdk.postRecordingPlaybackInfo as any).toHaveBeenCalled();
    });

    const request = (sdk.postRecordingPlaybackInfo as any).mock.calls[0]?.[0];
    expect(request?.body?.capabilitiesVersion).toBe(3);
    expect(request?.body?.audioCodecs).toEqual(['aac', 'mp3']);
    expect(request?.body?.hlsEngines).toContain('native');
    expect(request?.body?.preferredHlsEngine).toBe('native');
    expect(request?.body?.runtimeProbeUsed).toBe(true);
    expect(request?.body?.runtimeProbeVersion).toBe(2);
    expect(request?.body?.clientFamilyFallback).toBe('safari_native');

    await waitFor(() => {
      const playlistProbe = fetchSpy.mock.calls.find((call: any[]) =>
        call[0].toString().includes('/recordings/rec-audio/index.m3u8')
      );
      expect(playlistProbe).toBeDefined();
    });
  });

  it('surfaces target profile observability in the stats overlay', async () => {
    const mockInfo: any = {
      mode: 'transcode',
      requestId: 'req-observe-1',
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/recordings/rec-observe/index.m3u8?profile=compatible',
        selectedOutputKind: 'hls',
        targetProfileHash: 'hash-observe-1',
        targetProfile: {
          container: 'mpegts',
          packaging: 'ts',
          hwAccel: 'none',
          video: { mode: 'transcode', codec: 'h264', crf: 23, preset: 'fast', width: 0, height: 0, fps: 0 },
          audio: { mode: 'transcode', codec: 'aac', channels: 2, bitrateKbps: 256, sampleRate: 48000 },
          hls: { enabled: true, segmentContainer: 'mpegts', segmentSeconds: 6 }
        },
        trace: {
          requestId: 'req-observe-1',
          requestProfile: 'compatible',
          requestedIntent: 'quality',
          resolvedIntent: 'compatible',
          qualityRung: 'compatible_video_h264_crf23_fast',
          audioQualityRung: 'compatible_audio_aac_256_stereo',
          videoQualityRung: 'compatible_video_h264_crf23_fast',
          degradedFrom: 'quality',
          hostPressureBand: 'constrained',
          hostOverrideApplied: true,
          targetProfileHash: 'hash-observe-1',
          operator: {
            forcedIntent: 'repair',
            maxQualityRung: 'repair_audio_aac_192_stereo',
            ruleName: 'problem-recording-source',
            ruleScope: 'recording',
            clientFallbackDisabled: true,
            overrideApplied: true
          }
        }
      }
    };

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Map(),
      json: async () => ({})
    } as any);

    render(<V3Player autoStart={true} recordingId="rec-observe" />);

    await waitFor(() => {
      expect(sdk.postRecordingPlaybackInfo as any).toHaveBeenCalled();
    });

    const statsButton = screen.getAllByRole('button').find((button) =>
      button.textContent?.includes('📊')
    );
    expect(statsButton).toBeDefined();
    fireEvent.click(statsButton!);

    expect(await screen.findByText('hash-observe-1')).toBeInTheDocument();
    expect(screen.getAllByText('quality').length).toBeGreaterThan(0);
    expect(screen.getAllByText('compatible').length).toBeGreaterThan(0);
    expect(screen.getAllByText('repair').length).toBeGreaterThan(0);
    expect(screen.getAllByText('constrained').length).toBeGreaterThan(0);
    expect(screen.getByText('problem-recording-source')).toBeInTheDocument();
    expect(screen.getByText('recording')).toBeInTheDocument();
    expect(screen.getAllByText(/compatible video h264 crf23 fast/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/compatible audio aac 256 stereo/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/repair audio aac 192 stereo/i).length).toBeGreaterThan(0);
    expect(screen.getByText(/ts · v:transcode\/h264\/crf23\/fast · a:transcode\/aac\/2ch@256k/i)).toBeInTheDocument();
    expect(screen.getByText('CPU')).toBeInTheDocument();
    expect(screen.getAllByText('yes').length).toBeGreaterThanOrEqual(3);
  });

  it('renders legacy request profile ids with the clearer public label', async () => {
    const mockInfo: any = {
      mode: 'transcode',
      requestId: 'req-observe-2',
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/recordings/rec-observe/index.m3u8?profile=high',
        selectedOutputKind: 'hls',
        targetProfileHash: 'hash-observe-2',
        targetProfile: {
          container: 'mpegts',
          packaging: 'ts',
          hwAccel: 'none',
          video: { mode: 'copy', codec: 'h264', width: 0, height: 0, fps: 0 },
          audio: { mode: 'copy', codec: 'aac', channels: 2, bitrateKbps: 0, sampleRate: 48000 },
          hls: { enabled: true, segmentContainer: 'mpegts', segmentSeconds: 6 }
        },
        trace: {
          requestId: 'req-observe-2',
          requestProfile: 'high',
          targetProfileHash: 'hash-observe-2'
        }
      }
    };

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Map(),
      json: async () => ({})
    } as any);

    render(<V3Player autoStart={true} recordingId="rec-observe" />);

    await waitFor(() => {
      expect(sdk.postRecordingPlaybackInfo as any).toHaveBeenCalled();
    });

    const statsButton = screen.getAllByRole('button').find((button) =>
      button.textContent?.includes('📊')
    );
    expect(statsButton).toBeDefined();
    fireEvent.click(statsButton!);

    await waitFor(() => {
      expect(screen.queryAllByText('compatible').length).toBeGreaterThan(0);
    });
  });
});
