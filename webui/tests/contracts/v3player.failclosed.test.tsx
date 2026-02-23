import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../../src/components/V3Player';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as sdk from '../../src/client-ts';
import { useCapabilities } from '../../src/hooks/useCapabilities';

// Mock SDK
vi.mock('../../src/client-ts', async () => {
  return {
    postRecordingPlaybackInfo: vi.fn(),
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
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'transcode',
        decision: {
          // Missing selectedOutputUrl
        },
        url: '/legacy.m3u8'
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-1" />);

    await waitFor(() => {
      expect(screen.getByText(/Backend decision missing selectedOutputUrl|selectedOutputUrl/i)).toBeInTheDocument();
    });
  });

  it('fails when backend returns unsupported legacy mode value', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        decision: null,
        url: '/legacy.m3u8',
        mode: 'hls'
      }
    });

    render(<V3Player autoStart={true} recordingId="rec-contra-2" />);

    await waitFor(() => {
      expect(screen.getByText(/Unsupported backend playback mode: hls|player.playbackError/i)).toBeInTheDocument();
    });
  });
});
