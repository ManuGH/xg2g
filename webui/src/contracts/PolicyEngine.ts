
import matrixData from '../../../contracts/version_matrix.json';

// Types derived from schema
export interface PolicyRule {
  requireNormativeWhenPresent?: boolean;
  legacyFallbackAllowedIf?: Condition[];
  failClosedIf?: Condition[];
}

export interface Condition {
  capability?: string;
  equals?: string | boolean | null;
  missing?: string; // Check allowed deep path in payload? Or simple field?
  and?: Condition;
  or?: Condition;
}

export type ResolutionMode = 'normative' | 'legacy' | 'failclosed';

export interface Resolution {
  mode: ResolutionMode;
  reason: string;
}

export interface Capabilities {
  [key: string]: string | boolean | undefined;
}

const matrix = matrixData as { policies: Record<string, PolicyRule> };

/**
 * Evaluates a single condition against capabilities and payload.
 * Logic: (LocalConstraints && AND_Constraints) || OR_Constraints
 */
function evaluateCondition(cond: Condition, caps: Capabilities, payload: any): boolean {
  let localMatch = true;

  // Capability Check
  if (cond.capability) {
    const val = caps[cond.capability]; // e.g. "required", "absent"
    const target = cond.equals;
    if (target === 'absent') {
      if (val !== undefined && val !== 'absent') localMatch = false;
    } else {
      if (val !== target) localMatch = false;
    }
  }

  // Payload missing check (Deep check)
  if (cond.missing) {
    const parts = cond.missing.split('.');
    let current = payload;
    for (const part of parts) {
      if (current === undefined || current === null) break;
      current = current[part];
    }
    // If current is NOT undefined/null, then it is NOT missing.
    if (current !== undefined && current !== null) localMatch = false;
  }

  // Evaluate AND (Recursive)
  if (localMatch && cond.and) {
    if (!evaluateCondition(cond.and, caps, payload)) {
      localMatch = false;
    }
  }

  // Evaluate OR (Recursive) - Short-circuit
  if (cond.or) {
    if (evaluateCondition(cond.or, caps, payload)) {
      return true;
    }
  }

  return localMatch;
}

export function resolvePolicy(
  policyName: string,
  caps: Capabilities,
  payload: any
): Resolution {
  const policy = matrix.policies[policyName];
  if (!policy) {
    return { mode: 'failclosed', reason: 'POLICY_UNKNOWN' };
  }

  // 1. Check Fail Closed Conditions
  if (policy.failClosedIf) {
    for (const cond of policy.failClosedIf) {
      if (evaluateCondition(cond, caps, payload)) {
        return { mode: 'failclosed', reason: 'POLICY_VIOLATION' };
      }
    }
  }

  return { mode: 'normative', reason: 'PREFERRED' };
}

// Correction: The engine needs to know if Legacy is ALLOWED.
// If payload is garbage but Legacy Fallback is allowed, return Legacy.
// Revised logic:

export function resolvePlaybackInfoPolicy(
  caps: Capabilities,
  pInfo: any
): Resolution {
  const policy = matrix.policies['V3Player.PlaybackInfo'];
  if (!policy) return { mode: 'failclosed', reason: 'POLICY_UNKNOWN' };

  // 1. Fail Closed checks (Hard stops)
  if (policy.failClosedIf) {
    for (const cond of policy.failClosedIf) {
      if (evaluateCondition(cond, caps, pInfo)) {
        return { mode: 'failclosed', reason: 'POLICY_VIOLATION_FAILCLOSED' };
      }
    }
  }

  // 2. Decide Mode
  const hasNormative = !!(pInfo.decision);

  if (hasNormative) {
    return { mode: 'normative', reason: 'NORMATIVE_PRESENT' };
  } else {
    // Normative missing. Can we fallback?
    // GOVERNANCE: Default DENY. Only allow if explicit policy permits.
    let canFallback = false;

    if (policy.legacyFallbackAllowedIf) {
      for (const cond of policy.legacyFallbackAllowedIf) {
        if (evaluateCondition(cond, caps, pInfo)) {
          canFallback = true;
          break;
        }
      }
    }

    if (canFallback) {
      return { mode: 'legacy', reason: 'FALLBACK_PERMITTED' };
    } else {
      return { mode: 'failclosed', reason: 'FALLBACK_DENIED' };
    }
  }
}
