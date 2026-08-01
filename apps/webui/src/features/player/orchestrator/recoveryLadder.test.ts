import { describe, expect, it } from 'vitest';
import {
  MAX_SESSION_RESTARTS,
  createRecoveryLadderState,
  decideRecoveryEscalation,
} from './recoveryLadder';
import type { RecoveryEscalationInput } from './recoveryLadder';

function buildInput(overrides: {
  failure?: Partial<RecoveryEscalationInput['failure']>;
  explicitProfilePinned?: boolean;
  hasActiveIntent?: boolean;
  hasEstablishedSession?: boolean;
  state?: Partial<RecoveryEscalationInput['state']>;
} = {}): RecoveryEscalationInput {
  return {
    failure: {
      class: 'media',
      source: 'media-element',
      terminal: false,
      retryable: true,
      recoverable: true,
      ...overrides.failure,
    },
    explicitProfilePinned: overrides.explicitProfilePinned ?? false,
    hasActiveIntent: overrides.hasActiveIntent,
    hasEstablishedSession: overrides.hasEstablishedSession ?? true,
    state: { ...createRecoveryLadderState(), ...overrides.state },
  };
}

describe('decideRecoveryEscalation', () => {
  it('offers one profile-fallback restart for a recoverable media failure', () => {
    expect(decideRecoveryEscalation(buildInput())).toBe('restart_with_fallback_profile');
  });

  it('gives up once the fallback has been consumed', () => {
    expect(decideRecoveryEscalation(buildInput({
      state: { autoFallbackUsed: true },
    }))).toBe('give_up');
  });

  it('never escalates auth failures', () => {
    expect(decideRecoveryEscalation(buildInput({
      failure: { class: 'auth' },
    }))).toBe('give_up');
  });

  it('never escalates terminal failures', () => {
    expect(decideRecoveryEscalation(buildInput({
      failure: { terminal: true },
    }))).toBe('give_up');
  });

  it('leaves backend start failures to their own retry semantics', () => {
    expect(decideRecoveryEscalation(buildInput({
      failure: { source: 'backend' },
    }))).toBe('none');
    expect(decideRecoveryEscalation(buildInput({
      failure: { source: 'orchestrator' },
    }))).toBe('none');
  });

  it('respects a user-pinned profile', () => {
    expect(decideRecoveryEscalation(buildInput({
      explicitProfilePinned: true,
    }))).toBe('none');
  });

  it('gives up when the failure is neither retryable nor recoverable', () => {
    expect(decideRecoveryEscalation(buildInput({
      failure: { retryable: false, recoverable: false },
    }))).toBe('give_up');
  });

  // A dead spot long enough to lapse the lease used to end the viewing session:
  // the reap surfaced as a session-class failure and the ladder gave up, so the
  // player sat on an error until the user pressed Retry.
  describe('a reaped lease', () => {
    const reaped = {
      class: 'session' as const,
      source: 'native-host' as const,
      code: 'SESSION_EXPIRED',
      terminal: false,
      retryable: true,
      recoverable: true,
    };

    it('is re-established rather than surfaced', () => {
      expect(decideRecoveryEscalation(buildInput({ failure: reaped }))).toBe('restart_session');
    });

    it('does not consume the profile-fallback budget', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: reaped,
        state: { autoFallbackUsed: true },
      }))).toBe('restart_session');
    });

    it('is bounded, so a server that keeps reaping cannot spin the client', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: reaped,
        state: { sessionRestarts: MAX_SESSION_RESTARTS },
      }))).toBe('give_up');
    });

    it('is not re-established without an active intent', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: reaped,
        hasActiveIntent: false,
      }))).toBe('give_up');
    });

    it('is not re-established when the session is genuinely gone', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: { ...reaped, recoverable: false },
      }))).toBe('give_up');
    });

    // A 410 during startup readiness carries the same class and the same
    // fallback code, but it means the recording was deleted or the packager
    // failed — retrying spins on a fault that will not clear.
    it('is not confused with a startup 410, which never reached a live session', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: reaped,
        hasEstablishedSession: false,
      }))).toBe('give_up');
    });
  });

  // Answering a starved link with 'repair' — High + Transcode — asks it for
  // more bitrate than the stream it just failed to carry.
  describe('link starvation', () => {
    it.each([
      'HLS_NETWORK_RETRIES_EXHAUSTED',
      'HLSJS_STALL_RECOVERY_FAILED',
      'NATIVE_STALL_RECOVERY_FAILED',
    ])('restarts on the safe rung instead of the repair transcode (%s)', (code) => {
      expect(decideRecoveryEscalation(buildInput({
        failure: { code },
      }))).toBe('restart_on_safe_bandwidth');
    });

    it('still uses the repair transcode for decode failures', () => {
      expect(decideRecoveryEscalation(buildInput({
        failure: { code: 'MEDIA_DECODE_ERROR' },
      }))).toBe('restart_with_fallback_profile');
    });
  });
});
