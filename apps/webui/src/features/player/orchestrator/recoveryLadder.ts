// Recovery ladder: the orchestrator-level escalation ABOVE the engine's own
// in-place recoveries (hls.js startLoad/recoverMediaError, native stall
// recovery, session decode recovery). Those run first; when they exhaust, a
// failure surfaces here and the ladder decides the last automatic steps before
// giving the error to the user. Pure decision logic; the orchestrator owns
// timing.
//
// The ladder distinguishes WHY playback died, because the cheapest correct
// answer differs per cause and the output carries a single rendition — every
// restart is a fresh encoder session, so picking the wrong one costs a
// transcode and usually fails again:
//
//   * decode failures  -> restart on the 'repair' intent (High + Transcode)
//   * network failures -> restart pinned to the safe bandwidth rung; asking a
//     starved link for MORE bitrate, which 'repair' does, is the one thing
//     guaranteed not to work
//   * a reaped lease   -> re-establish the session as-is; the encode was fine,
//     the client just lost the network long enough for the lease to lapse

import type { PlaybackFailure } from './playbackTypes';

export type RecoveryEscalation =
  /** Engine/backend already handles it, or the failure is fresh enough to leave alone. */
  | 'none'
  /** Restart the whole attempt once with the forced 'repair' request profile. */
  | 'restart_with_fallback_profile'
  /** Restart with the automatic profile held down at the safe bandwidth rung. */
  | 'restart_on_safe_bandwidth'
  /** Re-establish a reaped session, keeping the profile that was already chosen. */
  | 'restart_session'
  /** All automatic steps are spent — surface the error. */
  | 'give_up';

/**
 * A reaped lease is re-established at most this often per viewing intent. The
 * budget exists so a server that keeps reaping (or a link that never really
 * comes back) cannot spin the client through endless encoder starts.
 */
export const MAX_SESSION_RESTARTS = 2;

export interface RecoveryLadderState {
  /** The one-shot profile fallback has been consumed for this viewing intent. */
  autoFallbackUsed: boolean;
  /** How many times a reaped session has been re-established this intent. */
  sessionRestarts: number;
  /**
   * A restart this ladder authorised is scheduled but has not begun yet. The
   * attempt it starts must inherit this budget rather than reset it — a budget
   * cleared by the very restart it granted bounds nothing, and a link that
   * stays bad would loop through encoder starts forever. Explicit rather than
   * inferred from status, because a failure handler pausing the video moves the
   * status on its own.
   */
  restartPending: boolean;
}

export function createRecoveryLadderState(): RecoveryLadderState {
  return { autoFallbackUsed: false, sessionRestarts: 0, restartPending: false };
}

/**
 * Failure codes that mean "the link could not carry this stream", as opposed to
 * "this device could not decode it". They are answered with less bitrate, never
 * with the 'repair' transcode.
 */
export const NETWORK_STARVATION_CODES: ReadonlySet<string> = new Set([
  'HLS_NETWORK_RETRIES_EXHAUSTED',
  'HLSJS_STALL_RECOVERY_FAILED',
  'NATIVE_STALL_RECOVERY_FAILED',
]);

export interface RecoveryEscalationInput {
  failure: Pick<PlaybackFailure, 'class' | 'source' | 'terminal' | 'retryable' | 'recoverable'> &
    Partial<Pick<PlaybackFailure, 'code'>>;
  /** User pinned a profile in the UI — automatic profile changes would override intent. */
  explicitProfilePinned: boolean;
  /** Whether the orchestrator is running an active playback intent (channel or recording) vs static src. */
  hasActiveIntent?: boolean;
  /**
   * The session had reached READY before this failure. Distinguishes the two
   * things a 410 can mean, which share a status and a code but not a remedy.
   */
  hasEstablishedSession?: boolean;
  state: RecoveryLadderState;
}

export function decideRecoveryEscalation({
  failure,
  explicitProfilePinned,
  hasActiveIntent = true,
  hasEstablishedSession = false,
  state,
}: RecoveryEscalationInput): RecoveryEscalation {
  // Auth is never a transcoder or a network problem — restarting cannot fix a
  // 401, and retrying one just burns the token faster.
  if (failure.class === 'auth') {
    return 'give_up';
  }

  // A reaped lease (heartbeat 410/404) is the normal outcome of a tunnel or a
  // dead spot: the session is gone server-side but nothing is actually broken.
  // Re-establishing it is the whole fix, and it must be decided BEFORE the
  // terminal bail because the failure is reported as terminal-ish state.
  //
  // The gate is hasEstablishedSession, not the status or the code: a 410 during
  // startup readiness carries the same 'session' class and the same
  // SESSION_EXPIRED fallback code, but it means the session never came up — the
  // recording was deleted, the packager failed — and retrying spins on a fault
  // that will not clear. Only a lease that lapsed under a session which had
  // actually reached READY is worth re-establishing.
  if (failure.class === 'session') {
    if (!hasEstablishedSession || !hasActiveIntent || failure.terminal || !failure.recoverable) {
      return 'give_up';
    }
    return state.sessionRestarts >= MAX_SESSION_RESTARTS ? 'give_up' : 'restart_session';
  }

  if (failure.terminal) {
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

  if (failure.code && NETWORK_STARVATION_CODES.has(failure.code)) {
    return 'restart_on_safe_bandwidth';
  }

  return 'restart_with_fallback_profile';
}
