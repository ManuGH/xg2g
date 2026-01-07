# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v3.1.x  | :white_check_mark: |
| v3.0.x  | :x:                |
| < 3.0   | :x:                |

## Threat Model & Guarantees

We operate under a "Thin Client, Smart Edge" model. The backend serves as the single source of truth for all policy decisions.

### 1. Authentication Contract (P0)

We enforce a **Fail-Closed** security model for all V3 APIs.

- **401 Unauthorized**: Returned when identity cannot be established.
  - No Token provided.
  - Invalid / Expired Token provided.
  - Malformed Authorization header.
- **403 Forbidden**: Returned when identity is known, but permission is denied.
  - Valid Token provided, but lacks required scope (e.g., `v3:admin`).

This behavior is strictly verified by `TestAuth_Invariant_Chain`.

### 2. Universal Policy Enforcement (P0)

- **Strict H.264/AAC**: The backend proactively rejects any attempt to negotiate legacy profiles.
- **Read-Only Config**: Clients must treat system configuration as read-only.

## Reporting a Vulnerability

Please report sensitive security issues via email to <security@xg2g-internal.do-not-email>.
