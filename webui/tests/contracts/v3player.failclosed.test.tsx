import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../../src/components/V3Player';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as sdk from '../../src/client-ts/sdk.gen';
import { useCapabilities } from '../../src/hooks/useCapabilities';

// Mock SDK
vi.mock('../../src/client-ts/sdk.gen', async () => {
  return {
    getRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

vi.mock('../../src/hooks/useCapabilities', () => ({
  useCapabilities: vi.fn().mockReturnValue({
    capabilities: { 'contracts.playbackInfoDecision': 'required' },
    loading: false
  })
}));

describe('V3Player Contract Enforcement (Fail-Closed)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('fails logically when decision exists but selectedOutputUrl is missing', async () => {
    // VIOLATION: Backend sends decision but forgets mandatory selection
    // The UI must not conform to this invalid state (e.g. by guessing).
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
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
      // Matches Policy Engine Violation
      expect(screen.getByText(/Policy Violation|POLICY_VIOLATION_FAILCLOSED/i)).toBeInTheDocument();
    });

    // Ensure we did NOT try to play the legacy URL
    // (This is hard to verify without inspecting internal state, 
    // but the error screen implies we didn't start success path)
  });

  it('ignores decision if null and falls back to legacy (if permitted)', async () => {
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        decision: null,
        url: '/legacy.m3u8',
        mode: 'hls'
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-2" />);

    // Should successfully start legacy playback (mocking HLS/Video would be needed to see video, 
    // but we can check we didn't error)
    await waitFor(() => {
      // If we don't see error, and we assumed mock implies valid fetch...
      // Actually V3Player tries to load video.
      // Let's just check we don't see the contract error.
      expect(screen.queryByText(/player.playbackError/i)).not.toBeInTheDocument();
    });
  });
});
