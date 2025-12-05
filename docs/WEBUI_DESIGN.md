# WebUI Design (xg2g v3.0.0)

## 1. Goals & Scope

### 1.1 Primary Goals

- Provide a **zero-SSH** management experience for xg2g
- Make setup and troubleshooting accessible for **non-technical homelab users**
- Keep the WebUI **lightweight, embeddable, and secure** for home networks

### 1.2 Non-Goals (for initial version)

- No full multi-tenant / multi-user RBAC
- No complex theming system (light/dark mode is sufficient)
- No external SaaS backend; WebUI is fully self-hosted

---

## 2. Target Users & Use Cases

### 2.1 User Personas

- **Homelab Power User**
  - Runs Proxmox/Docker/K8s
  - Wants insight into streams, health, EPG, logs
- **Advanced Home User**
  - Can deploy a container, but does not want to touch SSH
  - Wants a simple way to:
    - Select bouquets
    - Check if EPG and streams work
    - Trigger a manual refresh

### 2.2 Core Use Cases (Initial)

1. See if xg2g is **healthy**
   - Are receiver and EPG reachable?
   - Are there current errors/warnings?
2. Configure basic settings
   - OpenWebIf URL, credentials
   - Bouquets selection
   - Refresh interval
   - TLS / Proxy-URLs toggle
3. Inspect channels & EPG
   - List of channels per bouquet
   - Check mapping status (EPG present / missing)
4. View recent warnings / errors
   - Subset of logs focused on user-relevant issues

---

## 3. Architecture Overview

### 3.1 High-Level Decision

We will implement an **embedded WebUI** served directly by the xg2g daemon:

- Backend: existing Go daemon (`cmd/daemon`), using Chi
- Frontend: **SPA with React**, built to static assets and served by xg2g
- Data exchange: JSON over HTTPS (where enabled)

#### Why React SPA over HTMX (for this project)?

- We already have a non-trivial domain model (channels, EPG, health, logs)
- We likely want richer interactions later (filters, search, live-refresh)
- React SPA works well with existing API-style endpoints and future expansion
- HTMX would be simpler HTML-wise, but less flexible once complexity grows

If resources are constrained, we can still start with a **minimal React app** and grow as needed.

### 3.2 Components

- **Backend (Go, existing chi router)**
  - Adds `/api/*` endpoints for:
    - `/api/health`
    - `/api/config`
    - `/api/bouquets`
    - `/api/channels`
    - `/api/epg/status`
    - `/api/logs/recent`
  - Adds static file serving for `/ui/*` (React build)
  - Adds redirect: `/` → `/ui/`

- **Frontend (React)**
  - Built as an asset bundle: `webui/build/*`
  - Served by xg2g with cache headers
  - Uses fetch/axios to call `/api/*` endpoints

### 3.3 Deployment Model

- **Single binary**:
  - Dev: `xg2g-daemon --webui` (default on)
  - Option to disable: `XG2G_WEBUI_ENABLED=0` for minimal environments

- **Container**:
  - WebUI enabled by default
  - Accessible via `http(s)://host:port/ui/`
  - Recommended: place behind Caddy/Traefik for TLS in real setups

---

## 4. Backend Design

### 4.1 New API Endpoints

#### `GET /api/health`

Returns overall health:

```json
{
  "status": "ok",
  "receiver": {
    "status": "ok",
    "last_check": "2025-11-28T18:42:00Z"
  },
  "epg": {
    "status": "degraded",
    "missing_channels": 12
  },
  "version": "v2.2.0",
  "uptime_seconds": 123456
}
```

#### `GET /api/config`

Returns effective config (sanitized):

```json
{
  "openwebif_url": "http://receiver.local:80",
  "bouquets": ["TV-Favorites", "HD-Channels"],
  "refresh_interval_minutes": 30,
  "tls_enabled": true,
  "proxy_urls_enabled": true
}
```

#### `PUT /api/config`

Allows updating modifiable parts of config (e.g. via config file abstraction).
For v1 of WebUI, we can make this best-effort and require a restart for some changes.

```json
{
  "openwebif_url": "http://receiver.local:80",
  "bouquets": ["TV-Favorites"],
  "refresh_interval_minutes": 30
}
```

#### `GET /api/bouquets`

List available bouquets discovered from receiver:

```json
[
  {
    "name": "TV-Favorites",
    "services": 42
  },
  {
    "name": "HD-Channels",
    "services": 63
  }
]
```

#### `GET /api/channels?bouquet=TV-Favorites`

Returns channels with mapping/EPG status:

```json
[
  {
    "number": 1,
    "name": "Das Erste HD",
    "tvg_id": "das-erste-hd-3fa92b",
    "has_epg": true,
    "logo": "http://.../logo.png"
  },
  ...
]
```

#### `GET /api/logs/recent?limit=200`

Returns recent warnings/errors relevant to user:

```json
[
  {
    "timestamp": "2025-11-28T18:40:01Z",
    "level": "WARN",
    "component": "epg",
    "message": "EPG fetch delayed, retry in 5m"
  },
  ...
]
```

### 4.2 Auth & Security

Initial assumption: WebUI runs in trusted home network.

- v1: No auth, but:
  - Option to bind only to 127.0.0.1
  - Rely on reverse proxy for TLS & auth if needed
- v2: Optional basic-auth or token-based header

---

## 5. Frontend Design

### 5.1 Technology

- **React** (with functional components & hooks)
- Simple component library or custom minimal styling (CSS/Tailwind)
- Build tool: **Vite** or CRA (Vite preferred for speed)

### 5.2 Pages / Views

1. **Dashboard**
   - Health status (receiver, EPG, daemon)
   - Current version & uptime
   - Quick indicators:
     - "EPG coverage: 94 %"
     - "Active streams: 3"

2. **Config**
   - OpenWebIf URL
   - Credentials (if any, masked)
   - Bouquets selection (multi-select)
   - Refresh interval
   - Buttons:
     - "Test Connection"
     - "Save & Apply"

3. **Channels & EPG**
   - Per-bouquet view
   - Channel list with:
     - Number, name, tvg-id, EPG status
   - Filters:
     - "Only channels without EPG"
     - "Only HD"

4. **Logs**
   - Recent warnings & errors
   - Filter nach Component: Proxy / EPG / OpenWebIf / System
   - Manual "Refresh" button

5. **About**
   - Version, build date, commit
   - Links to docs, GitHub, release notes

---

## 6. Implementation Plan

### Phase 1: Backend API Skeleton

- Add `/api/health`, `/api/config`, `/api/bouquets`, `/api/channels`, `/api/logs/recent`
- Wire endpoints into existing manager/daemon state
- Add unit tests for JSON handlers

### Phase 2: WebUI Core (React)

- Set up `webui/` folder with React + Vite
- Implement basic layout and routing `/ui/*`
- Implement Dashboard using `/api/health`
- Implement Config page (read-only first, then editable)

### Phase 3: Channels & EPG

- Implement Channels view with filters
- Hook into `/api/channels?bouquet=...`
- Show EPG coverage info (from `/api/epg/status` or part of existing endpoints)

### Phase 4: Logs & Polish

- Implement Logs page
- Tailor error messages for user readability
- Add minimal theme (light/dark)

### Phase 5: Hardening & Release

- Integration tests: WebUI + daemon
- Performance test with large channel/EPG sets
- Update README + screenshots
- Tag v3.0.0

---

## 7. Open Questions

- Should config changes via WebUI be persisted to:
  - Existing YAML/TOML config file, or
  - A separate user-config overlay file?
- Should the WebUI be enabled by default in all builds and images?
- When to introduce authentication (if at all) for typical home-usage patterns?

---

## 8. Summary

The WebUI will be:

- Embedded in the existing xg2g daemon
- Implemented as a small React SPA
- Focused initially on:
  - Health & visibility
  - Basic configuration
  - EPG/channel overview

This design keeps the system simple, self-contained, and ready to evolve, while creating immediate value for users who currently rely on SSH and logs to manage xg2g.
