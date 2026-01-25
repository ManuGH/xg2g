
import { describe, it, expect } from 'vitest';
import { resolvePlaybackInfoPolicy } from '../../src/contracts/PolicyEngine';

describe('PolicyEngine (V3Player.PlaybackInfo)', () => {

  // Scenario 1: Backend supports Contract (v4+), Payload is Valid
  it('returns Normative when Decision present and Capability Required', () => {
    const caps = { 'contracts.playbackInfoDecision': 'required' };
    const pInfo = {
      decision: { selectedOutputUrl: 'http://norm' }
    };
    const res = resolvePlaybackInfoPolicy(caps, pInfo);
    expect(res).toEqual({ mode: 'normative', reason: 'NORMATIVE_PRESENT' });
  });

  // Scenario 2: Backend supports Contract (v4+), Payload Missing Selection (Violation)
  it('returns FailClosed when Decision present but Selection missing (Violation)', () => {
    const caps = { 'contracts.playbackInfoDecision': 'required' };
    const pInfo = {
      decision: { /* selectedOutputUrl missing */ } // Invokes failsClosedIf
    };
    const res = resolvePlaybackInfoPolicy(caps, pInfo);
    expect(res.mode).toBe('failclosed');
    expect(res.reason).toBe('POLICY_VIOLATION_FAILCLOSED');
  });

  // Scenario 3: Backend is Legacy (v3), Contract Absent -> Fallback Allowed
  it('returns Legacy when Decision missing and Capability Absent', () => {
    const caps = { 'contracts.playbackInfoDecision': 'absent' };
    const pInfo = {
      url: 'http://legacy'
    };
    const res = resolvePlaybackInfoPolicy(caps, pInfo);
    expect(res).toEqual({ mode: 'legacy', reason: 'FALLBACK_PERMITTED' });
  });

  // Scenario 4: Backend claims Contract Required, but sends Legacy only (Breach)
  it('returns FailClosed when Decision missing but Capability Required', () => {
    const caps = { 'contracts.playbackInfoDecision': 'required' };
    const pInfo = {
      url: 'http://legacy'
    };
    // fallbackAllowedIf requires 'absent'. Here it is 'required'.
    // Also triggers failClosedIf because capability=required implies decision MUST be present.
    const res = resolvePlaybackInfoPolicy(caps, pInfo);
    expect(res.mode).toBe('failclosed');
    expect(res.reason).toBe('POLICY_VIOLATION_FAILCLOSED');
  });

  // Scenario 5: Implicit Deny - Capability neither Required nor Absent (e.g. Optional/Mismatch)
  // and Payload is Legacy.
  // Matrix: fallbackAllowedIf requires 'absent'.
  // Here: 'optional' != 'absent'.
  // failClosedIf: 'required' != 'optional'.
  // Result: Block 2 Default Deny -> FailClosed.
  it('returns FailClosed when Capability is Unknown/Optional (Implicit Deny)', () => {
    const caps = { 'contracts.playbackInfoDecision': 'optional' };
    const pInfo = {
      url: 'http://legacy'
    };
    const res = resolvePlaybackInfoPolicy(caps, pInfo);
    expect(res.mode).toBe('failclosed');
    expect(res.reason).toBe('FALLBACK_DENIED');
  });
});
