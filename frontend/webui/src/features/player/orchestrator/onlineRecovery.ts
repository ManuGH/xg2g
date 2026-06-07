// Pure decision for "what to do when the browser regains connectivity after it
// was lost." Flaky web — mobile data, wifi handoffs, laptop sleep/wake — drops
// the connection, and today nothing reconnects unless the user switches tabs
// (foregroundResume) or hits Retry by hand. This picks the recovery action on
// the offline->online edge. Kept pure (no DOM/refs) so it is unit-tested; the
// effect in usePlaybackOrchestrator wires the side effects. It mirrors
// decideForegroundResume so the two recovery edges stay consistent.

export type OnlineRecoveryAction = 'retry' | 'play' | 'none';

export interface OnlineRecoveryInput {
  /** True only on a genuine offline -> online transition (not initial mount). */
  wasOffline: boolean;
  /** There is a stream to recover; if nothing was playing, do nothing. */
  hasActiveSession: boolean;
  /** Current player status. 'error' means the session was reaped/failed. */
  status: string;
  /** The user deliberately paused — never auto-resume. */
  userPaused: boolean;
  /** Status is terminal (stopped/idle/error). */
  hasTerminal: boolean;
}

export function decideOnlineRecovery(input: OnlineRecoveryInput): OnlineRecoveryAction {
  if (!input.wasOffline) {
    return 'none';
  }
  if (!input.hasActiveSession) {
    return 'none';
  }
  // A reaped/failed session surfaced as status 'error' must be handled BEFORE
  // the terminal bail — 'error' is terminal, but here we want to re-establish
  // it, not give up.
  if (input.status === 'error') {
    return 'retry';
  }
  if (input.userPaused || input.hasTerminal) {
    return 'none';
  }
  return 'play';
}
