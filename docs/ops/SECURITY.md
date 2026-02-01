# Security Invariants

This document formalizes the security invariants and authentication policies for the xg2g system.

## Authentication Policy: Transport Separation

To protect against Cross-Site Request Forgery (CSRF) while maintaining API usability for non-browser clients, xg2g enforces a strict separation of authentication mechanisms based on the request type:

| Request Category | Target Path | Authentication Mechanism |
| :--- | :--- | :--- |
| **Control Plane (API)** | `/api/v3/*` | `Authorization: Bearer <Token>` |
| **Data Plane (Media)** | `/api/v3/sessions/{id}/hls/*` | `Session Cookie` (`xg2g_session`) |

### Rationale

- **Bearer Tokens**: Ideal for headless automation and non-browser clients. They are not susceptible to CSRF as they are not automatically sent by the browser.
- **Session Cookies**: Required for HLS playback in standard HTML5 video players which do not support custom headers for fragment requests. Cookies are scoped to the media domain to prevent cross-site leaks.

## Browser Authentication Flow

Browser-based clients (integrations) must use the following flow to obtain access to the Data Plane:

1. **API Authentication**: The client authenticates against the Control Plane using a Bearer Token.
2. **Session Exchange**: The client makes a `POST` request to `/api/v3/auth/session` with the Bearer Token.
3. **Cookie Issuance**: The server validates the token and responds with a `Set-Cookie` header:
   - **Name**: `xg2g_session`
   - **HttpOnly**: `true` (Prevent XSS access)
   - **SameSite**: `Strict` (Prevent CSRF)
   - **Path**: `/api/v3/` (Scoped to API/Media routes)
   - **Secure**: `true` (If HTTPS is enabled)
4. **Media Access**: Subsequent requests to `/api/v3/sessions/{id}/hls/*` will include the cookie automatically.

## Admission Control: Fail-Closed Policy

The system implements a "Fail-Closed" policy for resource admission:

- **State Unknown**: If the system cannot determine the current resource usage (e.g., due to a database failure), it returns `503 Service Unavailable` with problem code `ADMISSION_STATE_UNKNOWN`.
- **Engine Disabled**: If the streaming engine is disabled, all media requests are rejected with `503`.

## Feature Flags Governance

Feature flags are treated as a strict product surface:

- **Registry Enforcement**: All feature flag keys must be registered (Screaming Snake Case).
- **Type Safety**: Values must match registered types (e.g. Bool).
- **Primitives Only**: Values MUST be JSON primitives (bool, string, number, null). Nested structures are forbidden.
- **Unknown/Invalid Keys**: Result in `400 Bad Request` with `INVALID_INPUT`.
