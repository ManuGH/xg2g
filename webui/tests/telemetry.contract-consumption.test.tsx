
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

  it('emits "normative" event when Decision is consumed', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'transcode',
        decision: {
          selectedOutputUrl: 'http://normative.url',
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
      expect(consumed?.payload.fields).toContain('decision.selectedOutputUrl');
    });
  });

  it('emits ui.failclosed when backend returns unsupported legacy mode', async () => {
    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        url: 'http://legacy.url',
        mode: 'hls',
        requestId: 'req-leg-1'
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-tele-2" />);

    await waitFor(() => {
      const events = telemetry.getEvents();
      const fail = events.find(e => e.type === 'ui.failclosed');
      expect(fail).toBeDefined();
      expect(fail?.payload.context).toBe('V3Player.mode.invalid');
    });
  });
});
