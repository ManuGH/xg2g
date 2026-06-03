import { describe, expect, it } from 'vitest';
import { resumeRecoverySettled, RESUME_RECOVERY_MAX_ATTEMPTS } from './resumePlaybackRecovery';

const MAX = RESUME_RECOVERY_MAX_ATTEMPTS;

describe('resumeRecoverySettled', () => {
  // The core of the fix: a stuck stream (currentTime not advancing) with attempts
  // remaining must NOT settle — the loop keeps nudging play(). A regression that
  // settled here immediately is exactly the prior single-play() behaviour that left
  // the frame black; this assertion goes red on that regression.
  it('keeps nudging while the stream is stuck and attempts remain', () => {
    expect(resumeRecoverySettled(12.0, 12.0, false, 1, MAX)).toBe(false);
  });

  it('settles once the stream advances (the decoder accepted the resume)', () => {
    expect(resumeRecoverySettled(12.0, 12.5, false, 1, MAX)).toBe(true);
  });

  it('does not treat sub-epsilon jitter as real progress', () => {
    expect(resumeRecoverySettled(12.0, 12.005, false, 1, MAX)).toBe(false);
  });

  it('gives up after the bounded attempts even if still stuck', () => {
    expect(resumeRecoverySettled(12.0, 12.0, false, MAX, MAX)).toBe(true);
  });

  it('settles when the media has ended', () => {
    expect(resumeRecoverySettled(12.0, 12.0, true, 1, MAX)).toBe(true);
  });
});
