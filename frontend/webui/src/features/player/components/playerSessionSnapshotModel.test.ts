import { describe, expect, it } from 'vitest';

import type { V3SessionSnapshot } from '../../../types/v3-player';
import { buildPlayerSessionSnapshotModel } from './playerSessionSnapshotModel';

function session(overrides: Partial<V3SessionSnapshot>): V3SessionSnapshot {
  return {
    sessionId: 'session-1',
    state: 'READY',
    ...overrides,
  };
}

describe('buildPlayerSessionSnapshotModel', () => {
  it('uses backend live seekable bounds when a valid live DVR window is present', () => {
    const model = buildPlayerSessionSnapshotModel(session({
      requestId: 'req-live-1',
      profileReason: 'direct_live',
      mode: 'LIVE',
      windowKind: 'live-dvr',
      seekableStartSeconds: -5,
      seekableEndSeconds: 20,
      liveEdgeSeconds: 25,
    }), 123);

    expect(model).toMatchObject({
      traceId: 'req-live-1',
      profileReason: 'direct_live',
      sessionWindowKind: 'live-dvr',
      liveSeekWindow: {
        start: 0,
        end: 20,
        liveEdge: 25,
        capturedAtMs: 123,
      },
    });
    expect(model.playbackTrace?.requestId).toBe('req-live-1');
  });

  it('derives a live DVR window from duration and live edge when explicit bounds are absent', () => {
    const model = buildPlayerSessionSnapshotModel(session({
      mode: 'LIVE',
      windowKind: 'live',
      durationSeconds: 30,
      liveEdgeSeconds: 100,
    }), 456);

    expect(model.sessionWindowKind).toBe('live');
    expect(model.liveSeekWindow).toEqual({
      start: 70,
      end: 100,
      liveEdge: 100,
      capturedAtMs: 456,
    });
  });

  it('clears the live seek window when live snapshots do not describe a valid window', () => {
    const model = buildPlayerSessionSnapshotModel(session({
      mode: 'LIVE',
      windowKind: 'live-dvr',
      durationSeconds: 0,
      seekableStartSeconds: 20,
      seekableEndSeconds: 20,
    }), 789);

    expect(model.sessionWindowKind).toBe('live-dvr');
    expect(model.liveSeekWindow).toBeNull();
  });

  it('normalizes recording snapshots to VOD and clears live seek state', () => {
    const model = buildPlayerSessionSnapshotModel(session({
      mode: 'RECORDING',
      windowKind: 'unknown',
    }), 123);

    expect(model.sessionWindowKind).toBe('vod');
    expect(model.liveSeekWindow).toBeNull();
  });

  it('preserves the current live seek window for snapshots without a known playback mode', () => {
    const model = buildPlayerSessionSnapshotModel(session({
      windowKind: 'unknown',
      durationSeconds: 30,
      liveEdgeSeconds: 100,
    }), 123);

    expect(model.sessionWindowKind).toBe('unknown');
    expect(model.liveSeekWindow).toBeUndefined();
  });
});
