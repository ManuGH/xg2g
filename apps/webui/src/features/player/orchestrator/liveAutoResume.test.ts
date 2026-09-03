import { describe, expect, it } from 'vitest';
import { decideLiveAutoResume, type LiveAutoResumeInput } from './liveAutoResume';

describe('decideLiveAutoResume', () => {
  const base: LiveAutoResumeInput = {
    isLiveMode: true,
    isPaused: true,
    userPaused: false,
    hasTerminal: false,
  };

  it('returns play when a live stream is paused without user intent', () => {
    expect(decideLiveAutoResume(base)).toBe('play');
  });

  it('returns none when not in live mode', () => {
    expect(decideLiveAutoResume({ ...base, isLiveMode: false })).toBe('none');
  });

  it('returns none when the video is already playing (not paused)', () => {
    expect(decideLiveAutoResume({ ...base, isPaused: false })).toBe('none');
  });

  it('returns none when the user deliberately paused the playback', () => {
    expect(decideLiveAutoResume({ ...base, userPaused: true })).toBe('none');
  });

  it('returns none when the status is terminal (stopped/error/idle)', () => {
    expect(decideLiveAutoResume({ ...base, hasTerminal: true })).toBe('none');
  });
});
