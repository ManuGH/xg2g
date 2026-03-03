import React from 'react';
import { render, waitFor } from '@testing-library/react';
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
});
