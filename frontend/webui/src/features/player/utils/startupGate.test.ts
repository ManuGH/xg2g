import { describe, expect, it } from 'vitest';

import { shouldOpenStartGate, type StartGateInput } from './startupGate';

function input(overrides: Partial<StartGateInput> = {}): StartGateInput {
  return {
    isLive: true,
    bufferedAheadSeconds: 0,
    capElapsed: false,
    targetSeconds: 4.5,
    ...overrides,
  };
}

describe('shouldOpenStartGate', () => {
  it('holds a live start until the headroom target is reached', () => {
    expect(shouldOpenStartGate(input({ bufferedAheadSeconds: 0 }))).toBe(false);
    expect(shouldOpenStartGate(input({ bufferedAheadSeconds: 4.49 }))).toBe(false);
  });

  it('opens a live start exactly at the headroom target', () => {
    expect(shouldOpenStartGate(input({ bufferedAheadSeconds: 4.5 }))).toBe(true);
    expect(shouldOpenStartGate(input({ bufferedAheadSeconds: 9 }))).toBe(true);
  });

  // The regression this module exists for. The previous inline gate re-checked
  // a hardcoded buffer threshold on EVERY trigger including the timeout, so the
  // cap could never fire and a stalled live stream span forever at a spinner.
  it('opens once the cap elapses even with no buffer at all', () => {
    expect(shouldOpenStartGate(input({ capElapsed: true, bufferedAheadSeconds: 0 }))).toBe(true);
  });

  it('opens once the cap elapses on a permanently stalled live stream', () => {
    expect(shouldOpenStartGate(input({ capElapsed: true, bufferedAheadSeconds: 2 }))).toBe(true);
  });

  // The previous gate had a dead band: a 4.5s target announced by the caller,
  // vetoed by a 5.0s check inside the gate. A buffer parked between the two
  // never opened it.
  it('has no dead band between the announced target and the gate check', () => {
    for (const bufferedAheadSeconds of [4.5, 4.6, 4.8, 4.9, 4.99, 5.0]) {
      expect(shouldOpenStartGate(input({ bufferedAheadSeconds }))).toBe(true);
    }
  });

  it('never holds a VOD start, whatever the buffer says', () => {
    expect(shouldOpenStartGate(input({ isLive: false, bufferedAheadSeconds: 0 }))).toBe(true);
    expect(shouldOpenStartGate(input({ isLive: false, bufferedAheadSeconds: 1 }))).toBe(true);
  });

  it('respects a caller-supplied target rather than any built-in constant', () => {
    expect(shouldOpenStartGate(input({ targetSeconds: 2, bufferedAheadSeconds: 2 }))).toBe(true);
    expect(shouldOpenStartGate(input({ targetSeconds: 8, bufferedAheadSeconds: 5 }))).toBe(false);
  });
});
