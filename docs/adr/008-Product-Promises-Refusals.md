# ADR-PROD-001: Explicit Promises & Conscious Refusals

## Status

Accepted (2026-01-06)

## Context

xg2g is an infrastructural system with rigid architecture, concurrency, and SRE invariants. Not every user or integration problem can be solved without compromising system integrity. To ensure clarity and reduce support overhead, we must define what the product explicitly promises—and what it consciously refuses.

## Decision

### 1. Explicit Product Promises (What We Guarantee)

#### P1. Stable API Contract

- **Guarantee**: Versioned APIs with strict semantic guarantees.
- **Enforcement**: Breaking changes only in Major versions; OpenAPI is the single source of truth.
- **Implicit**: If the client follows the contract, the system behavior is deterministic.

#### P2. Deterministic Streaming for Compliant Clients

- **Guarantee**: Standardized output contract (H.264 / AAC / fMP4 / HLS).
- **Enforcement**: Same Input + Same State = Same Behavior. No client-dependent branching.

#### P3. Honest Errors Over Silent Degradation

- **Guarantee**: 4xx/5xx responses are first-class product signals, not leaks.
- **Dogma**: A 503 error is a successful protection mechanism, not a failure. We do not hide stalls behind "stuck" progress indicators.

### 2. Conscious Refusals (What We Consciously Do Not Provide)

#### R1. No Best-Effort for Degraded Media

- **Refusal**: No "try-anyway" paths for corrupt or pathological media files.
- **Outcome**: Degraded inputs trigger immediate failure (SRE-compliant) rather than inconsistent outputs.

#### R2. No Browser or Device-Specific Workarounds

- **Refusal**: No special handling for Safari, Chrome, or specific Smart TV quirks.
- **Outcome**: Client-side incompatibility is treated as a "Non-Goal". We fix the contract, not the client.

#### R3. No Implicit Resumption of Terminated Jobs

- **Refusal**: No automatic retry of jobs terminated by the SRE "Death Line".
- **Outcome**: Termination is final and requires explicit user/policy re-triggering. Progress loss is consciously accepted to protect system health.

## Product Dogma

An honest "No" is a feature. An unstable "Yes" is a product defect.
xg2g prioritizes system integrity and predictability over opportunistic success rates.

## Consequences

### Positive

- Clear expectations for integrators and partners.
- Dramatically reduced Tier-3 support costs.
- Consistent UX across all compliant platforms.
- Product, Architecture, and Operation are 100% aligned.

### Negative (Consciously Accepted)

- High-pathology edge-case files may be unplayable.
- "It worked in older versions by accident" is no longer a valid reason for code changes.
- Reduced flexibility for Sales promises without Engineering/SRE validation.
