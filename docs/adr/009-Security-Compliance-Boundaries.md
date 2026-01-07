# ADR-SEC-001: Security Constraints & Compliance Boundaries

## Status

Accepted (2026-01-06)

## Context

xg2g must be an industrial-grade component, which requires non-negotiable security and compliance boundaries. Debuggability often conflicts with security and privacy; this ADR establishes the hard constraints where we prioritize the latter over the former to protect the system, its data, and our legal standing.

## Decision

### 1. Security Constraints: Convenience is an Attack Vector

Security-by-design takes precedence over developer or integrator convenience.

- **Categorically Prohibited**:
  - No authentication bypass for "internal", "debug", or "temporary" access.
  - No hardcoded fallback tokens or "emergency" accounts.
  - No IP-based trust or header-only authentication (e.g., "X-Forwarded-For" trust without strict validation).
  - No silent downgrades of authentication failures to "guest" access.
- **Dogma**: Authentication is binary. "Convenience exceptions" are security vulnerabilities.

### 2. Logging & Observability: Deliberate Non-Inclusion

We actively decide what NOT to log to reduce the impact of potential data leaks.

- **Rules of Non-Inclusion**:
  - **No User-IDs**: Only pseudonymous or hashed IDs are allowed in production logs.
  - **No PII**: No logging of IP addresses (must be masked or hashed), email addresses, or names.
  - **No Content-Specific Paths**: Filenames and full media paths are restricted. Use technical identifiers (Job IDs, ServiceRefs) for troubleshooting.
  - **Metadata Restriction**: Log only technical properties (codecs, bits, duration). No logging of media titles, descriptions, or cast.
- **Dogma**: Data that is not stored cannot be leaked.

### 3. Retention & Deletion

Data has a strictly defined lifespan.

- **Access Logs**: Maximum retention of 7 days (or minimum required by legal jurisdiction).
- **Job/Error Logs**: Maximum retention of 14 days for successful jobs; 30 days for failed jobs (for analysis).
- **Hard Rule**: No "just-in-case" data retention. Automated deletion must be part of the system lifecycle.

### 4. Liability & Content Integrity

We define the technical boundaries of our legal responsibility.

- **Refusals**:
  - **No Integrity Guarantee**: We do not guarantee the completion or "correctness" of media exports if the source material is corrupt (Fail-Closed per ADR-SRE-001).
  - **Non-Mutation Promise**: We guarantee the system will not alter content unless explicitly required by a pipeline action.
  - **Immutable Trails**: Logs serve as evidence; they must be append-only and immutable within their retention period.

## Consequences

### Positive

- Minimizes attack surface and blast radius of potential leaks.
- Ensures GDPR/PII compliance by design.
- Protects the project and its maintainers from content-related legal liability.
- Forces high-quality, ID-based troubleshooting over "grep for filename" habits.

### Negative (Consciously Accepted)

- Debugging certain edge-case errors may be harder due to lack of PII/Paths.
- User support cannot "see" exactly what a user saw, only technical job properties.
- Automated cleanup may remove evidence of long-past incidents.

## Dogma

Debuggability ends where Security, Privacy, or Liability begins. An unexplainable error is acceptable; an explainable leak is not.
