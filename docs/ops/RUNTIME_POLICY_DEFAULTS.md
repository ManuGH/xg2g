# Runtime Policy Defaults Matrix

**Status:** Active  
**Date:** 2026-04-18

This document freezes the intentional default guardrails for the runtime
playback policy introduced by the `confidence -> policy -> ladder -> session`
architecture slice.

It is not a second policy engine. The code remains authoritative. This document
exists so the shipped defaults are explicit, reviewable, and stable enough for
incident analysis and later tuning.

## Scope

These defaults govern:

- session loop cadence
- confidence-state hold times
- cooldown lengths
- probe scheduling and observation windows
- probe confirm / abort behavior
- allowed ladder boundaries

Primary implementation references:

- [confidence.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/confidence.go:12)
- [policy_loop.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/policy_loop.go:3)
- [playback_ladder.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/playback_ladder.go:9)

## Defaults

| Parameter | Default | Code | Rationale |
| :--- | :--- | :--- | :--- |
| Session loop minimum interval | `2s` | `sessionLoopMinimumInterval` | Fast enough for live adaptation without creating per-heartbeat thrash. |
| Probe schedule timeout | `8s` | `sessionLoopProbeScheduleTimeout` | A scheduled probe must become observable quickly or it is treated as ineffective. |
| Probe observation window | `12s` | `sessionLoopProbeObservationWindow` | Long enough to catch early network/buffer regressions without keeping an unstable probe alive for too long. |
| Cooldown after `lock_current` | `20s` | `cooldownLockCurrent` | Gives soft instability a short stabilization window before the next move. |
| Cooldown after `probe_up` | `15s` | `cooldownProbeUp` | Prevents immediate probe chaining while still allowing recovery to climb again reasonably quickly. |
| Cooldown after `degrade/step_down` | `30s` | `cooldownDegrade` | A downward move should hold longer than an exploratory upshift. |
| Cooldown after hard risk | `45s` | `cooldownHardRisk` | Decode-class failures remain the hardest class and should suppress optimism longest. |
| Hold `LOW_CONFIDENCE -> RECOVERY` | `20s` | `holdLowBeforeRecovery` | Avoids bounce-back immediately after a hard bad window. |
| Hold `RECOVERY -> STABLE` | `20s` | `holdRecoveryBeforeStable` | Requires a second stable phase before normal confidence returns. |
| Hold `STABLE -> HIGH_CONFIDENCE` | `40s` | `holdStableBeforeHigh` | High confidence should be earned over a longer clean period. |
| Hold `HIGH_CONFIDENCE -> probe_up` | `10s` | `holdHighBeforeProbeUp` | Forces a short dwell in `HIGH_CONFIDENCE` before a real upward experiment. |
| Probe trust recency window | `2m` | `probeTrustWindow` | Probe success/failure memory remains useful, but only over a bounded recent horizon. |

## Ladder Boundaries

The web runtime ladder is intentionally small and local-step only:

1. `repair_low`
2. `h264_720p`
3. `h264_1080p`
4. `video_copy_audio_aac`
5. `direct_copy`

Source:
- [playback_ladder.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/playback_ladder.go:9)

Guardrails:

- Never move more than one step per enforced transition.
- `step_down` may only move to the immediate lower neighbor.
- `probe_up` may only move to the immediate higher neighbor toward `target_step`.
- The runtime loop must never probe above the decision engine target.
- The runtime loop must never step below `repair_low`.

## Probe Guardrails

`probe_up` is allowed only when all of the following are true:

- confidence state is `HIGH_CONFIDENCE`
- confidence score is at least `50`
- at least `3` confidence windows were accumulated
- `HIGH_CONFIDENCE` has been held for at least `10s`
- no active cooldown
- no `no_probe_up` / decode-hard constraints
- current ladder step is below target ladder step

Source:
- [policy_loop.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/policy_loop.go:102)

## Probe Confirm Criteria

A probe is confirmed when either:

- the confidence layer already emits `probe_window_confirmed` or
  `probe_recently_confirmed`, or
- the session has been in `observing` for at least `12s` and all of the
  following hold:
  - confidence is not `LOW_CONFIDENCE`
  - no recent buffering reason
  - no recent network instability reason
  - no decode warning / decode hard risk
  - no stall reason

Source:
- [policy_loop.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/policy_loop.go:149)

## Probe Abort Criteria

A probe is aborted immediately when any of the following holds:

- `probe_window_regressed` or `probe_recently_regressed`
- confidence falls to `LOW_CONFIDENCE`
- decode hard risk, decode warning, or stall reason appears
- scheduled probe fails to become observable within `8s`
- after `12s` of observation the session still shows buffering or network
  instability

Source:
- [policy_loop.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/policy_loop.go:128)

## Hard vs Soft Failure Policy

Hard classes stay hard:

- decode failures
- decode warnings escalated into hard-risk semantics
- hard stalls

Soft classes remain eligible for hysteresis and recovery trust:

- buffering warnings
- network warnings
- typed recoveries for the same class

This asymmetry is intentional and must stay visible in tuning.

Source:
- [confidence.go](/root/xg2g-work/backend/internal/control/recordings/runtimepolicy/confidence.go:27)

## Review Rule

These defaults may be changed only when at least one of the following exists:

- replay evidence from real timeline artifacts
- an incident showing repeatable instability or over-conservatism
- a deliberate ladder or session-loop redesign

Do not tune these values ad hoc inside unrelated feature work.
