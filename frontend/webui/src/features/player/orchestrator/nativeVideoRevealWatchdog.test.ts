import { describe, expect, it } from 'vitest';
import {
  NATIVE_VIDEO_WATCHDOG_MIN_ADVANCE_SECONDS,
  shouldForceRevealNativeVideo,
} from './nativePlaybackHelpers';

describe('shouldForceRevealNativeVideo', () => {
  it('reveals when the element is playing and frames are advancing', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 4, advancedSeconds: 0.5 }),
    ).toBe(true);
  });

  it('reveals at the device-confirmed failure state (playing, readyState 4, hidden)', () => {
    // The exact state captured on the box: video plays healthy but stayed hidden.
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 4, advancedSeconds: 0.49 }),
    ).toBe(true);
  });

  it('keeps the veil up during a genuine rebuffer (currentTime frozen)', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 1, advancedSeconds: 0 }),
    ).toBe(false);
  });

  it('does not reveal when frames are not yet advancing', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 4, advancedSeconds: 0.05 }),
    ).toBe(false);
  });

  it('never reveals while paused (user-pause shows the frozen frame separately)', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: true, readyState: 4, advancedSeconds: 1 }),
    ).toBe(false);
  });

  it('requires decodable data (readyState >= 3)', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 2, advancedSeconds: 0.5 }),
    ).toBe(false);
  });

  it('honours the threshold boundary exactly', () => {
    expect(
      shouldForceRevealNativeVideo({
        paused: false,
        readyState: 3,
        advancedSeconds: NATIVE_VIDEO_WATCHDOG_MIN_ADVANCE_SECONDS,
      }),
    ).toBe(true);
  });
});
