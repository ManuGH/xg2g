import { describe, expect, it } from 'vitest';
import { createRecoveryLadderState, decideRecoveryEscalation } from './recoveryLadder';
import type { RecoveryEscalationInput } from './recoveryLadder';

function buildInput(overrides: {
  failure?: Partial<RecoveryEscalationInput['failure']>;
  explicitProfilePinned?: boolean;
  state?: RecoveryEscalationInput['state'];
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
    state: overrides.state ?? createRecoveryLadderState(),
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

  it('never escalates auth or session failures', () => {
    expect(decideRecoveryEscalation(buildInput({
      failure: { class: 'auth' },
    }))).toBe('give_up');
    expect(decideRecoveryEscalation(buildInput({
      failure: { class: 'session' },
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
});
