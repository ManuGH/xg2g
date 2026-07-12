// Recovery ladder: the orchestrator-level escalation ABOVE the engine's own
// in-place recoveries (hls.js startLoad/recoverMediaError, native stall
// recovery, session decode recovery). Those run first; when they exhaust, a
// failure surfaces here and the ladder decides the last automatic step —
// one restart with the lightweight 'repair' request profile — before giving
// the error to the user. Pure decision logic; the orchestrator owns timing.

import type { PlaybackFailure } from './playbackTypes';

export type RecoveryEscalation =
  /** Engine/backend already handles it, or the failure is fresh enough to leave alone. */
  | 'none'
  /** Restart the whole attempt once with the forced 'repair' request profile. */
  | 'restart_with_fallback_profile'
  /** All automatic steps are spent — surface the error. */
  | 'give_up';

export interface RecoveryLadderState {
  /** The one-shot profile fallback has been consumed for this viewing intent. */
  autoFallbackUsed: boolean;
}

export function createRecoveryLadderState(): RecoveryLadderState {
  return { autoFallbackUsed: false };
}

export interface RecoveryEscalationInput {
  failure: Pick<PlaybackFailure, 'class' | 'source' | 'terminal' | 'retryable' | 'recoverable'>;
  /** User pinned a profile in the UI — automatic profile changes would override intent. */
  explicitProfilePinned: boolean;
  /** Whether the orchestrator is running an active playback intent (channel or recording) vs static src. */
  hasActiveIntent?: boolean;
  state: RecoveryLadderState;
}

export function decideRecoveryEscalation({
  failure,
  explicitProfilePinned,
  hasActiveIntent = true,
  state,
}: RecoveryEscalationInput): RecoveryEscalation {
  // Terminal, auth and session-policy failures are not transcoder problems —
  // restarting with a lighter profile cannot fix a 401 or an occupied lease.
  if (failure.terminal || failure.class === 'auth' || failure.class === 'session') {
    return 'give_up';
  }

  // Only escalate for playback-runtime failures. Backend start failures keep
  // their own retry semantics (Retry-After loops) and stay untouched.
  if (failure.source !== 'media-element') {
    return 'none';
  }

  if (!failure.retryable && !failure.recoverable) {
    return 'give_up';
  }

  if (explicitProfilePinned) {
    return 'none';
  }

  if (!hasActiveIntent) {
    return 'give_up';
  }

  if (state.autoFallbackUsed) {
    return 'give_up';
  }

  return 'restart_with_fallback_profile';
}
