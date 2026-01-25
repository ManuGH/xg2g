import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../src/client-ts/sdk.gen';

vi.mock('../src/client-ts/sdk.gen', async () => {
  const actual = await vi.importActual<any>('../src/client-ts/sdk.gen');
  return {
    ...actual,
    getRecordingPlaybackInfo: vi.fn(),
  };
});

describe('V3Player Contract Consumption (UI-CON-PLAYER-001)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fails loudly if decision exists but selectedOutputUrl is missing (governance violation)', async () => {
    // Mock a response that has forbidden 'outputs' but missing 'selectedOutputUrl'
    const mockInfo: any = {
      decision: {
        mode: 'direct_play',
        outputs: [{ kind: 'file', url: '/forbidden/path.mp4' }]
        // missing selectedOutputUrl
      },
      requestId: 'req-bad-contract'
    };

    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

    render(<V3Player autoStart={true} recordingId="rec-1" />);

    await waitFor(async () => {
      // Check for the specific decision-led error message within the error toast
      const errorToast = await screen.findByRole('alert');
      expect(errorToast.textContent).toContain('Decision-led playback missing explicit selection');
    });
  });

  it('prefers normative selectedOutputUrl over legacy url', async () => {
    const mockInfo: any = {
      url: '/legacy/url.m3u8',
      mode: 'hls',
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/normative/url.m3u8',
        selectedOutputKind: 'hls'
      },
      requestId: 'req-good-contract'
    };

    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({ data: mockInfo });

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
});
