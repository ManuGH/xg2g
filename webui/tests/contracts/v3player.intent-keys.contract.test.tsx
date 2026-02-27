import React from 'react';
import { render, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterAll } from 'vitest';
import V3Player from '../../src/components/V3Player';
import { suppressExpectedConsoleNoise } from '../helpers/consoleNoise';
import { findFetchCall, mockLiveFlowFetch } from '../helpers/liveFlow';

describe('V3Player Intent Keys Contract', () => {
  const originalFetch = globalThis.fetch;
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      error: [/HLS playback engine not available/i],
      warn: [/Failed to stop v3 session/i, /Failed to parse URL from \/api\/v3\/intents/i]
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();

    mockLiveFlowFetch({
      mode: 'hlsjs',
      requestId: 'req-intent-keys-1',
      playbackDecisionToken: 'token-intent-keys-1',
      sessionId: 'sess-intent-keys-1',
      playbackUrl: '/live.m3u8'
    });
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
    globalThis.fetch = originalFetch;
  });

  it('sends canonical playback_decision_token and not deprecated playback_decision_id', async () => {
    render(<V3Player autoStart={true} channel={{ id: 'ch-keys-1', serviceRef: '1:0:1:AA:BB:CC:0:0:0:0:' } as any} />);

    await waitFor(() => {
      expect(findFetchCall((globalThis.fetch as any), '/live/stream-info')).toBeDefined();
      expect(findFetchCall((globalThis.fetch as any), '/intents')).toBeDefined();
    });

    const calls = (globalThis.fetch as any).mock.calls as any[];
    const streamInfoIndex = calls.findIndex((c: any[]) => String(c[0]).includes('/live/stream-info'));
    const intentsIndex = calls.findIndex((c: any[]) => String(c[0]).includes('/intents'));
    expect(streamInfoIndex).toBeGreaterThanOrEqual(0);
    expect(intentsIndex).toBeGreaterThan(streamInfoIndex);

    const intentCall = findFetchCall((globalThis.fetch as any), '/intents');
    expect(intentCall).toBeDefined();

    const body = JSON.parse(String(intentCall?.[1]?.body ?? '{}'));
    expect(body.params.playback_decision_token).toBe('token-intent-keys-1');
    expect(body.params.playback_decision_id).toBeUndefined();
  });
});
