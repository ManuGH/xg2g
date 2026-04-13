
import React from 'react';
import { render, waitFor } from '@testing-library/react';
import V3Player from '../src/features/player/components/V3Player';
import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
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

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('emits normalized contract consumption when playback info is consumed', async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);

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
      const advisory = events.find(e => e.type === 'playback_advisory');
      const blocked = events.find(e => e.type === 'playback_contract_blocked');
      expect(consumed).toBeDefined();
      expect(advisory).toBeDefined();
      expect(blocked).toBeUndefined();
      expect(consumed?.payload.mode).toBe('normalized');
      expect(consumed?.payload.kind).toBe('playable');
      expect(consumed?.payload.fields).toContain('playback.mode');
      expect(consumed?.payload.fields).toContain('playback.outputUrl');
    });

    expect(fetchSpy).not.toHaveBeenCalled();
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
      const semanticFail = events.find(e => e.type === 'playback_contract_blocked');
      expect(fail).toBeDefined();
      expect(semanticFail).toBeDefined();
    });
  });

  it('starts direct_mp4 without browser preflight even for same-origin urls', async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        mode: 'direct_mp4',
        isSeekable: true,
        decision: {
          selectedOutputUrl: `${window.location.origin}/streams/direct.mp4`,
          mode: 'direct_play',
          selectedOutputKind: 'file'
        },
        requestId: 'req-same-origin-direct'
      },
      response: { status: 200 }
    });

    render(<V3Player autoStart={true} recordingId="rec-tele-3" />);

    await waitFor(() => {
      const events = telemetry.getEvents();
      const consumed = events.find(e => e.type === 'ui.contract.consumed');
      expect(consumed).toBeDefined();
      expect(consumed?.payload.mode).toBe('normalized');
      expect(consumed?.payload.kind).toBe('playable');
    });

    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
