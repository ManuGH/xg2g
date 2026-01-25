# Telemetry Traceability Index

This document maps system states to the Normative Telemetry Events defined in `contracts/telemetry.schema.json`.

## Contract Consumption (`ui.contract.consumed`)

| State | Mode | Trigger | Rationale |
|---|---|---|---|
| **Normative Playback** | `normative` | V3/V4 Backend decision (`selectedOutputUrl`) | We are consuming the strictly negotiated contract. |
| **Legacy Playback** | `legacy` | Missing decision, fallback to `url` | We are relying on P3-x backward compatibility. |

## Contract Violations (`ui.failclosed`)

| Code | Context | Trigger |
|---|---|---|
| **PAYLOAD_VIOLATION** | `V3Player.decision.selectionMissing` | `decision` object exists but `selectedOutputUrl` is missing. |

## Error Semantics (`ui.error`)

| Status | Code | Meaning | Retry Policy |
|---|---|---|---|
| **401/403** | `AUTH_DENIED` | Session token invalid or scope missing. | Fatal. Re-login required. |
| **409** | `LEASE_BUSY` | No tuner available. | **Retry-After** header (UI shows countdown). |
| **410** | `GONE` | Recording deleted or session evicted. | Fatal. Stop playback. |
| **503** | `UNAVAILABLE` | Transcoder warming up / System overload. | **Retry-After** header (UI shows spinner). |

## Operational KPIs (Derivable)

- **Legacy consumption rate**: `count(legacy) / count(total sessions)`
- **Lease rejection rate**: `count(LEASE_BUSY) / count(total attempts)`
- **Fail-close rate**: `count(ui.failclosed)` (Should be 0)
