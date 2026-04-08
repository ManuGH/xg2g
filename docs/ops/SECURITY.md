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

### Operator Requirement

- Local loopback access (`http://localhost:8088` on the same host) may stay on
  plain HTTP for smoke tests and one-box development.
- Any browser-facing deployment reached from another device or hostname must be
  served over HTTPS, either directly in xg2g or through a trusted HTTPS proxy.
- When xg2g is intentionally deployed behind a trusted HTTPS proxy, the backend
  hop from proxy to xg2g may remain plain HTTP on an internal network. Startup
  cleartext-token warnings are therefore suppressed when `trustedProxies` is
  configured. Direct cleartext access to the xg2g listener is still an operator
  responsibility and should remain LAN-scoped or otherwise blocked.
- If xg2g sees a non-loopback browser request as plain HTTP,
  `POST /api/v3/auth/session` fails closed with `400 HTTPS required`. The
  `xg2g_session` cookie is not minted, native HLS media requests to
  `/api/v3/sessions/{id}/hls/*` fail authorization, and Safari/native players
  can collapse into generic playback errors such as `Video Error: 4`.

Browser-based clients (integrations) must use the following flow to obtain access to the Data Plane:

1. **API Authentication**: The client authenticates against the Control Plane using a Bearer Token.
2. **Session Exchange**: The client makes a `POST` request to `/api/v3/auth/session` with the Bearer Token.
3. **Cookie Issuance**: The server validates the token and responds with a `Set-Cookie` header:
   - **Name**: `xg2g_session`
   - **HttpOnly**: `true` (Prevent XSS access)
   - **SameSite**: `Lax` (Prevent cross-site ambient sends while preserving top-level navigation flows)
   - **Path**: `/api/v3/` (Scoped to API/Media routes)
   - **Secure**: `true` on HTTPS or trusted HTTPS proxy requests
   - **Transport Rule**: `/api/v3/auth/session` rejects plain HTTP unless the request originates from loopback (`127.0.0.1` / `::1`)
4. **Media Access**: Subsequent requests to `/api/v3/sessions/{id}/hls/*` will include the cookie automatically.

## Household PIN Policy

Household protection is optional and operator-controlled.

- Without `household.pin` or `household.pinHash`, household profiles remain a
  convenience and scoping feature only.
- With a configured PIN, protected household actions are enforced server-side:
  switching to adult profiles, household settings access, and logout from a
  child profile.
- Browser/UI prompts are only UX. The backend remains the source of truth.

### Configuration Inputs

Use exactly one of these YAML inputs:

- `household.pin`: operator input in plaintext. The loader hashes it before the
  runtime config is stored.
- `household.pinHash`: pre-hashed form for deployments that never want the
  plaintext PIN written to disk.

`household.pinHash` is the only form that is persisted back out through config
management surfaces. Do not place a plaintext PIN in screenshots, examples,
support bundles, or logs.

Optional tuning:

- `household.unlockTTL`: duration string controlling how long a successful
  household unlock stays active on the server. Default is `4h` and it is never
  allowed to outlive the authenticated session lifetime.

### Unlock Semantics

- Unlock state is stored server-side and bound to the `xg2g_household_unlock`
  cookie.
- The cookie is session-scoped in the browser and is also invalidated by the
  server-side unlock TTL.
- Unlock state ends on logout, explicit relock, browser-session end, or TTL
  expiry, whichever comes first.
- This is a practical household gate, not a hardened boundary against an
  already-unlocked and unattended client.

## Legacy Token Migration (X-API-Token)

Legacy `X-API-Token` header/cookie sources are supported only for migration and can be disabled explicitly:

- **Flag**: `XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES=true`
- **Default**: `false` (legacy sources still accepted during migration)
- **Recommended rollout**:
  1. Migrate clients to `Authorization: Bearer <token>` (API) and `xg2g_session` cookie (media).
  2. Enable `XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES=true`.
  3. Monitor auth logs for `auth.legacy_token_source` before and during cutover.

## Admission Control: Fail-Closed Policy

The system implements a "Fail-Closed" policy for resource admission:

- **State Unknown**: If the system cannot determine the current resource usage (e.g., due to a database failure), it returns `503 Service Unavailable` with problem code `ADMISSION_STATE_UNKNOWN`.
- **Engine Disabled**: If the streaming engine is disabled, all media requests are rejected with `503`.

## Live Stream Decision Token (JWT)

Live stream intent requests (`POST /api/v3/intents`) require a short-lived HS256 JWT signed by the
server. The signing key is a shared secret that **must** be configured before the service starts.

### Required Environment Variable

| Variable | Required | Min Length | Description |
| :--- | :--- | :--- | :--- |
| `XG2G_DECISION_SECRET` | **Yes** | 32 ASCII bytes | HMAC-SHA256 signing key for playback decision tokens. |

The service **refuses to start** if this variable is missing, empty, or whitespace-only.
The systemd `ExecStartPre` gate enforces the 32-byte minimum before any container is started.

> **Byte-count clarification:** The length check uses `wc -c` (raw byte count). For pure ASCII
> characters (hex, base64) one character equals one byte, so the check is unambiguous. Avoid
> Unicode or multi-byte characters in the secret value — use hex or base64 output as shown below.

**Recommended: generate with `openssl` (produces unambiguous ASCII output):**
```bash
# 256-bit key as hex (64 ASCII chars = 64 bytes — clearly above the 32-byte floor)
openssl rand -hex 32

# Alternative: base64url (43 ASCII chars = 43 bytes, 256 bits of entropy)
openssl rand -base64 32 | tr -d '=' | tr '+/' '-_'
```

Add to `/etc/xg2g/xg2g.env` (`root:root`, mode `0600`):
```
XG2G_DECISION_SECRET=<openssl-output>
```

**Verify length before deploy:**
```bash
secret="$(grep XG2G_DECISION_SECRET /etc/xg2g/xg2g.env | cut -d= -f2)"
printf '%s' "$secret" | wc -c   # must be >= 32
```

### Secret Rotation

**Rotation model: single-key, restart-based.**

JWT tokens are short-lived (≤ 120 s). This means the rotation window is at most 2 minutes, which is
acceptable for a single-instance deployment. No dual-key scheme is implemented.

**Rotation procedure:**
1. Generate a new secret: `openssl rand -hex 32`
2. Update `XG2G_DECISION_SECRET` in `/etc/xg2g/xg2g.env`
3. Restart the service: `systemctl restart xg2g`
4. **Expected transient behaviour during restart:** clients holding a token signed by the old key
   will receive a `401 TOKEN_INVALID_SIG` on their next intent request for up to one token TTL
   (≤ 120 s). This is by design. Compliant players re-initiate the intent flow automatically and
   receive a new token signed with the new key. No manual intervention is required; the error window
   is bounded by the TTL and does not persist beyond it.

**Dual-key rotation (multi-instance only):**
If you ever run more than one `xg2g` instance behind a load balancer, implement zero-downtime rotation
with a second variable `XG2G_DECISION_SECRET_PREV`:
- Deploy new instances with both `SECRET` (new) and `SECRET_PREV` (old).
- Verification accepts tokens signed by either key; new tokens are signed by `SECRET` only.
- After all old-key tokens have expired (≤ 120 s), remove `SECRET_PREV` and re-deploy.

This dual-key extension is **not** implemented in the current codebase — only add it if you scale
to multiple instances. Single-instance operators: the restart procedure above is sufficient.

## Feature Flags Governance

Feature flags are treated as a strict product surface:

- **Registry Enforcement**: All feature flag keys must be registered (Screaming Snake Case).
- **Type Safety**: Values must match registered types (e.g. Bool).
- **Primitives Only**: Values MUST be JSON primitives (bool, string, number, null). Nested structures are forbidden.
- **Unknown/Invalid Keys**: Result in `400 Bad Request` with `INVALID_INPUT`.
