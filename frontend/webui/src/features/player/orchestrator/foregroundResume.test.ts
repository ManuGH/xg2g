import { describe, expect, it } from 'vitest';
import { decideForegroundResume } from './foregroundResume';

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
});
