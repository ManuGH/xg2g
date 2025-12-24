# xg2g Architecture

This document describes the high-level architecture, component interactions, and design decisions of xg2g.

## Table of Contents

- [Overview](#overview)
- [Architecture Diagram](#architecture-diagram)
- [Component Breakdown](#component-breakdown)
- [Data Flow](#data-flow)
- [Deployment Architectures](#deployment-architectures)
- [Technology Stack](#technology-stack)

## Overview

xg2g is a middleware service that bridges Enigma2 satellite/cable receivers with media servers (Plex, Jellyfin, Emby) by:

1. **Converting** Enigma2 bouquets to M3U playlists
2. **Generating** XMLTV EPG (Electronic Program Guide)
3. **Emulating** HDHomeRun for auto-discovery
4. **Proxying** live streams with optional transcoding

### Architecture Principles

- **Stateless**: No persistent state (except cached playlists/EPG)
- **Middleware Pattern**: Sits between Enigma2 and media servers
- **Modular Design**: Separated API, V3 streaming, jobs, and shared core packages
- **12-Factor Compatible**: Configuration via file + ENV overrides
- **Deterministic Config**: ENV is read only at load/reload; a request/job runs against one config snapshot/epoch

## Architecture Diagram

### High-Level System Architecture

```mermaid
graph TB
    subgraph "Media Clients"
        Plex[Plex Media Server]
        Jellyfin[Jellyfin]
        VLC[VLC / IPTV Players]
    end

    subgraph "xg2g Middleware"
        API[HTTP API Server<br/>/api/v2/*]
        HDHR[HDHomeRun Emulation<br/>SSDP Discovery]
        Jobs[Background Jobs<br/>Refresh, EPG]
        V3[V3 Control Plane<br/>/api/v3/*]
    end

    subgraph "Backend"
        Enigma2[Enigma2 Receiver<br/>OpenWebIF]
        Storage[File Storage<br/>M3U, XMLTV]
    end

    subgraph "Observability"
        Metrics[Prometheus<br/>Metrics]
        Traces[Jaeger / Tempo<br/>Distributed Tracing]
    end

    Plex --> HDHR
    Plex --> API
    Jellyfin --> API
    VLC --> V3

    API --> Jobs
    HDHR --> API

    Jobs --> Enigma2
    Jobs --> Storage
    V3 --> Enigma2

    API --> Metrics
    Jobs --> Traces
```

### Component Architecture

```mermaid
graph LR
    subgraph "cmd/daemon"
        Main[main.go<br/>Entry Point]
    end

    subgraph "internal/daemon"
        App[app.go<br/>Lifecycle Orchestration]
    end

    subgraph "internal/api"
        Router[http.go<br/>Routing + UI]
        V2[server_gen.go + server_impl.go<br/>API v2 Handlers]
        MW[middleware/*<br/>Auth, CORS, Tracing]
    end

    subgraph "internal/jobs"
        Refresh[refresh.go<br/>Playlist/EPG Refresh]
    end

    subgraph "internal/hdhr"
        HDHR[hdhr.go<br/>HDHomeRun + SSDP]
    end

    subgraph "Supporting Packages"
        Config[config/*<br/>Configuration]
        Validate[validate/*<br/>Validation]
        Telemetry[telemetry/*<br/>OpenTelemetry]
        Log[log/*<br/>Structured Logging]
    end

    Main --> App
    App --> Router
    Router --> V2
    Router --> MW
    V2 --> Refresh
    V2 --> HDHR
    App --> Config
    V2 --> Validate
    MW --> Telemetry
    App --> Log
```

## Component Breakdown

### 1. HTTP API (`internal/api`)

**Responsibility**: Expose REST API for playlist management and status queries

**Key Endpoints**:

- `GET /api/v2/system/health` - Service health and runtime status
- `POST /api/v2/system/refresh` - Trigger playlist/EPG refresh
- `GET /api/v2/services` - List available channels (services)

**Architecture Decision**: [ADR-001 API Versioning](adr/001-api-versioning.md)

### 3. Background Jobs (`internal/jobs`)

**Responsibility**: Periodic tasks for playlist/EPG updates

**Jobs**:

- **Playlist Refresh**: Fetch bouquets, generate M3U playlist
- **EPG Refresh**: Fetch EPG data, generate XMLTV

**Concurrency**: Configurable `EPGMaxConcurrency` for parallel channel fetching

**Related**: [ADR-003 Validation Package](adr/003-validation-package.md)

### 4. HDHomeRun Emulation (`internal/hdhr`)

**Responsibility**: Emulate HDHomeRun tuner for auto-discovery in Plex/Jellyfin

**Protocols**:

- **SSDP**: UDP multicast discovery on 239.255.255.250:1900
- **HTTP**: HDHomeRun JSON API responses

**Endpoints**:

- `GET /discover.json` - Device info
- `GET /lineup.json` - Channel lineup
- `GET /lineup_status.json` - Tuner status

### 5. Configuration (`internal/config`)

**Responsibility**: Load and validate configuration from ENV, YAML, defaults

**Precedence**: ENV > File > Defaults (highest to lowest)

**Related**: [ADR-002 Config Precedence](adr/002-config-precedence.md)

### 6. Validation (`internal/validate`)

**Responsibility**: Centralized validation with chainable API

**Validators**: URL, Port, Directory, Range, NotEmpty, OneOf, Custom

**Related**: [ADR-003 Validation Package](adr/003-validation-package.md)

### 7. Telemetry (`internal/telemetry`)

**Responsibility**: Distributed tracing with OpenTelemetry

**Instrumented Components**:

- HTTP API (all requests)
- GPU Transcoding (latency, codec, device)
- EPG Refresh Jobs (duration, channel count)

**Related**: [ADR-004 OpenTelemetry](adr/004-opentelemetry.md)

### 8. v3 Control Plane (`internal/v3`)

**Responsibility**: Stateful, event-driven orchestration for advanced streaming scenarios.

**Core Components**:

- **Intent API**: `/api/v3/intents` (POST) - accepts "Start" or "Stop" intents.
- **Event Bus**: In-memory message bus for decoupling API from Worker.
- **State Store**: Persists session lifecycle (New -> Tuning -> Transcoding -> Ready).
- **Worker**: Orchestrator that acquires tuner leases and manages FFmpeg processes.

**Architecture**:

```mermaid
graph LR
    Client -->|Intent| API[v3 API]
    API -->|Event| Bus[Event Bus]
    Bus -->|Consume| Worker[v3 Worker]
    Worker -->|Update| Store[State Store]
    Worker -->|Manage| FFmpeg[FFmpeg Process]
    FFmpeg -->|HLS Segments| Disk
    Client -->|GET .m3u8| API
```

## Data Flow

### Playlist Refresh Flow

```mermaid
sequenceDiagram
    participant Client as Plex/Jellyfin
    participant API as xg2g API
    participant Jobs as Jobs Package
    participant OWI as Enigma2 OpenWebIF
    participant Storage as File Storage

    Client->>API: POST /api/v2/system/refresh
    API->>Jobs: Trigger Refresh Job
    Jobs->>OWI: GET /api/bouquets
    OWI-->>Jobs: Bouquet List
    loop For each Bouquet
        Jobs->>OWI: GET /api/services?bouquet=X
        OWI-->>Jobs: Service List
    end
    Jobs->>Jobs: Generate M3U Playlist
    Jobs->>Storage: Write playlist.m3u
    Jobs->>OWI: GET /api/epgservice (if enabled)
    OWI-->>Jobs: EPG Data
    Jobs->>Jobs: Generate XMLTV
    Jobs->>Storage: Write xmltv.xml
    Jobs-->>API: Refresh Complete
    API-->>Client: 200 OK
```

## Deployment Architectures

### Single Instance (Home Lab)

```
┌─────────────┐
│   Plex      │──┐
└─────────────┘  │
                 │
┌─────────────┐  │    ┌──────────────┐      ┌──────────────┐
│  Jellyfin   │──┼───→│    xg2g      │─────→│   Enigma2    │
└─────────────┘  │    │  (Docker)    │      │   Receiver   │
                 │    └──────────────┘      └──────────────┘
┌─────────────┐  │
│   Clients   │──┘
└─────────────┘
```

### High Availability (Multi-Instance)

```
                          ┌──────────────┐
                          │   Load       │
                          │   Balancer   │
                          └───────┬──────┘
                                  │
                   ┌──────────────┼──────────────┐
                   │              │              │
              ┌────▼────┐    ┌────▼────┐    ┌───▼─────┐
              │  xg2g   │    │  xg2g   │    │  xg2g   │
              │ Instance│    │ Instance│    │ Instance│
              │    1    │    │    2    │    │    3    │
              └────┬────┘    └────┬────┘    └────┬────┘
                   │              │              │
                   └──────────────┼──────────────┘
                                  │
                          ┌───────▼──────┐
                          │   Shared     │
                          │   Storage    │
                          │  (NFS/S3)    │
                          └──────────────┘
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: xg2g
spec:
  replicas: 3
  selector:
    matchLabels:
      app: xg2g
  template:
    metadata:
      labels:
        app: xg2g
    spec:
      containers:
      - name: xg2g
        image: xg2g:latest
        env:
        - name: XG2G_OWI_BASE
          valueFrom:
            configMapKeyRef:
              name: xg2g-config
              key: owi_base
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: xg2g-pvc
```

## Technology Stack

### Core Technologies

| Component | Technology | Why |
|-----------|------------|-----|
| **Language** | Go 1.25 | Performance, concurrency, static typing |
| **HTTP Server** | `net/http` + `go-chi/chi` | Standard library + flexible routing |
| **Logging** | `rs/zerolog` | Fast, structured, zero-allocation |
| **Metrics** | Prometheus | Industry standard, rich ecosystem |
| **Tracing** | OpenTelemetry | Vendor-neutral, future-proof |
| **Config** | YAML + ENV | Human-readable + 12-factor compliance |
| **Transcoding** | FFmpeg / External | Proven, hardware acceleration support |

### Dependencies

**Production**:

- `github.com/go-chi/chi` - HTTP routing
- `github.com/rs/zerolog` - Structured logging
- `github.com/prometheus/client_golang` - Metrics
- `go.opentelemetry.io/otel` - Distributed tracing
- `gopkg.in/yaml.v3` - YAML parsing

**Development/Testing**:

- `github.com/stretchr/testify` - Test assertions
- `github.com/google/go-cmp` - Deep comparisons

### External Services

- **Enigma2 OpenWebIF**: Required backend
- **Jaeger/Tempo**: Optional (tracing backend)
- **Prometheus**: Optional (metrics scraping)
- **GPU Transcoder**: Optional (hardware transcoding)

## Performance Characteristics

### Throughput

- **API Requests**: ~10,000 req/s (single instance)
- **Concurrent Streams**: Limited by network bandwidth
- **EPG Refresh**: ~100 channels/sec

### Resource Usage

| Scenario | CPU | Memory | Network |
|----------|-----|--------|---------|
| Idle | <1% | ~30MB | Minimal |
| EPG Refresh | 10-20% | ~50MB | 5-10 Mbps |
| 10 Streams (passthrough) | <5% | ~60MB | 50-100 Mbps |
| GPU Transcoding | <5% | ~80MB | 100+ Mbps |

### Scalability

- **Horizontal**: Stateless design allows multiple instances
- **Vertical**: Single instance handles 100+ concurrent streams
- **Bottleneck**: Network bandwidth and Enigma2 tuner limits

## Security Architecture

### Attack Surface

1. **HTTP API**: Exposed to network
   - Mitigation: API token auth, rate limiting
2. **OpenWebIF Proxy**: Passes credentials to Enigma2
   - Mitigation: ENV vars, secrets management
3. **Stream URLs**: May expose internal IPs
   - Mitigation: `XG2G_STREAM_BASE` URL rewriting

> [!IMPORTANT]
> **Developer Note**: The `Process` method in `internal/transcoder/rust.go` encapsulates critical safety mechanisms (`runtime.LockOSThread`, buffer limits, error retrieval). **Never** bypass this method or create custom FFI wrappers without replicating these safety guarantees. Direct CGO calls without thread locking will lead to Undefined Behavior (UB) due to Rust's `thread_local` storage.

### Security Measures

- **Non-Root Containers**: User `nonroot:nonroot`
- **Read-Only Filesystem**: Prevents tampering
- **SBOM Generation**: Supply chain transparency
- **Automated Scanning**: govulncheck, Trivy, CodeQL

**Related**: [SECURITY.md](../SECURITY.md)

### Stream Resolution Standards (Core Invariants)

To prevent regression of encrypted channel support, all developers must adhere to these invariants:

1. **NEVER Hardcode Ports 8001 or 17999**:
    - **Rule**: The V3 streaming system MUST NOT guess the stream port.
    - **Reason**: Enigma2 dynamically assigns ports based on decryption needs (8001=FTA, 17999=Encrypted/OSCam). Guessing 8001 for an encrypted channel results in a silent failure (black screen).
    - **Correct Approach**: Always parse the URL returned by the WebAPI.

2. **WebAPI is the Source of Truth**:
    - **Rule**: All stream requests must be resolved via the Enigma2 Web API (`/web/stream.m3u?ref=...`).
    - **Reason**: This API handles the "Zap" command and returns the correct, active stream URL (including the correct port).
    - **Implementation**: Use `ZapAndResolveStream` helper which encapsulates this logic.

3. **Respect Hardware Timing (Zap Delay)**:
    - **Rule**: A delay (currently **5 seconds**) is MANDATORY between Zapping and connecting to the stream port for encrypted channels.
    - **Reason**: `oscam-emu` needs time to initialize the descrambler and open port 17999. Connecting too early causes "Connection Refused".

## References

- [Internals Guide](INTERNALS.md)
- [ADR-001: API Versioning](adr/001-api-versioning.md)
- [ADR-002: Config Precedence](adr/002-config-precedence.md)
- [ADR-003: Validation Package](adr/003-validation-package.md)
- [ADR-004: OpenTelemetry Integration](adr/004-opentelemetry.md)
- [Telemetry & Observability](OBSERVABILITY.md)

---

**Last Updated**: 2025-12-19
**Version**: 2.1.0
**Maintainer**: @ManuGH
