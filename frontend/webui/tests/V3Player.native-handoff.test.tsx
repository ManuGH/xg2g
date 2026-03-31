import React from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/features/player/components/V3Player';
import { HOST_NATIVE_PLAYBACK_STATE_EVENT } from '../src/lib/hostBridge';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  return { default: HlsMock };
});

describe('V3Player native Android handoff', () => {
  let nativeState: Record<string, unknown>;

  beforeEach(() => {
    nativeState = {
      activeRequest: null,
      session: null,
      playerState: 1,
      playWhenReady: false,
      isInPip: false,
      lastError: null,
    };

    vi.clearAllMocks();
    delete window.__XG2G_HOST__;
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});

    window.Xg2gHost = {
      getCapabilitiesJson: () => JSON.stringify({
        platform: 'android-tv',
        isTv: true,
        supportsKeepScreenAwake: true,
        supportsHostMediaKeys: true,
        supportsInputFocus: true,
        supportsNativePlayback: true,
      }),
      requestInputFocus: vi.fn(),
      setPlaybackActive: vi.fn(),
      startNativePlayback: vi.fn((payloadJson: string) => {
        nativeState = {
          activeRequest: JSON.parse(payloadJson),
          session: null,
          playerState: 1,
          playWhenReady: true,
          isInPip: false,
          lastError: null,
        };
      }),
      stopNativePlayback: vi.fn(() => {
        nativeState = {
          activeRequest: null,
          session: null,
          playerState: 1,
          playWhenReady: false,
          isInPip: false,
          lastError: null,
        };
      }),
      getNativePlaybackStateJson: vi.fn(() => JSON.stringify(nativeState)),
    };
  });

  afterEach(() => {
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;
    vi.restoreAllMocks();
  });

  it('hands live playback off to the native host without posting stream.start from the web player', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);

      throw new Error(`web fetch should not run for native live handoff: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock as unknown as typeof globalThis.fetch);

    render(<V3Player autoStart={true} channel={{ id: 'ch-live-1', serviceRef: '1:0:1:AA', name: 'Das Erste HD' } as any} />);

    await waitFor(() => {
      expect(window.Xg2gHost?.startNativePlayback).toHaveBeenCalledTimes(1);
    });

    const payload = JSON.parse((window.Xg2gHost?.startNativePlayback as any).mock.calls[0][0]);
    expect(payload).toMatchObject({
      kind: 'live',
      serviceRef: '1:0:1:AA',
      title: 'Das Erste HD',
    });
    expect(payload.playbackDecisionToken).toBeUndefined();
    expect(payload.params).toBeUndefined();
    expect(fetchMock).not.toHaveBeenCalled();

    const liveReadyState = {
      activeRequest: payload,
      session: {
        sessionId: 'sess-native-live-1',
        state: 'READY',
        requestId: 'req-native-live-1',
        profileReason: 'directplay_match',
        trace: {
          requestId: 'req-native-live-1',
          sessionId: 'sess-native-live-1',
          clientPath: 'android/native',
        },
      },
      diagnostics: {
        requestId: 'req-native-live-1',
        playbackMode: 'native_hls',
        playbackInfo: {
          requestId: 'req-native-live-1',
          decision: {
            trace: {
              requestId: 'req-native-live-1',
              clientPath: 'android/native',
            },
          },
        },
        trace: {
          requestId: 'req-native-live-1',
          sessionId: 'sess-native-live-1',
          clientPath: 'android/native',
        },
      },
      playerState: 3,
      playWhenReady: true,
      isInPip: false,
      lastError: null,
    };

    await act(async () => {
      nativeState = liveReadyState;
      window.dispatchEvent(new CustomEvent(HOST_NATIVE_PLAYBACK_STATE_EVENT, {
        detail: liveReadyState,
      }));
    });

    fireEvent.click(screen.getByRole('button', { name: /stats/i }));
    await waitFor(() => {
      expect(screen.getByText('sess-native-live-1')).toBeInTheDocument();
      expect(screen.getByText('req-native-live-1')).toBeInTheDocument();
    });
  });

  it('hands recording playback off to the native host and stops it through the host bridge', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      throw new Error(`web fetch should not run for native recording handoff: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock as unknown as typeof globalThis.fetch);

    render(<V3Player autoStart={true} recordingId="rec-native-1" />);

    await waitFor(() => {
      expect(window.Xg2gHost?.startNativePlayback).toHaveBeenCalledTimes(1);
    });

    const payload = JSON.parse((window.Xg2gHost?.startNativePlayback as any).mock.calls[0][0]);
    expect(payload).toMatchObject({
      kind: 'recording',
      recordingId: 'rec-native-1',
      startPositionMs: 0,
      title: 'rec-native-1',
    });

    fireEvent.click(screen.getByRole('button', { name: /stop/i }));

    await waitFor(() => {
      expect((window.Xg2gHost?.stopNativePlayback as any).mock.calls.length).toBeGreaterThanOrEqual(1);
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
