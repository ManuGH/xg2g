import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/features/player/components/V3Player';
import { postRecordingPlaybackInfo } from '../src/client-ts';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
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
    postRecordingPlaybackInfo: vi.fn(),
  };
});

describe('V3Player TV resume overlay focus', () => {
  const requestInputFocus = vi.fn();
  const mockedPostRecordingPlaybackInfo = vi.mocked(postRecordingPlaybackInfo);

  beforeEach(() => {
    vi.clearAllMocks();
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;

    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});

    window.Xg2gHost = {
      getCapabilitiesJson: () => JSON.stringify({
        platform: 'android-tv',
        isTv: true,
        supportsKeepScreenAwake: true,
        supportsHostMediaKeys: true,
        supportsInputFocus: true,
        supportsNativePlayback: false,
      }),
      requestInputFocus,
      setPlaybackActive: vi.fn(),
    };

    mockedPostRecordingPlaybackInfo.mockResolvedValue({
      data: {
        mode: 'direct_mp4',
        requestId: 'vod-resume-focus-1',
        isSeekable: true,
        durationSeconds: 1800,
        resume: {
          posSeconds: 569,
          durationSeconds: 1800,
          finished: false,
        },
        decision: {
          selectedOutputUrl: `${window.location.origin}/streams/recordings/focus.mp4`,
          reasons: ['direct_play'],
        },
      } as any,
      error: undefined,
      response: {
        status: 200,
        headers: {
          get: () => null,
        },
      } as any,
    });

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: {
        get: () => null,
      },
      json: async () => ({}),
      text: async () => '{}',
    }) as unknown as typeof globalThis.fetch);
  });

  afterEach(() => {
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;
    vi.restoreAllMocks();
  });

  it('moves TV focus onto the resume action when the overlay opens', async () => {
    render(<V3Player autoStart={true} recordingId="rec-focus-1" />);

    const resumeButton = await screen.findByRole('button', { name: /resume/i });

    await waitFor(() => {
      expect(requestInputFocus).toHaveBeenCalledTimes(2);
      expect(resumeButton).toHaveFocus();
    });
  });

  it('suppresses playback chrome while the startup overlay is active', async () => {
    mockedPostRecordingPlaybackInfo.mockResolvedValueOnce({
      data: {
        mode: 'direct_mp4',
        requestId: 'vod-startup-layout-1',
        isSeekable: true,
        durationSeconds: 1800,
        decision: {
          selectedOutputUrl: `${window.location.origin}/streams/recordings/layout.mp4`,
          reasons: ['direct_play'],
        },
      } as any,
      error: undefined,
      response: {
        status: 200,
        headers: {
          get: () => null,
        },
      } as any,
    });

    render(<V3Player autoStart={true} recordingId="rec-startup-layout-1" />);

    expect(await screen.findByText(/preparing direct play/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /stats/i })).not.toBeInTheDocument();
    expect(screen.queryByText('+15s')).not.toBeInTheDocument();
  });
});
