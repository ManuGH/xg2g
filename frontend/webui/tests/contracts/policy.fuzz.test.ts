
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { resolvePlaybackInfoPolicy } from './helpers/PolicyEngine';

describe('PolicyEngine Fuzzing', () => {
  it('Invariant: Never throws', () => {
    fc.assert(
      fc.property(
        fc.dictionary(fc.string(), fc.oneof(fc.string(), fc.boolean(), fc.constant(undefined))), // Capabilities
        fc.object(), // Payload (any object)
        (caps, payload) => {
          // console.log(caps, payload);
          const res = resolvePlaybackInfoPolicy(caps as any, payload);
          expect(['normative', 'legacy', 'failclosed']).toContain(res.mode);
          expect(res.reason).toBeTruthy();
        }
      )
    );
  });

  it('Invariant: Required Capability + Missing Normative = FailClosed', () => {
    fc.assert(
      fc.property(
        fc.record({
          // Force strictly missing decision
          decision: fc.constant(undefined),
          url: fc.webUrl()
        }),
        (payload) => {
          const caps = { 'contracts.playbackInfoDecision': 'required' };
          const res = resolvePlaybackInfoPolicy(caps, payload);
          // Must be failclosed because we require contract but decision is missing
          expect(res.mode).toBe('failclosed');
        }
      )
    );
  });
});
