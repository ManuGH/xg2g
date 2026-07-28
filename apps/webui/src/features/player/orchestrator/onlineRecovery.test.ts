import { describe, expect, it } from 'vitest';
import { decideOnlineRecovery } from './onlineRecovery';

const base = {
  wasOffline: true,
  hasActiveSession: true,
  status: 'playing',
  userPaused: false,
  hasTerminal: false,
};

describe('decideOnlineRecovery', () => {
  it('does nothing without a genuine offline->online edge', () => {
    expect(decideOnlineRecovery({ ...base, wasOffline: false })).toBe('none');
  });

  it('does nothing when there was no active session to recover', () => {
    expect(decideOnlineRecovery({ ...base, hasActiveSession: false })).toBe('none');
  });

  it('re-establishes a reaped session (status error) BEFORE the terminal bail', () => {
    expect(decideOnlineRecovery({ ...base, status: 'error', hasTerminal: true })).toBe('retry');
  });

  it('does not auto-resume a user-paused stream', () => {
    expect(decideOnlineRecovery({ ...base, userPaused: true })).toBe('none');
  });

  it('does not play a terminal (stopped) stream', () => {
    expect(decideOnlineRecovery({ ...base, status: 'stopped', hasTerminal: true })).toBe('none');
  });

  it('nudges a still-alive stream back to play on reconnect', () => {
    expect(decideOnlineRecovery(base)).toBe('play');
  });

  // Negative control: a non-error active stream on the offline->online edge must
  // resolve to 'play', NOT 'retry' — we only tear down + re-establish when the
  // session was actually reaped (status 'error'). This pins that boundary so a
  // future change can't silently turn every reconnect into a full restart.
  it('does not retry a healthy stream (retry is reserved for reaped sessions)', () => {
    expect(decideOnlineRecovery({ ...base, status: 'buffering' })).not.toBe('retry');
  });
});
