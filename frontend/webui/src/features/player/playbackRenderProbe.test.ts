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
});
