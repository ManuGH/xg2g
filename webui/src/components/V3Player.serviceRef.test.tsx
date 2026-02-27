import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach, afterAll } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../types/v3-player';
import { suppressExpectedConsoleNoise } from '../../tests/helpers/consoleNoise';
import { findFetchCall, mockLiveFlowFetch } from '../../tests/helpers/liveFlow';

describe('V3Player ServiceRef Input', () => {
  let originalFetch: typeof globalThis.fetch;
  let restoreConsoleNoise: (() => void) | null = null;

  beforeAll(() => {
    originalFetch = globalThis.fetch;
    restoreConsoleNoise = suppressExpectedConsoleNoise({
      error: [/HLS playback engine not available/i],
      warn: [/Failed to stop v3 session/i, /Failed to parse URL from \/api\/v3\/intents/i]
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mockLiveFlowFetch({
      mode: 'hlsjs',
      requestId: 'live-decision-1',
      playbackDecisionToken: 'live-token-1',
      sessionId: 'sess-live-ref-1',
      playbackUrl: '/live-ref.m3u8'
    });
  });

  afterAll(() => {
    restoreConsoleNoise?.();
    restoreConsoleNoise = null;
    (globalThis as any).fetch = originalFetch;
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
      expect(findFetchCall((globalThis.fetch as any), '/intents')).toBeDefined();
    });

    const streamInfoCall = findFetchCall((globalThis.fetch as any), '/live/stream-info');
    expect(streamInfoCall).toBeDefined();
    const streamInfoBody = JSON.parse(String(streamInfoCall?.[1]?.body ?? '{}'));
    expect(streamInfoBody.serviceRef).toBe(newRef);

    const intentsCall = findFetchCall((globalThis.fetch as any), '/intents');
    expect(intentsCall).toBeDefined();
    const intentsBody = JSON.parse(String(intentsCall?.[1]?.body ?? '{}'));
    expect(intentsBody.serviceRef).toBe(newRef);
    expect(intentsBody.params.playback_mode).toBe('hlsjs');
    expect(intentsBody.params.playback_decision_token).toBe('live-token-1');
    expect(intentsBody.params.playback_decision_id).toBeUndefined();
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

    const startButton = screen.getByRole('button', { name: /common\.startStream/i });
    fireEvent.click(startButton);

    await waitFor(() => {
      expect(findFetchCall((globalThis.fetch as any), '/intents')).toBeDefined();
    });

    const streamInfoCall = findFetchCall((globalThis.fetch as any), '/live/stream-info');
    expect(streamInfoCall).toBeDefined();
    const streamInfoBody = JSON.parse(String(streamInfoCall?.[1]?.body ?? '{}'));
    expect(streamInfoBody.serviceRef).toBe(newRef);

    const intentsCall = findFetchCall((globalThis.fetch as any), '/intents');
    expect(intentsCall).toBeDefined();
    const intentsBody = JSON.parse(String(intentsCall?.[1]?.body ?? '{}'));
    expect(intentsBody.serviceRef).toBe(newRef);
    expect(intentsBody.params.playback_mode).toBe('hlsjs');
    expect(intentsBody.params.playback_decision_token).toBe('live-token-1');
    expect(intentsBody.params.playback_decision_id).toBeUndefined();
  });
});
