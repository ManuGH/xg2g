import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../../src/client-ts';
import fc from 'fast-check';

vi.mock('../../src/client-ts', async () => {
  return {
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player Contract Fuzzing', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fails closed whenever backend decision selection is missing', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.record({
          mode: fc.constantFrom('hlsjs', 'native_hls', 'direct_mp4', 'transcode'),
          decision: fc.oneof(
            fc.constant(undefined),
            fc.record({
              selectedOutputUrl: fc.oneof(fc.constant(undefined), fc.string()),
              mode: fc.constantFrom('direct_play', 'direct_stream', 'transcode'),
              selectedOutputKind: fc.constantFrom('file', 'hls')
            })
          )
        }),
        async (payload) => {
          vi.clearAllMocks();
          (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
            data: payload,
            response: { status: 200 }
          });

          const { unmount } = render(<V3Player autoStart={true} recordingId="rec-fuzz" />);
          const hasSelected = !!payload.decision?.selectedOutputUrl;

          if (!hasSelected) {
            await waitFor(() => {
              expect(screen.getByText(/Backend decision missing selectedOutputUrl/i)).toBeInTheDocument();
            });
          }

          unmount();
        }
      ),
      { numRuns: 10, seed: 42 }
    );
  });
});
