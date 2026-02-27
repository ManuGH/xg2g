import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../../src/client-ts';

// Mock SDK
vi.mock('../../src/client-ts', async () => {
  return {
    createSession: vi.fn(),
    postLivePlaybackInfo: vi.fn(),
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player Contract Enforcement (Fail-Closed)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fails logically when decision exists but selectedOutputUrl is missing', async () => {
    // VIOLATION: Backend sends decision but forgets mandatory selection
    // The UI must not conform to this invalid state (e.g. by guessing).
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'hlsjs',
        decision: {
          // Missing selectedOutputUrl
          // Missing mode
          // But object exists
        },
        // Legacy fields should be ignored if decision is present (per precedence rules)
        url: '/legacy.m3u8'
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-1" />);

    // Expect generic error or specific contract error
    await waitFor(() => {
      expect(screen.getByText(/Backend decision missing selectedOutputUrl/i)).toBeInTheDocument();
    });

    // Ensure we did NOT try to play the legacy URL
    // (This is hard to verify without inspecting internal state,
    // but the error screen implies we didn't start success path)
  });

  it('fails closed when decision is missing', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        decision: null,
        url: '/legacy.m3u8',
        mode: 'hlsjs'
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-2" />);

    await waitFor(() => {
      expect(screen.getByText(/Backend decision missing selectedOutputUrl/i)).toBeInTheDocument();
    });
  });

  it('maps deny mode deterministically even when selectedOutputUrl is absent', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'deny',
        reason: 'policy_denies_transcode',
        decision: {
          mode: 'deny',
          selected: {
            container: 'none',
            videoCodec: 'none',
            audioCodec: 'none'
          },
          outputs: [],
          constraints: [],
          reasons: ['policy_denies_transcode'],
          trace: { requestId: 'req-deny-1' }
        }
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-3" />);

    await waitFor(() => {
      expect(screen.getByText(/player.playbackDenied/i)).toBeInTheDocument();
      expect(screen.queryByText(/Backend decision missing selectedOutputUrl/i)).not.toBeInTheDocument();
    });
  });

  it('live path fails closed on deny and does not start intents fallback', async () => {
    const originalFetch = globalThis.fetch;
    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            mode: 'deny',
            requestId: 'req-live-deny-1',
            reason: 'no_compatible_playback_path',
            decision: {
              mode: 'deny',
              reasons: ['no_compatible_playback_path'],
              selected: {
                container: 'none',
                videoCodec: 'none',
                audioCodec: 'none'
              },
              outputs: [],
              constraints: [],
              trace: { requestId: 'req-live-deny-1' }
            }
          })
        });
      }

      if (url.includes('/intents')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ sessionId: 'should-not-be-used' })
        });
      }

      return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({})
      });
    });

    try {
      render(<V3Player autoStart={true} channel={{ id: 'ch-live-1', serviceRef: '1:0:1:AA:BB:CC:0:0:0:0:' } as any} />);

      await waitFor(() => {
        expect(screen.getByText(/player.playbackDenied/i)).toBeInTheDocument();
      });

      const calls = (globalThis.fetch as any).mock.calls.map((c: any[]) => String(c[0]));
      expect(calls.some((url: string) => url.includes('/intents'))).toBe(false);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
