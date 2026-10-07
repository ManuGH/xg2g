import { describe, expect, it } from 'vitest';
import { decideForegroundResume, LIVE_FOREGROUND_REATTACH_MS } from './foregroundResume';

const base = {
  wasHidden: true,
  isPiP: false,
  status: 'playing',
  userPaused: false,
  hasTerminal: false,
};

describe('decideForegroundResume', () => {
  it('does nothing without a genuine hidden->visible edge', () => {
    expect(decideForegroundResume({ ...base, wasHidden: false })).toBe('none');
  });

  it('does nothing while in picture-in-picture', () => {
    expect(decideForegroundResume({ ...base, isPiP: true })).toBe('none');
  });

  it('re-establishes a reaped session (status error) BEFORE the terminal bail', () => {
    expect(decideForegroundResume({ ...base, status: 'error', hasTerminal: true })).toBe('retry');
  });

  it('does not auto-resume a user-paused stream', () => {
    expect(decideForegroundResume({ ...base, userPaused: true })).toBe('none');
  });

  it('does not play a terminal (stopped) stream', () => {
    expect(decideForegroundResume({ ...base, status: 'stopped', hasTerminal: true })).toBe('none');
  });

  it('plays a healthy backgrounded stream on return', () => {
    expect(decideForegroundResume(base)).toBe('play');
    expect(decideForegroundResume({ ...base, status: 'paused' })).toBe('play');
  });

  describe('live native HLS after a long background', () => {
    const live = { ...base, isLive: true, isNative: true, hiddenMs: LIVE_FOREGROUND_REATTACH_MS };

    it('rejoins the live edge instead of resuming a stale position', () => {
      expect(decideForegroundResume(live)).toBe('reattach');
      expect(decideForegroundResume({ ...live, status: 'paused' })).toBe('reattach');
    });

    it('keeps the plain resume for a short background', () => {
      expect(decideForegroundResume({ ...live, hiddenMs: LIVE_FOREGROUND_REATTACH_MS - 1 })).toBe('play');
    });

    it('keeps the plain resume for VOD/recordings and for hls.js', () => {
      expect(decideForegroundResume({ ...live, isLive: false })).toBe('play');
      expect(decideForegroundResume({ ...live, isNative: false })).toBe('play');
    });

    it('still honours user pause, PiP, terminal states and reaped sessions first', () => {
      expect(decideForegroundResume({ ...live, userPaused: true })).toBe('none');
      expect(decideForegroundResume({ ...live, isPiP: true })).toBe('none');
      expect(decideForegroundResume({ ...live, status: 'stopped', hasTerminal: true })).toBe('none');
      expect(decideForegroundResume({ ...live, status: 'error', hasTerminal: true })).toBe('retry');
    });
  });
});
