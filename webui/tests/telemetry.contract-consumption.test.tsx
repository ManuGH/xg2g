
import React from 'react';
import { render, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../src/client-ts';
import { telemetry } from '../src/services/TelemetryService';

// Mock SDK
vi.mock('../src/client-ts', async () => {
  return {
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('Telemetry Contract Consumption', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    telemetry.clear();
  });

  it('emits "backend" event when PlaybackInfo decision is consumed', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'direct_mp4',
        decision: {
          selectedOutputUrl: 'http://normative.url',
          mode: 'direct_play',
          selectedOutputKind: 'file'
        },
        requestId: 'req-norm-1'
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-tele-1" />);

    await waitFor(() => {
      const events = telemetry.getEvents();
      const consumed = events.find(e => e.type === 'ui.contract.consumed');
      expect(consumed).toBeDefined();
      expect(consumed?.payload.mode).toBe('backend');
      expect(consumed?.payload.fields).toContain('mode');
      expect(consumed?.payload.fields).toContain('decision.selectedOutputUrl');
    });
  });

  it('emits failclosed when backend omits decision selection', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'hlsjs',
        requestId: 'req-leg-1'
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-tele-2" />);

    await waitFor(() => {
      const events = telemetry.getEvents();
      const fail = events.find(e => e.type === 'ui.failclosed');
      expect(fail).toBeDefined();
    });
  });
});
