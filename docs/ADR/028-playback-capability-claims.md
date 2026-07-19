# ADR-028: Playback Capability Claims and Verified Truth

- **Status:** Active
- **Date:** 2026-07-19
- **Supersedes:** ADR-P7 section 2 where it treats client codec lists as truth

## Context

The WebUI runtime probe, backend client-family fixtures, and planner capability
veto independently answered whether a codec was usable. In particular, a
browser could claim Dolby audio support while another layer rejected the same
claim. Correct planner changes therefore depended on which copy of the matrix a
request happened to reach.

Runtime APIs such as `canPlayType` and Media Capabilities are useful evidence,
but their result is a client statement. They do not establish that a complete
container, packaging, engine, and device route is verified for xg2g playback.

## Decision

Capability resolution has three explicit stages:

1. **Raw** preserves canonical client claims (or an explicit fallback when a
   field was omitted).
2. **Verified** applies the versioned server compatibility policy. A policy may
   remove a claim and records a stable reason; it may not widen a supplied set.
3. **Effective** applies operator and host constraints to Verified. Only
   Effective capabilities may enter a playback decision engine.

The authoritative compatibility rules live in
`backend/internal/domain/playbackcompat`. Browser and native-app routes are
distinct. WebUI and native clients report observations and family identity;
they do not carry an independent verified compatibility matrix.

During migration, the old planner veto API may remain only as a thin adapter to
the same `playbackcompat` policy. It must not contain a second rule table.

## Change contract

- **Fixed:** contradictory browser Dolby answers and unverified client claims
  entering playback decisions as truth.
- **Improved:** capability provenance, policy versioning, audit reasons, and
  no-widening guarantees.
- **New:** the `Raw → Verified → Effective` capability contract.
- **Removed:** the WebUI family codec/container matrix and the planner-owned
  veto table.
- **Unchanged:** public v3 request/response schemas, native-app Dolby handling,
  live receipt integrity, and client runtime probing.
- **Risks:** an incorrectly classified client family can become unnecessarily
  conservative; registry snapshots currently persist Effective rather than the
  complete three-stage contract.
- **Acceptance criteria:** browser Dolby claims remain visible in Raw but not
  Effective; native app claims remain Effective; Effective never widens a
  non-empty claim set; planner and HTTP tests remain green.
- **Exit condition:** remove the planner veto adapter after recording playback
  also consumes planner evidence exclusively and parity tests prove all
  decisions use Effective capabilities. Codex owns the follow-up PR; Manuel
  approves merge and promotion.

## Consequences

Client probes can improve observability without silently changing server
policy. A compatibility correction is made once, tested as a matrix, and takes
effect before both legacy and planner decision code during the remaining
migration.
