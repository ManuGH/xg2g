import matrixData from '../../../../contracts/version_matrix.json';

export interface PolicyRule {
  requireNormativeWhenPresent?: boolean;
  legacyFallbackAllowedIf?: Condition[];
  failClosedIf?: Condition[];
}

export interface Condition {
  capability?: string;
  equals?: string | boolean | null;
  missing?: string;
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

function evaluateCondition(cond: Condition, caps: Capabilities, payload: any): boolean {
  let localMatch = true;

  if (cond.capability) {
    const val = caps[cond.capability];
    const target = cond.equals;
    if (target === 'absent') {
      if (val !== undefined && val !== 'absent') localMatch = false;
    } else {
      if (val !== target) localMatch = false;
    }
  }

  if (cond.missing) {
    const parts = cond.missing.split('.');
    let current = payload;
    for (const part of parts) {
      if (current === undefined || current === null) break;
      current = current[part];
    }
    if (current !== undefined && current !== null) localMatch = false;
  }

  if (localMatch && cond.and) {
    if (!evaluateCondition(cond.and, caps, payload)) {
      localMatch = false;
    }
  }

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

  if (policy.failClosedIf) {
    for (const cond of policy.failClosedIf) {
      if (evaluateCondition(cond, caps, payload)) {
        return { mode: 'failclosed', reason: 'POLICY_VIOLATION' };
      }
    }
  }

  return { mode: 'normative', reason: 'PREFERRED' };
}

export function resolvePlaybackInfoPolicy(
  caps: Capabilities,
  pInfo: any
): Resolution {
  const policy = matrix.policies['V3Player.PlaybackInfo'];
  if (!policy) return { mode: 'failclosed', reason: 'POLICY_UNKNOWN' };

  if (policy.failClosedIf) {
    for (const cond of policy.failClosedIf) {
      if (evaluateCondition(cond, caps, pInfo)) {
        return { mode: 'failclosed', reason: 'POLICY_VIOLATION_FAILCLOSED' };
      }
    }
  }

  const hasNormative = !!(pInfo.decision);

  if (hasNormative) {
    return { mode: 'normative', reason: 'NORMATIVE_PRESENT' };
  }

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
  }
  return { mode: 'failclosed', reason: 'FALLBACK_DENIED' };
}
