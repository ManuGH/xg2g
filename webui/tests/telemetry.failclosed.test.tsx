
import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../src/client-ts/sdk.gen';
import { telemetry } from '../src/services/TelemetryService';

vi.mock('../src/client-ts/sdk.gen', async () => {
  return {
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('Telemetry Fail-Closed', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    telemetry.clear();
  });

  it('emits ui.failclosed when Contract fails', async () => {
    // VIOLATION: Decision present but no selected URL
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'hlsjs',
        decision: {
          // Missing selection
        },
        requestId: 'req-fail-1'
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-fail-1" />);

    await waitFor(() => {
      // Check UI error
      expect(screen.getByText(/Backend decision missing selectedOutputUrl|player.playbackError/i)).toBeInTheDocument();

      // Check Telemetry
      const events = telemetry.getEvents();
      const failEvent = events.find(e => e.type === 'ui.failclosed');
      expect(failEvent).toBeDefined();
      expect(failEvent?.payload.context).toContain('V3Player.decision.selectionMissing');
    });
  });

  it('emits deterministic deny telemetry without requiring selected output url', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'deny',
        reason: 'policy_denies_transcode',
        decision: {
          mode: 'deny',
          selected: { container: 'none', videoCodec: 'none', audioCodec: 'none' },
          outputs: [],
          constraints: [],
          reasons: ['policy_denies_transcode'],
          trace: { requestId: 'req-deny-telemetry' }
        }
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-deny-telemetry" />);

    await waitFor(() => {
      expect(screen.getByText(/player.playbackDenied/i)).toBeInTheDocument();
      const events = telemetry.getEvents();
      const denyEvent = events.find(e => e.type === 'ui.failclosed' && e.payload?.context === 'V3Player.mode.deny');
      expect(denyEvent).toBeDefined();
      expect(denyEvent?.payload.reason).toBe('policy_denies_transcode');
    });
  });
});
