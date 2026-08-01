import { describe, expect, it } from 'vitest';
import { shouldWatchForNetworkRecovery } from './useNetworkRecoveryWatchdog';

function failure(overrides: Partial<Parameters<typeof shouldWatchForNetworkRecovery>[0] & object> = {}) {
  return {
    class: 'media',
    code: 'PLAYBACK_FAILURE',
    terminal: false,
    status: null as number | null,
    ...overrides,
  };
}

describe('shouldWatchForNetworkRecovery', () => {
  it('watches when the request never reached the server', () => {
    expect(shouldWatchForNetworkRecovery(failure({ status: null }))).toBe(true);
  });

  it('watches a stream that starved even though the server answered', () => {
    expect(shouldWatchForNetworkRecovery(failure({
      status: 404,
      code: 'HLS_NETWORK_RETRIES_EXHAUSTED',
    }))).toBe(true);
  });

  // If the server answered, connectivity is not the problem. Polling until it
  // is reachable would retry a fault that is not going to clear — a deleted
  // recording stays deleted, a failed packager stays failed.
  it.each([410, 409, 500])('does not watch a failure the server answered with (%i)', (status) => {
    expect(shouldWatchForNetworkRecovery(failure({ status, class: 'session' }))).toBe(false);
  });

  it('never watches auth or terminal failures', () => {
    expect(shouldWatchForNetworkRecovery(failure({ class: 'auth' }))).toBe(false);
    expect(shouldWatchForNetworkRecovery(failure({ terminal: true }))).toBe(false);
  });

  it('does nothing without a failure', () => {
    expect(shouldWatchForNetworkRecovery(null)).toBe(false);
  });
});
