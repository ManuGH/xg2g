# Verbose Schema (v3.0) Sunset Plan

## 1. Definition of Legacy

"Legacy" refers to the verbose, TitleCase JSON tags in `DecisionInput`.

- e.g. `Source`, `Capabilities`, `APIVersion`, `videoCodec`.
- Compact flavor (v3.1+) uses lowercase: `source`, `caps`, `api`, `v`.

## 2. Transition Mechanism (Dual-Decode)

The engine currently supports **Dual-Decode** with **Strict Overlap Rejection** (Mix is 400).

- Callers can send *either* legacy OR compact.
- Sending both results in `capabilities_invalid`.

## 3. Monitoring & Measurement

Observability field: `xg2g.decision.schema`

- Values: `legacy`, `compact`, `mixed`, `unknown`.
- Metric: `xg2g_decision_total` (dimension: `schema`).

## 4. Milestones

- **Phase 1 (Active)**: Support both, record usage metrics.
- **Phase 2 (v3.2)**: Emit `WARN` logs for `schema="legacy"`.
- **Phase 3 (v4.0)**: Remove `legacy` support from `decode.go`. Delete Dual-Decode tests.

## 5. Normative Metrics Contract

The **Exit Criteria for Sunset** (Legacy usage < 1% for 14 consecutive days) is strictly defined as follows:

| Metric | Definition | Status |
| :--- | :--- | :--- |
| **Denominator** | Total **Valid** decisions. This includes `200 OK` (Play/Stream/Transcode) and `200 Deny` (Policy/Logic). | Normative |
| **Excluded** | `4xx` (Client Fault / Mixed Schema) and `5xx` (Internal Error). | Normative |
| **Numerator** | Total Valid decisions where `xg2g.decision.schema == "legacy"`. | Normative |

> [!IMPORTANT]
> Excluding `4xx/5xx` prevents malformed-request spikes or infrastructure outages from distorting the sunset timeline.
