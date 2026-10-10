import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { watchForMovingPictures } from './presentedFrameWatch';

type FrameCallback = (now: number, metadata: { mediaTime?: number }) => void;

function videoWithFrameCallbacks() {
  const video = document.createElement('video');
  let pending: FrameCallback | null = null;
  Object.assign(video, {
    requestVideoFrameCallback: vi.fn((cb: FrameCallback) => {
      pending = cb;
      return 1;
    }),
    cancelVideoFrameCallback: vi.fn(() => {
      pending = null;
    }),
  });
  const present = (mediaTime?: number) => {
    const cb = pending;
    pending = null;
    cb?.(0, mediaTime === undefined ? {} : { mediaTime });
  };
  return { video, present };
}

function videoWithFrameCounter(start: number) {
  const video = document.createElement('video');
  let total = start;
  Object.defineProperty(video, 'getVideoPlaybackQuality', {
    configurable: true,
    value: () => ({ totalVideoFrames: total, droppedVideoFrames: 0 }),
  });
  return { video, advance: (frames: number) => { total += frames; } };
}

describe('watchForMovingPictures', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('accepts frames whose media time advances', () => {
    const { video, present } = videoWithFrameCallbacks();
    const onMissing = vi.fn();
    watchForMovingPictures(video, 2_500, onMissing);
    present(10.0);
    present(10.04);
    vi.advanceTimersByTime(5_000);
    expect(onMissing).not.toHaveBeenCalled();
  });

  it('reports a picture that never comes back', () => {
    const { video } = videoWithFrameCallbacks();
    const onMissing = vi.fn();
    watchForMovingPictures(video, 2_500, onMissing);
    vi.advanceTimersByTime(2_499);
    expect(onMissing).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(onMissing).toHaveBeenCalledTimes(1);
  });

  it('does not count a re-presented still frame as moving', () => {
    const { video, present } = videoWithFrameCallbacks();
    const onMissing = vi.fn();
    watchForMovingPictures(video, 2_500, onMissing);
    present(10.0);
    present(10.0);
    present(10.0);
    vi.advanceTimersByTime(2_500);
    expect(onMissing).toHaveBeenCalledTimes(1);
  });

  it('falls back to the frame counter without frame callbacks', () => {
    const { video, advance } = videoWithFrameCounter(100);
    const onMissing = vi.fn();
    watchForMovingPictures(video, 2_500, onMissing);
    advance(3);
    vi.advanceTimersByTime(5_000);
    expect(onMissing).not.toHaveBeenCalled();
  });

  it('reports a stalled frame counter', () => {
    const { video } = videoWithFrameCounter(100);
    const onMissing = vi.fn();
    watchForMovingPictures(video, 2_500, onMissing);
    vi.advanceTimersByTime(2_500);
    expect(onMissing).toHaveBeenCalledTimes(1);
  });

  it('reports at once when nothing can be observed', () => {
    const onMissing = vi.fn();
    watchForMovingPictures(document.createElement('video'), 2_500, onMissing);
    expect(onMissing).toHaveBeenCalledTimes(1);
  });

  it('stays silent once cancelled', () => {
    const { video } = videoWithFrameCallbacks();
    const onMissing = vi.fn();
    const cancel = watchForMovingPictures(video, 2_500, onMissing);
    cancel();
    vi.advanceTimersByTime(5_000);
    expect(onMissing).not.toHaveBeenCalled();
    expect((video as unknown as { cancelVideoFrameCallback: ReturnType<typeof vi.fn> }).cancelVideoFrameCallback).toHaveBeenCalled();
  });
});
