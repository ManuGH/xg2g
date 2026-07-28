import { describe, expect, it } from 'vitest';
import {
  NATIVE_VIDEO_WATCHDOG_MIN_ADVANCE_SECONDS,
  shouldForceRevealNativeVideo,
  isInMemorySeekTarget,
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

  it('trusts sustained playhead advancement when Safari leaves readyState below 3', () => {
    expect(
      shouldForceRevealNativeVideo({ paused: false, readyState: 2, advancedSeconds: 0.5 }),
    ).toBe(true);
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

describe('isInMemorySeekTarget', () => {
  it('treats a buffered, decodable, playing target as in-memory (no veil)', () => {
    expect(
      isInMemorySeekTarget({ paused: false, readyState: 4, bufferedAheadSeconds: 12 }),
    ).toBe(true);
  });

  it('veils when the target is not buffered (headroom <= 0.5)', () => {
    expect(
      isInMemorySeekTarget({ paused: false, readyState: 4, bufferedAheadSeconds: 0 }),
    ).toBe(false);
    expect(
      isInMemorySeekTarget({ paused: false, readyState: 4, bufferedAheadSeconds: 0.5 }),
    ).toBe(false);
  });

  it('veils when the element is not yet decodable (readyState < 3)', () => {
    expect(
      isInMemorySeekTarget({ paused: false, readyState: 2, bufferedAheadSeconds: 12 }),
    ).toBe(false);
  });

  it('veils while paused', () => {
    expect(
      isInMemorySeekTarget({ paused: true, readyState: 4, bufferedAheadSeconds: 12 }),
    ).toBe(false);
  });

  it('reveals just past the headroom threshold', () => {
    expect(
      isInMemorySeekTarget({ paused: false, readyState: 3, bufferedAheadSeconds: 0.51 }),
    ).toBe(true);
  });
});
