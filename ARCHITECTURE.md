# xg2g v3 Architecture

> **TL;DR**: Single-tenant home-lab control plane for Enigma2 → HLS streaming.
> Optimized for stability & operational simplicity over horizontal scalability.
> Production-ready for personal/family use, not multi-tenant SaaS.

---

## Design Philosophy

xg2g v3 is a **single-tenant, home-lab focused control plane** for live TV streaming from Enigma2 receivers. It optimizes for:

1. **Stability** → Reliable operation over weeks/months without intervention
2. **Debuggability** → Clear observability when things go wrong
3. **Operational Simplicity** → Minimal moving parts, explicit configuration
4. **Streaming Performance** → Low latency HLS delivery (secondary to stability)

This is **not** designed for:

- Multi-tenant SaaS deployments
- Horizontal scaling across data centers
- Complex user management / federated auth
- Sub-second real-time responsiveness

---

## System Context

### Target Environment

- **Deployment**: Single-user or family home network
- **Scale**: 1-4 concurrent streams, 1-8 tuners
- **Lifespan**: Long-running process (weeks between restarts)
- **Network**: Trusted LAN or secure tunnel (Tailscale/Wireguard)

### Threat Model

#### Assumptions (Required for Security)

- **Trusted network** → Home LAN or secure tunnel (Tailscale/Wireguard)
- **Single owner/family** → All authenticated users are trusted
- **Hardened host** → OS/container patched, no malware
- **Physical security** → Device in owner's physical control
- **Network perimeter** → Firewall/NAT protects from public internet

#### Threats Mitigated (In Scope)

- **Authentication bypass** → Multi-token RBAC prevents unauthorized access
- **Path traversal** → HLS file serving validates basenames + symlink resolution
- **CSRF** → Middleware exists but is not wired globally; relies on token auth + SameSite cookies
- **Rate-based DoS** → Global (1000 req/s) + per-IP (100 req/s) limits
- **Secrets in logs** → Sensitive fields (tokens, passwords) masked
- **Privilege escalation** → Scope-based authorization (read/write/admin)

#### Acceptable Risks (Out of Scope)

- **Multi-tenant isolation** → Single-owner assumption
- **DDoS/volumetric attacks** → Requires upstream firewall
- **Advanced session hijacking** → UUID + HTTPS sufficient for home-lab
- **Token expiration** → Manual rotation acceptable (not SaaS)
- **Compliance audit trail** → Structured logs sufficient (not regulated)

---

## Architecture Overview

### Event-Driven Control Plane

```
┌──────────────┐
│  HTTP API    │ ← Intents (POST /api/v3/intents)
│ (handlers_v3)│ → 202 Accepted {sessionId}
└──────┬───────┘
       │ Publish Event
       ↓
┌──────────────┐
│  Event Bus   │ ← In-Memory Pub/Sub
│ (bus.go)     │ → session.start, session.stop, lease.lost, pipeline.tick
└──────┬───────┘
       │ Subscribe
       ↓
┌──────────────┐
│ Orchestrator │ ← Consumes Events
│ (worker/)    │ → Manages Leases, FFmpeg Lifecycle
└──────┬───────┘
       │ Update State
       ↓
┌──────────────┐
│ State Store  │ ← Single Source of Truth
│ (store/)     │ → BoltDB / Badger / Memory
└──────────────┘
```

### Key Design Decisions

#### 1. In-Memory Event Bus (Not Persistent)

**Decision**: Use lightweight in-memory pub/sub instead of NATS JetStream or Redis Streams.

**Rationale**:

- **Simplicity**: No external dependencies or network hops
- **Latency**: Sub-millisecond event delivery
- **Failure Model**: Sessions are ephemeral (tied to tuner + FFmpeg process). On restart, clean slate is safer than replaying stale events.

**Trade-off**: Events lost on process restart.

- **Acceptable**: State Store is source of truth. Worker rebuilds from persisted sessions on startup.
- **Not Acceptable If**: You need 24/7 operation with zero session loss on deployment.

**Upgrade Path**: Replace with NATS JetStream if durability becomes critical.

---

#### 2. Token-Based Auth (Not JWT/OIDC)

**Decision**: Use long-lived API tokens with scope-based RBAC instead of JWT + refresh tokens.

**Rationale**:

- **No Clock Skew**: Static tokens don't expire, no clock sync issues
- **No Refresh Flow**: Simpler client implementation (no token rotation logic)
- **Explicit Rotation**: Config-based rotation (update env var + restart) is auditable
- **Multi-Token Support**: Different scopes per client (read-only mobile, admin desktop)

**Trade-off**: No automatic expiration.

- **Acceptable**: Home-lab assumes single owner. Compromise = rotate token manually.
- **Not Acceptable If**: Multiple untrusted users or regulatory compliance (PCI-DSS, SOC2).

**Upgrade Path**: Add JWT support for federated auth if external IdP needed.

---

#### 3. Session-Level Access Control (Not Per-User Isolation)

**Decision**: Any authenticated user with `v3:read` can access any session's HLS stream.

**Rationale**:

- **Single-Tenant Assumption**: All users in household are trusted
- **Simplicity**: No session-to-user binding or ownership tracking
- **UUID Obscurity**: Session IDs are UUIDs (128-bit entropy) → unguessable

**Trade-off**: No multi-user isolation.

- **Acceptable**: Home-lab use case assumes family trust model.
- **Not Acceptable If**: Multiple untrusted clients (e.g., public demo, shared apartment).

**Upgrade Path**: Add signed HLS URLs with session-owner binding if needed.

---

#### 4. State Machine with Single-Writer Leases

**Decision**: Tuner allocation uses optimistic locking (lease-based coordination) instead of distributed locks.

**Rationale**:

- **Correctness**: Prevents over-subscription (more streams than tuners)
- **Resilience**: Lease expiry handles crashed workers (self-healing)
- **No Coordination Service**: No ZooKeeper/etcd dependency

**Trade-off**: Lease contention causes temporary failures (retries needed).

- **Acceptable**: Home-lab has low concurrency (1-4 streams). Retry succeeds within 1-2s.
- **Not Acceptable If**: 10+ concurrent users with instant-on expectations.

---

#### 5. Atomic Lease Acquisition (TOCTOU Prevention)

**Decision**: Lease acquisition uses atomic try-acquire, not probe-then-acquire.

**Rationale**:

- **Correctness**: Prevents TOCTOU (Time-of-check Time-of-use) race conditions
- **Admission Control**: HTTP 409 returned before session creation on contention
- **No Queueing**: Fail-fast instead of implicit queue building
- **Atomic Guarantee**: Single atomic operation ensures either lease acquired OR 409 returned

**Trade-off**: Client must implement retry logic for 409 responses.

- **Acceptable**: Home-lab clients can retry with exponential backoff (1-2s typical)
- **Not Acceptable If**: You need guaranteed admission with queuing semantics

**Non-Goals** (explicitly excluded):

- No worker refactor as part of admission control
- No request queueing or waiting
- No new session states for "waiting"

---

## V3 Session Lifecycle State Machine

The V3 control plane implements an event-driven state machine for session lifecycle management.

**Key States**:

- **STARTING** → Worker initializing, FFmpeg starting
- **PRIMING** → FFmpeg running, producing HLS artifacts (not yet playable)
- **READY** → **Playable guarantee** (playlist + at least one segment atomically published)
- **DRAINING/STOPPING** → Graceful shutdown in progress
- **FAILED/CANCELLED/STOPPED** → Terminal states

**Critical Guarantees**:

- **Admission Guard**: No session created without successful lease acquisition (atomic try-acquire)
- **READY Guard**: READY state guarantees immediate playability (playlist + segment exist)
- **Timeout Guard**: Each phase has deadlines to prevent infinite waits
- **Client Contract**: Normative polling behavior and error handling rules

**Full specification**: See [V3_FSM.md](docs/V3_FSM.md) for complete state semantics, transition table, and normative client behavior contract.

---

## API Design Principles

### 1. Intent-Based API (Not Direct Control)

**Pattern**: Client submits **intent** (e.g., "I want to watch CNN"), not imperative commands (e.g., "allocate tuner 2, start FFmpeg").

**Benefits**:

- **Decoupling**: API layer doesn't block on slow operations (FFmpeg startup takes 3-5s)
- **Async Feedback**: Client polls `/sessions/{id}` for state transitions
- **Resilience**: Worker failures don't block HTTP responses

**Example**:

```http
POST /api/v3/intents
{"serviceRef": "1:0:1:445D:453:1:C00000:0:0:0:", "profileID": "hls_720p"}

→ 202 Accepted {"sessionId": "uuid", "status": "accepted"}

GET /api/v3/sessions/{uuid}
→ 200 OK {"state": "ready", "correlationID": "hls_uuid"}
```

---

### 2. Structured Error Responses (RFC 7807 Style)

**Pattern**: All errors return consistent JSON with machine-readable codes.

```json
{
  "code": "UNAUTHORIZED",
  "message": "Authentication required",
  "request_id": "req_abc123",
  "details": "Invalid token format"
}
```

**Benefits**:

- **Debuggability**: `request_id` links HTTP → logs → traces
- **Client Automation**: Machine codes enable retry logic (e.g., 503 → backoff)
- **Consistency**: No mix of plain text, JSON, and HTTP status

---

### 3. Scope-Based RBAC (Hierarchical)

**Model**:

```
*           → All operations (superuser)
v3:*        → All v3 operations
v3:admin    → Admin operations (implies v3:write + v3:read)
v3:write    → Write operations (implies v3:read)
v3:read     → Read-only access
```

**Enforcement**:

- **Middleware-Level**: Route middleware checks scopes before handler
- **Fail-Closed**: No tokens configured → deny all
- **Token-Only**: API access always requires a valid token

---

## API Conventions

### Error Response Format

All errors use structured JSON with machine-readable codes:

```json
{
  "code": "UNAUTHORIZED",
  "message": "Authentication required",
  "request_id": "req_abc123",
  "details": "Invalid token format"
}
```

### Pagination Parameters

List endpoints support `offset` (default 0) and `limit` (default 100, max
1000):

```http
GET /api/v3/sessions?offset=0&limit=50
```

Response includes metadata: `total`, `count`, `offset`, `limit`

### HTTP Status Code Policy

- **200 OK**: Successful read operation
- **202 Accepted**: Async operation started (intent accepted)
- **400 Bad Request**: Invalid input (`INVALID_INPUT`)
- **401 Unauthorized**: Missing/invalid auth (`UNAUTHORIZED`)
- **403 Forbidden**: Insufficient scopes (`FORBIDDEN`)
- **404 Not Found**: Resource not found (`SESSION_NOT_FOUND`)
- **429 Too Many Requests**: Rate limit exceeded (`RATE_LIMIT_EXCEEDED`)
- **503 Service Unavailable**: Temporary unavailability (`V3_UNAVAILABLE`)

---

## Observability

### 1. Request ID Propagation

- Every HTTP request gets a unique ID (`X-Request-ID` header or generated)
- Logged at API, event bus, worker, and FFmpeg layers
- Enables end-to-end tracing (HTTP → event → FFmpeg log)

### 2. Structured Logging (zerolog)

- JSON output for machine parsing
- Contextual fields: `component`, `sessionID`, `tunerID`, `reason`
- Log levels: `DEBUG` → `INFO` → `WARN` → `ERROR`

### 3. Prometheus Metrics

- HTTP latency histograms per endpoint
- Active session count by state
- Tuner utilization (leased vs available)
- FFmpeg process lifecycle events

### 4. OpenTelemetry Tracing (Optional)

- Distributed traces via OTLP exporter
- Disabled by default (enable via `XG2G_OTEL_ENDPOINT`)

---

## Security Hardening

### Defense-in-Depth Layers

1. **Authentication**: Multi-token RBAC with constant-time comparison
2. **Authorization**: Scope middleware on all v3 routes
3. **CSRF Protection**: Middleware exists but is not wired globally; mutating routes rely on token auth + SameSite cookies
4. **Path Traversal**: Basename-only HLS file serving + symlink detection
5. **Rate Limiting**: Global (1000 req/s) + per-IP (100 req/s) limits
6. **Input Validation**: `DisallowUnknownFields` on JSON decoding
7. **Secure Headers**: CSP, HSTS, X-Frame-Options, X-Content-Type-Options
8. **Request Size Limits**: 1MB max body on `/intents`

### Security Features (Supported)

- ✅ **TLS 1.3** - Via reverse proxy or `--tls` flag
- ✅ **HTTP/2** - Go net/http default
- ✅ **Constant-time token comparison** - Prevents timing attacks
- ✅ **Secure cookie flags** - `HttpOnly`, `Secure`, `SameSite=Strict`
- ✅ **Fail-closed auth** - No default credentials
- ✅ **Structured errors** - No sensitive info leakage
- ✅ **Rate limiting** - Global + per-IP limits
- ✅ **Audit logging** - Structured JSON logs
- ✅ **RBAC** - Scope-based authorization (read/write/admin)
- ✅ **OpenAPI spec** - Machine-readable API contract

### Security Features (Not Planned)

- ❌ **JWT/refresh tokens** - Long-lived tokens simpler for home-lab
- ❌ **OAuth2/OIDC** - No external IdP needed
- ❌ **WAF integration** - Reverse proxy handles this
- ❌ **Token rotation** - Manual via config update
- ❌ **Session binding** - UUID obscurity sufficient

---

## Stream Resolution Standards

**Critical operational standards** to prevent regression of stream resolution.

**Core Invariants**:

1. **NEVER hardcode stream ports** - Enigma2 dynamically assigns ports based on channel configuration
2. **WebAPI is the source of truth** - Always resolve stream URLs via `/web/stream.m3u?ref=...` using `ResolveStreamURL()`
3. **Parse returned URLs** - Extract host, port, and path from the WebAPI response

**Why this matters**: Early versions hardcoded ports, causing silent failures (black screen) for certain channels. These invariants capture hard-won operational lessons.

**Full specification**: See [STREAMING.md](docs/STREAMING.md) for detailed rules, implementation checklist, and common pitfalls.

## VOD Playback Policy

**Design Decision: Direct MP4 Startup**

For Direct MP4 playback, the client implements an aggressive optimization to minimize Time-to-First-Picture (TTFP):

- **Optimistic Polling**: When receiving `503 Service Unavailable` + `Retry-After`, the client **ignores the duration** specified in the header.
- **Why**: The backend sets a conservative estimate (e.g., 5s), but the remux job often completes sooner.
- **Behavior**: The client polls at a **1-second interval** to catch the "Ready" state immediately.
- **Contract**: `Retry-After` serves only as a **capability flag** indicating that the 503 is a temporary "Preparing" state (safe to retry) rather than a hard failure.

---

## Non-Goals (Explicit)

These are **intentionally excluded** to maintain simplicity:

1. **OAuth2/OIDC Integration**
   - **Why**: Adds complexity (clock skew, refresh tokens, IdP dependency)
     without benefit in single-tenant home-lab
   - **When to add**: If you need Google/GitHub login or federated auth

2. **Persistent Event Bus (NATS/Redis)**
   - **Why**: In-memory pub/sub is faster, simpler, and safer (clean restart
     vs replaying stale events)
   - **When to add**: If 24/7 uptime with zero session loss on restart is
     critical

3. **GraphQL API**
   - **Why**: REST + OpenAPI is sufficient for home-lab clients; GraphQL adds
     operational complexity
   - **When to add**: If complex UI requires flexible queries (unlikely for
     streaming use case)

4. **Multi-Region Deployment**
   - **Why**: Home-lab is single-instance by definition; tuners are physical
     devices
   - **When to add**: Never (fundamentally out of scope)

5. **Sub-Second End-to-End Latency**
   - **Why**: HLS protocol has 3-5s inherent latency (segment duration +
     buffering)
   - **When to add**: Never (physical constraint of HLS; use RTSP for
     low-latency)

6. **Signed HLS URLs (Per-Session Auth)**
   - **Why**: UUID session IDs (128-bit entropy) + HTTPS + trusted network =
     sufficient security
   - **When to add**: If exposing to untrusted clients or public internet
     without VPN

---

## FAQ

### Q: Why no persistent event bus?

**A**: Events are ephemeral triggers, not durable state. State Store is the source of truth. On restart, worker rebuilds from persisted sessions. This is simpler and safer than replaying stale events.

### Q: Why not JWT tokens?

**A**: JWT adds complexity (clock skew, refresh flow, storage) with no benefit in single-tenant home-lab. Long-lived tokens are simpler and explicit rotation is auditable.

### Q: Is this production-ready?

**A**: Yes, for **home-lab production** (24/7 streaming for personal use). Not for multi-tenant SaaS or commercial deployment (see Threat Model).

### Q: How do I scale horizontally?

**A**: You don't. This is designed for vertical scaling (more tuners, faster CPU). If you need multiple instances, you're outside the design scope.

### Q: Can I expose this to the internet?

**A**: Yes, with caveats:

- Use **TLS** (reverse proxy or `--tls` flag)
- Use **secure tunnel** (Tailscale, Wireguard, Cloudflare Tunnel)
- Enable **rate limiting** (default enabled)
- Consider **IP allowlist** (firewall rules)
- **Do not** expose without HTTPS (tokens in plaintext)

---

## Upgrade Paths (If Needed)

If your use case grows beyond home-lab:

### 1. Multi-User Isolation

**Trigger**: You need to support untrusted users OR shared apartment/office
environment

**Implementation**:

- Add user claims to auth middleware (bind token → user ID)
- Store session owner in `SessionRecord.contextData["user_id"]`
- Filter `/sessions` list by ownership
- Add signed HLS URLs with owner verification

### 2. Event Bus Durability

**Trigger**: You require zero session loss on process restart (24/7 uptime
SLA)

**Implementation**:

- Replace in-memory bus with NATS JetStream or Redis Streams
- Configure retention policy (e.g., 24h replay window)
- Add worker resume logic (replay missed events on startup)
- Add event deduplication (idempotency keys)

### 3. OAuth2/OIDC Integration

**Trigger**: You want external identity provider (Google, GitHub, Keycloak)

**Implementation**:

- Add JWT validation middleware (verify signature + claims)
- Support `Bearer` tokens with `sub`, `scope`, `exp` claims
- Integrate with OIDC discovery endpoint
- Map OIDC groups → v3 scopes

### 4. Advanced Monitoring & Alerting

**Trigger**: You need proactive alerting OR multi-instance deployment

**Implementation**:

- Export metrics to Prometheus/Grafana Cloud
- Set up alerts: session failure rate >10%, tuner saturation >80%
- Add distributed tracing (Jaeger/Tempo) for cross-service debugging
- Implement health check endpoint with dependency checks

---

## Version History

- **v3.0.0** (2025-12-24): Initial release with event-driven architecture, RBAC, pagination, OpenAPI spec
- **Unreleased**: Recording playback enhancements, improved documentation structure

---

## License

PolyForm Noncommercial License 1.0.0. Commercial use restricted.

## Appendix A: Threat Model & Security Hardening

### 1. Trust Boundaries

- **External Network**: The API is exposed on `:8088`. While token auth is enforced for API operations, it is designed for single-tenant home-lab use.

- **Enigma2 Receiver**: Trusted internal component. Communication is HTTP (Basic Auth supported).
- **Filesystem**: configuration and data are stored in `XG2G_DATA` (default: `./data`).

### 2. Hardening Measures

The system utilizes robust defaults to minimize the attack surface:

- **Configuration Integrity**: Config loading is strict. Directories disguised as config files are rejected. Explicit `--config` paths must allow fast failure if invalid.
- **Systemd Isolation**:
  - **Base Hardening (xg2g.service)**:
    - `PrivateTmp=true`: Isolates temporary files.
    - `NoNewPrivileges=true`: Prevents privilege escalation.
    - `LimitNOFILE=65535`: Mitigates DoS via file descriptor exhaustion.
    - `UMask=0077`: Ensures secrets/config are not world-readable.
  - **Optional Extended Hardening (Drop-in)**:
    - `LockPersonality`, `RestrictSUIDSGID`, `PrivateDevices`, `ProtectControlGroups`, `ProtectKernelTunables`, `RestrictNamespaces` can be enabled via `/etc/systemd/system/xg2g.service.d/hardening.conf`.

### 3. Known Risks & Mitigations

- **Fail-Closed Auth**: Authentication is enforced by default. Startup fails if no token is configured.

- **Root Execution**: Currently, the service typically runs as `root` to simplify permission management for Docker/CI workflows.
  - *Mitigation*: Systemd hardening reduces the impact. Future Roadmap includes migration to a dedicated `xg2g` user.
  - *Mitigation*: Systemd hardening reduces the impact. Future Roadmap includes migration to a dedicated `xg2g` user.
- **API Token Storage**: Tokens are stored in `config.yaml`.
  - *Mitigation*: `UMask=0077` prevents unauthorized read access on the host.
