import { describe, expect, it } from 'vitest';
import { describeHlsRenderProbe, isBlackRenderSuspect, type HlsRenderProbeSnapshot } from './playbackRenderProbe';

function snapshot(overrides: Partial<HlsRenderProbeSnapshot> = {}): HlsRenderProbeSnapshot {
  return {
    currentTime: 12,
    readyState: 4,
    networkState: 2,
    videoWidth: 1280,
    videoHeight: 720,
    paused: false,
    bufferedAhead: 18,
    playbackRate: 1,
    totalFrames: 0,
    droppedFrames: 0,
    ...overrides,
  };
}

describe('playbackRenderProbe', () => {
  it('flags progressed playback without frame growth as a black-render suspect', () => {
    const started = snapshot();
    const settled = snapshot({
      currentTime: 14.6,
      bufferedAhead: 15.4,
      totalFrames: 0,
    });

    expect(isBlackRenderSuspect(started, settled)).toBe(true);
    expect(describeHlsRenderProbe('black_suspect', settled, started)).toContain('df=0');
  });

  it('keeps progressed playback with frame growth on the stable path', () => {
    const started = snapshot();
    const settled = snapshot({
      currentTime: 14.6,
      bufferedAhead: 15.4,
      totalFrames: 96,
      droppedFrames: 1,
    });

    expect(isBlackRenderSuspect(started, settled)).toBe(false);
    expect(describeHlsRenderProbe('stable', settled, started)).toContain('frames=96');
  });

  it('reports buffer shape and every stall and hole with per-beat deltas', () => {
    const counters = {
      stalls: 1,
      stallMs: 400,
      holeJumps: 2,
      nudges: 0,
      stallErrors: 1,
      appendErrors: 0,
    };
    const previous = snapshot({ stalls: counters });
    const beat = snapshot({
      currentTime: 42,
      bufferedAhead: 0.95,
      bufferedRanges: 3,
      bufferedTail: 7.5,
      liveLatency: 4.25,
      stalls: { ...counters, stalls: 4, stallMs: 1900, holeJumps: 9, nudges: 2 },
    });

    const line = describeHlsRenderProbe('heartbeat', beat, previous);

    expect(line).toContain('buf=0.95');
    expect(line).toContain('ranges=3');
    expect(line).toContain('tail=7.50');
    expect(line).toContain('lat=4.25');
    expect(line).toContain('stalls=4 stall_ms=1900 holes=9 nudges=2 stall_errs=1 append_errs=0');
    expect(line).toContain('dstalls=3 dstall_ms=1500 dholes=7 dnudges=2 dstall_errs=0');
  });

  it('leaves the line unchanged for snapshots without buffer shape or counters', () => {
    const line = describeHlsRenderProbe('heartbeat', snapshot({ currentTime: 42 }), snapshot());

    expect(line).toBe(
      'hlsjs_render stage=heartbeat t=42.00 rs=4 ns=2 paused=0 dims=1280x720 buf=18.00 rate=1.00 frames=0 drop=0 dt=30.00 df=0',
    );
  });

  it('marks an unknown live latency explicitly instead of omitting it', () => {
    const line = describeHlsRenderProbe('heartbeat', snapshot({ liveLatency: null }));

    expect(line).toContain('lat=na');
  });
});
