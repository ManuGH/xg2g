import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../types/v3-player';

vi.mock('../client-ts', () => ({
  createSession: vi.fn(),
  postRecordingPlaybackInfo: vi.fn(),
}));

describe('V3Player ServiceRef Input', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: vi.fn().mockReturnValue('application/json') },
          text: vi.fn().mockResolvedValue(JSON.stringify({
            mode: 'hlsjs',
            requestId: 'live-decision-1',
            playbackDecisionToken: 'live-token-1',
            decision: { reasons: ['direct_stream_match'] },
          }))
        });
      }
      if (url.includes('/intents')) {
        return Promise.resolve({
          status: 409,
          ok: false,
          headers: { get: vi.fn().mockReturnValue(null) },
          json: vi.fn().mockResolvedValue({ code: 'LEASE_BUSY', requestId: 'test' })
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
    expect(body.params.playback_mode).toBe('hlsjs');
    expect(body.params.playback_decision_token).toBe('live-token-1');
    expect(body.params.playback_decision_id).toBeUndefined();
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
      expect(globalThis.fetch).toHaveBeenCalled();
    });

    const intentCall = (globalThis.fetch as any).mock.calls.find((c: any[]) => String(c[0]).includes('/intents'));
    expect(intentCall).toBeDefined();
    const [url, options] = intentCall;
    expect(String(url)).toContain('/intents');
    const body = JSON.parse(options.body);
    expect(body.serviceRef).toBe(newRef);
    expect(body.params.playback_mode).toBe('hlsjs');
    expect(body.params.playback_decision_token).toBe('live-token-1');
    expect(body.params.playback_decision_id).toBeUndefined();
  });
});
