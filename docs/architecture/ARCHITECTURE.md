# xg2g System Architecture

> [!IMPORTANT]
> **System Contract**: This document defines the immutable architectural principles of xg2g.
> Changes to these principles require a major version bump and a new RFC.

## 1. Core Principles

### 1.1. The "Universal Policy"

The system has **one** way to deliver video. There are no content negotiation, no user-agent sniffing, and no "profiles".

- **Source of Truth**: [ADR-00X: Delivery Policy](../rfc/ADR-00X-delivery-policy.md)
- **Invariant**: Every stream is **H.264/AAC/fMP4/HLS**.

### 1.2. The Thin Client Rule

The WebUI is a **projection** of the backend state. It contains:

- **No business logic** (no decision making on transcoding).
- **No state persistence** (preferences are ephemeral or server-side).
- **No fallbacks** (if the stream fails, the UI reports the error; it does not retry with different settings).
- **Verification**: [WebUI Thin Client Audit](WEBUI_THIN_CLIENT_AUDIT.md)

### 1.3. Security Fail-Closed

- Auth is mandatory for control planes.
- Invalid configuration prevents startup ("Fail-Start").
- Legacy environment variables trigger a fatal error.

## 2. High-Level Design

```mermaid
graph TD
    Client[WebUI / iOS] -->|HLS (Universal)| CDN[Nginx/Internal]
    Client -->|Intent API| API[Intent Handler]
    
    API -->|Validation| Config[Configuration]
    API -->|Session| Workers[Transcode Workers]
    
    Workers -->|MPEG-TS| Enigma2[Upstream Receiver]
    Workers -->|fMP4 Segments| CDN
```

## 3. Worker Model

- **Ephemeral**: Workers start on `stream.start` intent and die on `stream.stop` or timeout.
- **Isolated**: One worker = One FFmpeg process.
- **Hardware-Aware**: Workers automatically grab VAAPI/NVENC devices if available.

## 4. Configuration as Product

Configuration is not just flags; it is the **API surface** for the operator.
See [Configuration Guide](../guides/CONFIGURATION.md) for the bounded set of allowed keys.
