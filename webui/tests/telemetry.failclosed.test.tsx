
import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../src/client-ts';
import { telemetry } from '../src/services/TelemetryService';

vi.mock('../src/client-ts', async () => {
  return {
    getRecordingPlaybackInfo: vi.fn(),
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
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
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
      expect(screen.getByText(/Decision-led|player.playbackError/i)).toBeInTheDocument();

      // Check Telemetry
      const events = telemetry.getEvents();
      const failEvent = events.find(e => e.type === 'ui.failclosed');
      expect(failEvent).toBeDefined();
      expect(failEvent?.payload.context).toContain('V3Player.decision.selectionMissing');
    });
  });
});
