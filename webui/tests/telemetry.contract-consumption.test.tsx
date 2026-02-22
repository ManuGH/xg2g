
import React from 'react';
import { render, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as sdk from '../src/client-ts';
import { telemetry } from '../src/services/TelemetryService';

// Mock SDK
vi.mock('../src/client-ts', async () => {
  return {
    getRecordingPlaybackInfo: vi.fn(),
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
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
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
      expect(consumed?.payload.mode).toBe('normative');
      expect(consumed?.payload.fields).toContain('decision.selectedOutputUrl');
    });
  });

  it('emits "legacy" event when Legacy URL is consumed', async () => {
    // Legacy fallback: decision undefined
    (sdk.getRecordingPlaybackInfo as any).mockResolvedValue({
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
      const consumed = events.find(e => e.type === 'ui.contract.consumed');
      expect(consumed).toBeDefined();
      expect(consumed?.payload.mode).toBe('legacy');
      expect(consumed?.payload.fields).toContain('url');
    });
  });
});
