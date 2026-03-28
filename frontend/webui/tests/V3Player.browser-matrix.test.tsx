import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import * as sdk from '../src/client-ts';
import V3Player from '../src/features/player/components/V3Player';
import { resetCachedCodecs } from '../src/features/player/utils/codecDetection';
import { suppressExpectedConsoleNoise } from './helpers/consoleNoise';
import { findFetchCall, mockLiveFlowFetch } from './helpers/liveFlow';
import { applyBrowserFamilyMatrix, browserFamilyMatrixCases } from './helpers/playbackMatrix';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
    };
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError',
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    postRecordingPlaybackInfo: vi.fn(),
  };
});

const originalFetch = global.fetch;
const liveChannel = {
  id: '1:0:1:MATRIX',
  serviceRef: '1:0:1:MATRIX',
  name: 'Matrix Channel',
};

function mockJsonResponse(url: string, status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    url,
    headers: {
      get: (name: string) => (name.toLowerCase() === 'content-type' ? 'application/json' : null),
    },
    json: async () => body,
    text: async () => JSON.stringify(body),
  };
}

describe('V3Player Browser Family Matrix', () => {
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      error: [/HLS playback engine not available/i],
      warn: [/Failed to stop v3 session/i, /Failed to parse URL from \/api\/v3\/intents/i],
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();
    resetCachedCodecs();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    global.fetch = originalFetch;
    resetCachedCodecs();
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
  });

  it.each(browserFamilyMatrixCases)('live capabilities: %s family sends the expected matrix', async (fixture) => {
    const restoreBrowserFamily = applyBrowserFamilyMatrix(fixture.id);

    try {
      mockLiveFlowFetch({
        mode: fixture.liveMode,
        requestId: `req-${fixture.id}`,
        playbackDecisionToken: `token-${fixture.id}`,
        sessionId: `sess-${fixture.id}`,
        playbackUrl: `/${fixture.id}.m3u8`,
      });

      render(<V3Player autoStart={true} channel={liveChannel} />);

      await waitFor(() => expect(findFetchCall(global.fetch as any, '/live/stream-info')).toBeDefined());
      await waitFor(() => expect(findFetchCall(global.fetch as any, '/intents')).toBeDefined());

      const streamInfoCall = findFetchCall(global.fetch as any, '/live/stream-info');
      const streamInfoBody = JSON.parse(String(streamInfoCall?.[1]?.body ?? '{}'));
      expect(streamInfoBody.capabilities.capabilitiesVersion).toBe(3);
      expect(streamInfoBody.capabilities.container).toEqual(fixture.capabilities.live.container);
      expect(streamInfoBody.capabilities.videoCodecs).toEqual(fixture.capabilities.live.videoCodecs);
      expect(streamInfoBody.capabilities.audioCodecs).toEqual(fixture.capabilities.live.audioCodecs);
      expect(streamInfoBody.capabilities.hlsEngines).toEqual(fixture.capabilities.live.hlsEngines);
      expect(streamInfoBody.capabilities.preferredHlsEngine).toBe(fixture.capabilities.live.preferredHlsEngine);
      expect(streamInfoBody.capabilities.runtimeProbeUsed).toBe(true);
      expect(streamInfoBody.capabilities.runtimeProbeVersion).toBe(2);
      expect(streamInfoBody.capabilities.clientFamilyFallback).toBe(fixture.id);

      const intentCall = findFetchCall(global.fetch as any, '/intents');
      const intentBody = JSON.parse(String(intentCall?.[1]?.body ?? '{}'));
      expect(intentBody.params.playback_mode).toBe(fixture.liveMode);
      expect(intentBody.params.playback_decision_token).toBe(`token-${fixture.id}`);
    } finally {
      restoreBrowserFamily();
    }
  });

  it.each(browserFamilyMatrixCases)('recording capabilities: %s family sends the expected matrix', async (fixture) => {
    const restoreBrowserFamily = applyBrowserFamilyMatrix(fixture.id);

    try {
      vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = String(input);
        return Promise.resolve(mockJsonResponse(url, 200, {}));
      }) as unknown as typeof globalThis.fetch);

      (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
        data: {
          mode: fixture.recordingMode,
          requestId: `req-recording-${fixture.id}`,
          decision: {
            mode: 'transcode',
            selectedOutputUrl: `/recordings/${fixture.id}/index.m3u8`,
            selectedOutputKind: 'hls',
          },
        },
      });

      render(<V3Player autoStart={true} recordingId={`rec-${fixture.id}`} />);

      await waitFor(() => {
        expect(sdk.postRecordingPlaybackInfo as any).toHaveBeenCalled();
      });

      const request = (sdk.postRecordingPlaybackInfo as any).mock.calls[0]?.[0];
      expect(request?.body?.capabilitiesVersion).toBe(3);
      expect(request?.body?.container).toEqual(fixture.capabilities.recording.container);
      expect(request?.body?.videoCodecs).toEqual(fixture.capabilities.recording.videoCodecs);
      expect(request?.body?.audioCodecs).toEqual(fixture.capabilities.recording.audioCodecs);
      expect(request?.body?.hlsEngines).toEqual(fixture.capabilities.recording.hlsEngines);
      expect(request?.body?.preferredHlsEngine).toBe(fixture.capabilities.recording.preferredHlsEngine);
      expect(request?.body?.runtimeProbeUsed).toBe(true);
      expect(request?.body?.runtimeProbeVersion).toBe(2);
      expect(request?.body?.clientFamilyFallback).toBe(fixture.id);
    } finally {
      restoreBrowserFamily();
    }
  });

  it.each(browserFamilyMatrixCases)('stats overlay: %s family surfaces intent and ladder semantics', async (fixture) => {
    const restoreBrowserFamily = applyBrowserFamilyMatrix(fixture.id);

    try {
      mockLiveFlowFetch({
        mode: fixture.liveMode,
        requestId: `req-trace-${fixture.id}`,
        playbackDecisionToken: `token-trace-${fixture.id}`,
        sessionId: `sess-trace-${fixture.id}`,
        playbackUrl: `/${fixture.id}-trace.m3u8`,
        sessionTrace: {
          requestId: `trace-${fixture.id}`,
          requestedIntent: fixture.trace.requestedIntent,
          resolvedIntent: fixture.trace.resolvedIntent,
          qualityRung: fixture.trace.qualityRung,
          degradedFrom: fixture.trace.degradedFrom ?? undefined,
          targetProfileHash: `hash-${fixture.id}`,
        },
      });

      render(<V3Player autoStart={true} channel={liveChannel} />);

      await waitFor(() => expect(findFetchCall(global.fetch as any, `/sessions/sess-trace-${fixture.id}`)).toBeDefined());

      const statsButton = screen.getAllByRole('button').find((button) =>
        button.textContent?.includes('📊')
      );
      expect(statsButton).toBeDefined();
      fireEvent.click(statsButton!);

      expect(await screen.findByText(fixture.expectedOverlayClientPath)).toBeInTheDocument();
      expect(screen.getAllByText(fixture.trace.requestedIntent).length).toBeGreaterThan(0);
      expect(screen.getAllByText(fixture.trace.resolvedIntent).length).toBeGreaterThan(0);
      expect(
        screen.getByText(new RegExp(fixture.trace.qualityRung.replace(/_/g, ' '), 'i'))
      ).toBeInTheDocument();

      if (fixture.trace.degradedFrom) {
        expect(screen.getAllByText(fixture.trace.degradedFrom).length).toBeGreaterThan(0);
      }
    } finally {
      restoreBrowserFamily();
    }
  });
});
