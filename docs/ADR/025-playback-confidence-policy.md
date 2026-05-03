# ADR-025: Playback Confidence and Runtime Policy Layer

**Status:** Accepted
**Date:** 2026-04-18

## Context

`xg2g` already has strong playback inputs:

- source truth
- client capabilities
- host runtime and benchmark truth
- typed feedback warnings and recoveries

That is enough sensor quality to stop optimizing only for the current moment.

The remaining gap is temporal behavior.

Today the system is strongest at:

- "what is technically the best option now?"
- "what is the safe fallback after a failure?"

It is weaker at:

- "how much trust has this session earned over the last 10-60 seconds?"
- "how aggressively may quality recover after instability?"
- "what action is best over the next 30-90 seconds, not only this tick?"

If that temporal logic is pushed directly into the existing decision engine or
into `feedback_policy.go`, the modules start to blur:

- the decision engine stops being a pure best-option evaluator
- feedback policy becomes a mix of event parsing, history aggregation, and
  behavior control
- runtime probing arrives before there is a stable place to interpret it

Per [ADR-005](005-Architecture-Invariants.md), policy truth must remain explicit
and debuggable. Per [ADR-009](009-playback-decision-spec.md), the playback
decision engine must remain deterministic and narrow in scope.

## Decision

Introduce an explicit runtime layer above the existing decision engine with
three separate responsibilities:

1. `decision`
   - chooses the technically best target profile under ideal assumptions
   - remains the authoritative best-option engine

2. `confidence`
   - aggregates recent playback evidence over fixed time windows
   - emits a continuous `confidence_score`
   - derives a bounded `confidence_state`

3. `policy`
   - converts base decision + confidence snapshot into a short-horizon runtime
     action for the next `30-90s`

This layer is normative for adaptive playback behavior.

The runtime contract is composed of three explicit outputs:

- `confidence_score`
- `confidence_state`
- `policy_constraints`

### 1. Module boundaries

#### 1.1 Decision engine

The decision engine answers only:

`What is the best technically valid playback option if the system may act freely right now?`

It must not own:

- time-window aggregation
- cooldown state
- runtime confidence hysteresis
- probe scheduling
- recovery trust accumulation

#### 1.2 Feedback policy

`feedback_policy` remains responsible for typed event interpretation and for
normalizing raw playback feedback into bounded event categories.

It may keep lightweight local safety rules, but it is not the long-horizon
policy owner.

#### 1.3 Confidence layer

The confidence layer owns:

- fixed-window aggregation
- score accumulation and decay
- typed asymmetry between hard and soft failure classes
- state derivation from score plus hysteresis

#### 1.4 Policy layer

The policy layer owns:

- short-horizon runtime action selection
- degrade / hold / probe-up / cooldown behavior
- guardrails that determine when quality recovery may be attempted

## Normative model

### 2. Confidence score

The runtime layer maintains a continuous score:

- `confidence_score: [-100, +100]`

It is not exposed as an API contract by itself, but it is the primary internal
tuning surface.

The score must be:

- bounded
- monotone within one evaluation tick
- decayed smoothly rather than reset arbitrarily
- derived from recent windows, not from a single latest event

### 3. Confidence state

The score maps to a bounded state:

- `LOW_CONFIDENCE`
- `RECOVERY`
- `STABLE`
- `HIGH_CONFIDENCE`

Initial bucketing for the first implementation:

- `LOW_CONFIDENCE`: `score <= -30`
- `RECOVERY`: `-29 .. +9`
- `STABLE`: `+10 .. +49`
- `HIGH_CONFIDENCE`: `>= +50`

These thresholds are intentionally policy-tunable and must not leak into the
decision engine.

### 4. Fixed window aggregation

The first implementation uses fixed windows:

- `window_duration = 10s`

Each window aggregates normalized playback evidence for that slice rather than
reacting to individual events directly.

Minimum first-cut window features:

- `hard_decode_fail_count`
- `hard_stall_fail_count`
- `buffer_warning_count`
- `network_warning_count`
- `decode_warning_count`
- `typed_recovery_buffer_count`
- `typed_recovery_network_count`
- `typed_recovery_decode_count`
- `clean_playing_ms`
- `host_pressure_band`
- `host_performance_class`
- `host_benchmark_class`
- `source_bitrate_confidence`
- `source_truth_freshness`

The confidence layer may also carry:

- `window_kind`
  - `live`
  - `live_dvr`
  - `vod`

### 5. Scoring rules

The first cut should stay intentionally simple and asymmetric.

#### 5.1 Hard negatives

These must carry the strongest negative weight:

- hard decode failures
- repeated decode warnings that escalated to failure semantics
- hard stall failures
- failed probe-up attempts

Decode remains the hardest class. A typed recovery must never fully cancel a
recent hard decode failure.

#### 5.2 Soft negatives

These are penalized, but with more hysteresis:

- buffer warnings
- network warnings
- short non-fatal recoverable stalls

#### 5.3 Positives

Positive weight comes from:

- clean uninterrupted playback time
- typed recoveries
- favorable host headroom
- fresh, high-confidence source truth

Typed recoveries are intentionally asymmetric:

- they may rebuild trust for the same class
- they may soften future policy for that class
- they must not erase hard decode risk

### 6. Initial score guidance

The exact numeric weights are implementation-tunable, but the initial shape must
follow this relative ordering:

- hard decode fail: very strong negative
- hard stall fail: strong negative
- failed probe-up: strong negative
- decode warning: medium negative
- network warning: light-to-medium negative
- buffer warning: light negative
- typed recovery: moderate positive for the same class only
- clean stable window: moderate positive
- good headroom / fresh truth: light positive modifier

## Policy layer

### 7. Policy actions

The policy layer emits one bounded action per evaluation tick:

- `hold`
- `degrade`
- `probe_up`
- `lock_current`
- `cooldown`

Action intent:

- `hold`
  - keep the current resolved profile
- `degrade`
  - step down one bounded quality rung or apply stricter max-quality constraint
- `probe_up`
  - temporarily test the next better rung
- `lock_current`
  - freeze the current rung despite mild soft noise
- `cooldown`
  - forbid upshift/probe attempts for a bounded interval

In addition to the action itself, the policy layer emits bounded guardrails as
`policy_constraints`, for example:

- `no_probe_up`
- `max_quality_compatible`
- `lock_current_rung`
- `cooldown_active`
- `decode_risk_hard`

### 8. Policy rules

#### 8.1 LOW_CONFIDENCE

Default behavior:

- `degrade` or `cooldown`
- no `probe_up`
- strong preference for stable compatible paths

#### 8.2 RECOVERY

Default behavior:

- `hold`
- optionally `lock_current`
- no proactive upgrade
- typed soft warnings do not immediately force further downgrade if the current
  rung is already conservative

#### 8.3 STABLE

Default behavior:

- `hold`
- allow future `probe_up` eligibility once minimum stable time and headroom
  constraints are met

#### 8.4 HIGH_CONFIDENCE

Default behavior:

- `hold`
- may schedule `probe_up` if:
  - cooldown inactive
  - host headroom sufficient
  - source truth sufficiently fresh
  - recent decode risk absent

### 9. Probe-up guardrails

Runtime probing is explicitly downstream of confidence and policy.

The first confidence-policy implementation does not require live probing to
exist. When probing is introduced, it must obey:

- only from `STABLE` or `HIGH_CONFIDENCE`
- one rung at a time
- bounded probe window, initially `8-15s`
- immediate rollback on hard fail
- immediate rollback on repeated typed warning above policy threshold
- failed probes reduce confidence more than a soft warning

## Observability

### 10. Policy snapshot

Each policy evaluation must be explainable.

The runtime layer should emit a bounded snapshot shaped like:

```go
type ConfidenceState string

const (
    ConfidenceLow      ConfidenceState = "LOW_CONFIDENCE"
    ConfidenceRecovery ConfidenceState = "RECOVERY"
    ConfidenceStable   ConfidenceState = "STABLE"
    ConfidenceHigh     ConfidenceState = "HIGH_CONFIDENCE"
)

type PolicyAction string

const (
    PolicyHold       PolicyAction = "hold"
    PolicyDegrade    PolicyAction = "degrade"
    PolicyProbeUp    PolicyAction = "probe_up"
    PolicyLockCurrent PolicyAction = "lock_current"
    PolicyCooldown   PolicyAction = "cooldown"
)

type ConfidenceSnapshot struct {
    Score             int
    State             ConfidenceState
    WindowCount       int
    CooldownUntil     time.Time
    PolicyConstraints []string
    Reasons           []string
}

type PolicyDecision struct {
    Action            PolicyAction
    MaxQualityRung    string
    ProbeCandidate    string
    PolicyConstraints []string
    Reasons           []string
}
```

Minimum reasons should be machine-bounded and human-meaningful, for example:

- `decode_risk_high`
- `stall_recent`
- `network_recently_unstable`
- `buffering_recently_recovered`
- `headroom_good`
- `source_truth_stale`
- `source_truth_low_confidence`
- `cooldown_active`

### 11. Debuggability rule

The system must be able to answer all three questions independently:

1. `What was the best technical option?`
2. `How confident was the runtime layer?`
3. `Why did policy choose this action for the next horizon?`

No single module may collapse those answers into one opaque result.

## Implementation slicing

### 12. Required order

Implementation must proceed in this order:

1. `confidence_score` and `confidence_state`
2. fixed-window aggregation
3. policy action selection
4. runtime probing

This order is mandatory because probing without a stable confidence-policy
foundation only injects more volatility into the feedback stream.

### 13. Minimal Go-near interfaces

The first implementation should target a narrow internal surface:

```go
type WindowFeatures struct {
    HardDecodeFails       int
    HardStallFails        int
    BufferWarnings        int
    NetworkWarnings       int
    DecodeWarnings        int
    RecoveryBuffer        int
    RecoveryNetwork       int
    RecoveryDecode        int
    CleanPlayingMS        int64
    WindowKind            string
    HostPressureBand      string
    HostPerformanceClass  string
    HostBenchmarkClass    string
    SourceBitrateConfidence string
    SourceTruthFreshness  string
}

func EvaluateConfidence(prev ConfidenceSnapshot, win WindowFeatures, now time.Time) ConfidenceSnapshot
func DecidePolicy(base decision.Decision, conf ConfidenceSnapshot, now time.Time) PolicyDecision
```

The exact package names are implementation detail, but the separation of
responsibilities is normative.

## Consequences

### Positive

- Adaptive behavior becomes time-aware instead of event-nervous.
- Quality recovery can become more aggressive without sacrificing stability.
- Runtime decisions become observable and explainable.
- Future probing and host/client-specific policies fit into a stable frame.

### Negative

- The runtime model adds another layer of state.
- Tuning responsibility shifts from single thresholds to score and hysteresis.
- Poor module boundaries would create duplication if implemented carelessly.

## Non-Goals

- Replacing the decision engine with a state machine
- Collapsing feedback interpretation and policy control into one file
- Treating typed recoveries as proof that hard decode risk no longer exists

## References

- [ADR-005](005-Architecture-Invariants.md)
- [ADR-009](009-playback-decision-spec.md)
- [ADR-024](024-published-endpoint-connectivity-truth.md)
