# P8-4: Observability Decision Contract

## 1. Trace Attributes (Normative)

The Application MUST emit these attributes on the Decision Span:

| Attribute | Meaning | Domain |
| :--- | :--- | :--- |
| `xg2g.decision.mode` | The final decision mode | `direct_play`, `direct_stream`, `transcode`, `deny` |
| `xg2g.decision.protocol` | The wire protocol | `mp4`, `hls`, `none` |
| `xg2g.decision.reasons` | Full list of normative reason codes | List[String] |
| `xg2g.decision.reason_primary` | The first/primary reason code | String (Normative Enum) |
| `xg2g.requestId` | Unique Request ID | String (Correlation ID) |

**Freeze**: No other `xg2g.decision.*` attributes are allowed. This MUST be enforced by code (Whitelist).

## 2. Span Semantics

- **Operation**: `Decide()` MUST start a self-contained span named `xg2g.decision`.
- **Termination**: The span ends when `Decide()` returns.
- **Ownership**: The decision engine owns this span.

## 3. Metrics (Normative)

The Application MUST emit these metrics:

### `xg2g_decision_total`

Counter.
Labels:

- `mode`: matches `xg2g.decision.mode`
- `protocol`: matches `xg2g.decision.protocol` (derived: `mp4`, `hls`, `none`)
- `reason_primary`:
  - If `reasons` populated: `reasons[0]`
  - If `direct_play`: `directplay_match`
  - If `direct_stream`: `directstream_match`
  - If `deny` (fallback): `no_compatible_playback_path`

### `xg2g_decision_problem_total`

Counter (for 422/500 errors).
Labels:

- `code`: Valid ProblemCodes (`decision_ambiguous`, `invariant_violation`)

## 4. Implementation

- **Single Source**: `internal/control/recordings/decision/observe.go`
- **Function**: `EmitDecisionObs(ctx, input, dec, prob)`
- **Caller**: `Decide()` (Phase 6, after Invariant Check)
- **Constraint**: `EmitDecisionObs` must perform **Attribute Whitelisting**.

## 5. Verification

- `TestDecisionObservabilityContract`:
  - Uses OTel SDK (In-Memory Exporter).
  - Asserts exact attribute keys/values (No superset allowed).
  - Asserts exact metric increment.
