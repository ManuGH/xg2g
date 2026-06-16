import { describe, expect, it } from 'vitest';
import {
  DEFAULT_RENDER_QUALITY_THRESHOLDS,
  describeHlsRenderProbe,
  evaluateRenderQuality,
  isBlackRenderSuspect,
  type HlsRenderProbeSnapshot,
} from './playbackRenderProbe';

function snapshot(overrides: Partial<HlsRenderProbeSnapshot> = {}): HlsRenderProbeSnapshot {
  return {
    currentTime: 12,
    readyState: 4,
    networkState: 2,
    videoWidth: 1280,
    videoHeight: 720,
    paused: false,
    bufferedAhead: 18,
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
});

describe('evaluateRenderQuality', () => {
  it('flags a window whose dropped ratio is at or above the threshold', () => {
    const verdict = evaluateRenderQuality(
      { totalFrames: 1000, droppedFrames: 10 },
      { totalFrames: 1500, droppedFrames: 60 }, // 50 dropped / 500 decoded = 10%
    );
    expect(verdict.totalDelta).toBe(500);
    expect(verdict.droppedDelta).toBe(50);
    expect(verdict.ratio).toBeCloseTo(0.1);
    expect(verdict.exceeded).toBe(true);
  });

  it('does not flag a smooth window', () => {
    const verdict = evaluateRenderQuality(
      { totalFrames: 1000, droppedFrames: 10 },
      { totalFrames: 1500, droppedFrames: 14 }, // 4 / 500 = 0.8%
    );
    expect(verdict.exceeded).toBe(false);
  });

  it('withholds a verdict until enough frames have been decoded', () => {
    const verdict = evaluateRenderQuality(
      { totalFrames: 1000, droppedFrames: 0 },
      { totalFrames: 1050, droppedFrames: 40 }, // 80% but only 50 frames < minTotalFrames
    );
    expect(verdict.exceeded).toBe(false);
    expect(verdict.ratio).toBeNull();
  });

  it('returns no verdict when frame counters are unavailable', () => {
    const verdict = evaluateRenderQuality(
      { totalFrames: null, droppedFrames: null },
      { totalFrames: null, droppedFrames: null },
    );
    expect(verdict.exceeded).toBe(false);
    expect(verdict.ratio).toBeNull();
  });

  it('ignores counter resets (negative delta) rather than reporting a bogus ratio', () => {
    const verdict = evaluateRenderQuality(
      { totalFrames: 5000, droppedFrames: 100 },
      { totalFrames: 200, droppedFrames: 2 }, // counters reset on a new media element
    );
    expect(verdict.exceeded).toBe(false);
  });

  it('exposes sane defaults', () => {
    expect(DEFAULT_RENDER_QUALITY_THRESHOLDS.maxDroppedRatio).toBeGreaterThan(0);
    expect(DEFAULT_RENDER_QUALITY_THRESHOLDS.minTotalFrames).toBeGreaterThan(0);
  });
});
