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
