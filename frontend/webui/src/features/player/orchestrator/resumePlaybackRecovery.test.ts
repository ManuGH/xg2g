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

  it('never touches a clean resume: no play() if the stream advances during the window', () => {
    const v = fakeVideo(10);
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250 });
    v.currentTime = 10.5; // recovered on its own during the observation window
    vi.advanceTimersByTime(400);
    expect(v.play).not.toHaveBeenCalled();
  });

  it('nudges play() only AFTER the window confirms the stream is stuck, then stops once it advances', () => {
    const v = fakeVideo(10);
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250, maxAttempts: 8 });
    vi.advanceTimersByTime(400); // still stuck at 10 -> intervene
    expect(v.play).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(250); // still stuck -> retry
    expect(v.play).toHaveBeenCalledTimes(2);
    v.currentTime = 11; // decoder accepted the resume
    vi.advanceTimersByTime(250);
    expect(v.play).toHaveBeenCalledTimes(2); // settled, no further nudges
  });

  it('respects a user pause that happens MID-recovery (shouldContinue=false) — manual pause stays sacred', () => {
    const v = fakeVideo(10);
    let userPaused = false;
    startResumePlaybackRecovery(v, { observeMs: 400, intervalMs: 250, shouldContinue: () => !userPaused });
    vi.advanceTimersByTime(400);
    expect(v.play).toHaveBeenCalledTimes(1);
    userPaused = true; // the user pauses while we were recovering
    vi.advanceTimersByTime(250);
    expect(v.play).toHaveBeenCalledTimes(1); // stopped — never overrides the user
  });

  it('cancel() aborts the loop (page hidden again mid-recovery)', () => {
    const v = fakeVideo(10);
    const cancel = startResumePlaybackRecovery(v, { observeMs: 400 });
    cancel();
    vi.advanceTimersByTime(2000);
    expect(v.play).not.toHaveBeenCalled();
  });
});
