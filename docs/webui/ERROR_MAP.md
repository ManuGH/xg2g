# WebUI Error Semantics & Fail-Closed UX (ERROR_MAP.md)

This document defines the deterministic mapping of backend error states to operator-facing UX outcomes.

| Status | Code / Condition | UX Message | Operator Action | Component State |
| :--- | :--- | :--- | :--- | :--- |
| **401/403** | Auth Failure | "Authentication required." | Re-enter API Token | Dispatch `auth-required` |
| **409** | `LEASE_BUSY` | "Device busy (Lease in use)." | Check other clients | Error Toast (Status: `error`) |
| **410** | Session Expired | "Session expired." | Restart Stream | Status: `error`, Stop Heartbeat |
| **422** | Validation / Conflict | "Timer conflict." | Adjust begin/end times | Inline Alert (Form Red) |
| **502** | Timeout / No Ack | "Receiver not responding." | Check STB connectivity | Inline Alert |
| **503** | `Preparing` | "Preparing stream..." | Wait for retry | Status: `building` (Auto-retry) |

---

## 1. Critical Flow Handlers

### Leasing (409)

Components must respect the `Retry-After` header if present. UI must not loop blindly.

### Heartbeat (410)

If a heartbeat returns 410, the UI must:

1. Stop the heartbeat timer immediately.
2. Pause the video element.
3. Show a terminal error message.

### RFC7807 Parsing

All API errors must be parsed for `title` and `detail`. If `detail` exists, it should be surfaceable via a "Show Details" toggle.

## 2. Live Playback Truth Problems (`POST /api/v3/live/stream-info`)

Clients MUST treat `/problems/live/*` as the live truth contract. They MUST branch on `type` and may refine copy with `truthReason`. They MUST NOT derive live media truth from free-text `title` or `detail`.

| Status | Problem Type | UX Message | Operator Action | Component State |
| :--- | :--- | :--- | :--- | :--- |
| **503** | `/problems/live/scan_unavailable` | "Live stream is being verified." | Respect `Retry-After`; allow retry | Error panel (`retryable`) |
| **503** | `/problems/live/missing_scan_truth` | "Live stream is still being checked." | Respect `Retry-After`; allow retry | Error panel (`retryable`) |
| **503** | `/problems/live/partial_truth` | "Live stream details are incomplete." | Respect `Retry-After`; allow retry | Error panel (`retryable`) |
| **503** | `/problems/live/inactive_event_feed` | "Live stream is unavailable." | Respect `Retry-After`; allow retry | Error panel (`retryable`) |
| **503** | `/problems/live/failed_scan_truth` | "Live stream details could not be verified." | Respect `Retry-After`; allow retry | Error panel (`retryable`) |

### Stable Live Problem Fields

- `type`: primary UX discriminator for live truth failures.
- `code`: coarse machine family only (`SCAN_UNAVAILABLE` or `UNAVAILABLE`); never use it as the sole UX branch.
- `Retry-After` header: normative retry/backoff signal.
- `retryAfterSeconds`: JSON mirror of `Retry-After` for raw fetch flows.
- `truthState` and `truthReason`: stable live-truth classification fields.
- `truthOrigin` and `problemFlags`: diagnostic fields only; do not build primary UX branching on them.
