import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  resumeStreamRecovered,
  resumeRecoverySettled,
  startResumePlaybackRecovery,
  RESUME_RECOVERY_MAX_ATTEMPTS,
} from './resumePlaybackRecovery';

const MAX = RESUME_RECOVERY_MAX_ATTEMPTS;

describe('resumeStreamRecovered', () => {
  it('is recovered once currentTime advances past epsilon', () => {
    expect(resumeStreamRecovered(12.0, 12.5, false)).toBe(true);
  });
  it('is NOT recovered while the stream is stuck (drives the intervene decision)', () => {
    expect(resumeStreamRecovered(12.0, 12.0, false)).toBe(false);
  });
  it('does not treat sub-epsilon jitter as progress', () => {
    expect(resumeStreamRecovered(12.0, 12.005, false)).toBe(false);
  });
  it('is recovered when the media ended', () => {
    expect(resumeStreamRecovered(12.0, 12.0, true)).toBe(true);
  });
});

describe('resumeRecoverySettled', () => {
  // The core of the fix: a stuck stream with attempts remaining must NOT settle —
  // the loop keeps nudging. A regression that settled immediately is exactly the
  // prior single-play() behaviour that left the frame black; this goes red on it.
  it('keeps nudging while the stream is stuck and attempts remain', () => {
    expect(resumeRecoverySettled(12.0, 12.0, false, 1, MAX)).toBe(false);
  });
  it('settles once the stream advances', () => {
    expect(resumeRecoverySettled(12.0, 12.5, false, 1, MAX)).toBe(true);
  });
  it('gives up after the bounded attempts even if still stuck', () => {
    expect(resumeRecoverySettled(12.0, 12.0, false, MAX, MAX)).toBe(true);
  });
});

describe('startResumePlaybackRecovery (observe-first)', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  function fakeVideo(initial: number) {
    return { currentTime: initial, ended: false, play: vi.fn(() => Promise.resolve()) } as unknown as HTMLVideoElement & {
      play: ReturnType<typeof vi.fn>;
    };
  }

  it('issues initial nudge but stops when stream recovers during observation window', () => {
    const v = fakeVideo(10);
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250 });
    // One immediate play() is always issued (React cleanup cannot suppress it).
    expect(v.play).toHaveBeenCalledTimes(1);
    v.currentTime = 10.5; // recovered on its own during the observation window
    vi.advanceTimersByTime(400);
    expect(v.play).toHaveBeenCalledTimes(1); // no follow-up nudges
  });

  it('continues nudging after the observation window when the stream is still stuck, then stops once it advances', () => {
    const v = fakeVideo(10);
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250, maxAttempts: 8 });
    // 1 immediate play + 1 at observation timeout + 1 after interval = 3 before recovery
    vi.advanceTimersByTime(400); // still stuck at 10 -> nudge again
    expect(v.play).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(250); // still stuck -> retry
    expect(v.play).toHaveBeenCalledTimes(3);
    v.currentTime = 11; // decoder accepted the resume
    vi.advanceTimersByTime(250);
    expect(v.play).toHaveBeenCalledTimes(3); // settled, no further nudges
  });

  it('respects a user pause that happens MID-recovery (shouldContinue=false) — manual pause stays sacred', () => {
    const v = fakeVideo(10);
    let userPaused = false;
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250, shouldContinue: () => !userPaused });
    // 1 immediate play is always issued
    expect(v.play).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(400);
    expect(v.play).toHaveBeenCalledTimes(2); // observation window still stuck -> nudge
    userPaused = true; // the user pauses while we were recovering
    vi.advanceTimersByTime(250);
    expect(v.play).toHaveBeenCalledTimes(2); // stopped — never overrides the user
  });

  it('cancel() stops subsequent nudges after the initial play (page hidden again mid-recovery)', () => {
    const v = fakeVideo(10);
    const cancel = startResumePlaybackRecovery(v, { observeMs: 400 });
    // 1 immediate play happens synchronously before the cancel function is returned.
    cancel();
    vi.advanceTimersByTime(2000);
    expect(v.play).toHaveBeenCalledTimes(1); // only the synchronous first nudge
  });

  it('does not count attempts while video.readyState < 2 (waiting for initial buffer on foreground return)', () => {
    const v = { currentTime: 0, readyState: 1, ended: false, play: vi.fn(() => Promise.resolve()) } as unknown as HTMLVideoElement & {
      play: ReturnType<typeof vi.fn>;
      readyState: number;
    };
    const onFailed = vi.fn();
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250, maxAttempts: 8, onFailed });

    expect(v.play).toHaveBeenCalledTimes(1);

    // Advance past the normal 8-attempt ceiling (400ms + 8 * 250ms = 2400ms)
    vi.advanceTimersByTime(2400);

    // Should NOT fail because readyState is 1 (< 2, waiting for data)
    expect(onFailed).not.toHaveBeenCalled();

    // Data arrives and playback starts
    v.readyState = 4;
    v.currentTime = 0.5;
    vi.advanceTimersByTime(250);

    expect(onFailed).not.toHaveBeenCalled();
  });
});
