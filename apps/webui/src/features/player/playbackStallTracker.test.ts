import { describe, expect, it } from 'vitest';
import { createPlaybackStallTracker } from './playbackStallTracker';

function clock(start = 1000) {
  let t = start;
  return {
    now: () => t,
    advance: (ms: number) => {
      t += ms;
    },
  };
}

describe('playbackStallTracker', () => {
  it('counts each stall once and sums its duration when playback resumes', () => {
    const c = clock();
    const tracker = createPlaybackStallTracker(c.now);
    tracker.ensureSession('s1');

    tracker.stallStarted();
    c.advance(400);
    tracker.stallStarted(); // 'stalled' after 'waiting' is the same stall
    c.advance(600);
    tracker.stallEnded();
    tracker.stallEnded(); // a second 'playing' closes nothing
    c.advance(5000);
    tracker.stallStarted();
    c.advance(250);
    tracker.stallEnded();

    expect(tracker.snapshot('s1')).toMatchObject({ stalls: 2, stallMs: 1250 });
  });

  it('includes a stall still in progress in the snapshot without closing it', () => {
    const c = clock();
    const tracker = createPlaybackStallTracker(c.now);
    tracker.ensureSession('s1');

    tracker.stallStarted();
    c.advance(700);
    expect(tracker.snapshot('s1').stallMs).toBe(700);
    c.advance(300);
    tracker.stallEnded();
    expect(tracker.snapshot('s1')).toMatchObject({ stalls: 1, stallMs: 1000 });
  });

  it('counts every hls.js buffer stall detail and ignores all others', () => {
    const tracker = createPlaybackStallTracker(clock().now);
    tracker.ensureSession('s1');

    for (const details of [
      'bufferSeekOverHole',
      'bufferSeekOverHole',
      'bufferSeekOverHole',
      'bufferNudgeOnStall',
      'bufferStalledError',
      'bufferAppendError',
      'fragLoadError',
      'levelLoadTimeOut',
      undefined,
    ]) {
      tracker.hlsNonFatal(details);
    }

    expect(tracker.snapshot('s1')).toEqual({
      stalls: 0,
      stallMs: 0,
      holeJumps: 3,
      nudges: 1,
      stallErrors: 1,
      appendErrors: 1,
    });
  });

  it('starts from zero for a new session and never reports another session\'s counts', () => {
    const c = clock();
    const tracker = createPlaybackStallTracker(c.now);
    tracker.ensureSession('s1');
    tracker.hlsNonFatal('bufferSeekOverHole');
    tracker.stallStarted();

    tracker.ensureSession('s2');
    c.advance(900);
    expect(tracker.snapshot('s2')).toMatchObject({ stalls: 0, stallMs: 0, holeJumps: 0 });
    expect(tracker.snapshot('s1')).toMatchObject({ stalls: 0, holeJumps: 0 });

    tracker.ensureSession('s2'); // same session: nothing is reset
    tracker.hlsNonFatal('bufferNudgeOnStall');
    tracker.ensureSession('s2');
    expect(tracker.snapshot('s2').nudges).toBe(1);
  });
});
