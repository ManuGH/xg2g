## 9. Production Integration: Enigma2 Streaming Topology

**Author:** xg2g maintainers
**Date:** 2026-01-18
**Status:** Runtime reference

### Executive Summary

This section documents the Enigma2 streaming chain used by xg2g in DVB-S2 and
FBC receiver environments. The important rule is simple: resolve the effective
stream URL through OpenWebIF first, then fall back to a direct stream port only
when receiver-side resolution fails.

**Key Insight:** xg2g does not emulate IPTV. It models the actual DVB-S2 to
Enigma2 to HTTP streaming pipeline.

---

### The Complete Streaming Chain

```
┌─────────────────────────────────────────────────────────────────────┐
│                   Production Topology (Verified)                     │
└─────────────────────────────────────────────────────────────────────┘

DVB-S2 Satellite (e.g., Orbital Position 19.2°E)
        │
        ├─ Encrypted MPEG-TS (Provider-specific CAIDs)
        │
        ▼
┌─────────────────┐
│ Enigma2 Receiver│
│ (Various Models)│
└────────┬────────┘
         │
         ├─ Tuner Allocation (8 FBC Tuner Pools)
         ├─ Demux (Transport Stream → Elementary Streams)
         │
         ▼
┌─────────────────┐
│ Optional        │  Port <relay-port> (Stream Processing)
│ Middleware      │
└────────┬────────┘
         │
         ├─ Stream Processing Layer
         ├─ Proxies request to Enigma2 Port 8001
         │
         ▼
┌─────────────────┐
│  Enigma2 HTTP   │  Port 8001 (Native Stream Port)
│  Streaming API  │
└────────┬────────┘
         │
         ├─ /web/stream.m3u?ref={serviceref}
         ├─ Allocates Independent FBC Tuner
         ├─ No zapping (main display unaffected)
         │
         ▼
┌─────────────────┐
│  Client (VLC,   │  Port 80/443 (M3U Playlist)
│   xg2g, etc.)   │
└─────────────────┘
```

---

### Port Mapping & Responsibilities

| Port       | Component          | Responsibility                           | Protocol      |
|-------|------------------|----------------------------------------------|---------------|
| **80/443** | OpenWebIF    | Web UI, API endpoints, M3U generation        | HTTP(S)       |
| **8001**   | Enigma2 Stream | **Native streaming port / direct fallback** | HTTP (MPEG-TS)|
| **<relay-port>**  | Optional Middleware | Stream processing proxy (optional)        | HTTP (MPEG-TS) |

**Compatibility default:** `enigma2.streamPort = 8001` remains in the registry
as a deprecated direct fallback. Live playback still queries
`/web/stream.m3u` first so the receiver can return `8001`, `<relay-port>`, or another
effective stream endpoint.

---

### Service Reference Anatomy

Enigma2 uses a colon-delimited reference to uniquely identify DVB services:

```
1:0:19:81:6:85:C00000:0:0:0:
```

**Field Breakdown:**

| Field     | Hex    | Decimal  | Meaning                          |
|-----------|--------|----------|----------------------------------|
| Type      | `1`    | 1        | TV Service                       |
| Flags     | `0`    | 0        | Standard                         |
| Service Type | `19` | 25       | MPEG-4 HD (H.264)                |
| **SID**   | `81`   | **129**  | **Service ID** (unique per mux)  |
| **TSID**  | `6`    | 6        | Transport Stream ID              |
| **ONID**  | `85`   | **133**  | Original Network ID (Provider X) |
| Namespace | `C00000` | 12582912 | Generic Satellite (orbital position) |

**Example:**  
`http://192.0.2.10/web/stream.m3u?ref=1:0:19:EF:1A:85:C00000:0:0:0:`

→ OpenWebIF generates an M3U playlist:

```m3u
#EXTM3U
#EXTINF:-1,Example HD Channel
#EXTVLCOPT:program=239
http://127.0.0.1:<relay-port>/1:0:19:EF:1A:85:C00000:0:0:0:
```

---

### Multi-Stream Behavior (FBC Tuner Architecture)

**Key Invariant:** Each stream request allocates an **independent FBC tuner**.

```
User Action                  Tuner Allocation              Main Display
─────────────────────────────────────────────────────────────────────────
Watching TV (Port 80)        Tuner #1 (active)             Example HD
VLC opens stream.m3u         Tuner #2 (allocated)          (unchanged)
xg2g requests 2nd stream     Tuner #3 (allocated)          (unchanged)
```

**Why `/web/stream.m3u` Doesn't Zap:**

- Allocates a **background tuner** (no UI interaction)
- Main display continues on current channel
- Up to **8 concurrent streams** (FBC tuner pool)

**Contrast with `/web/zap`:**

- Changes the **main display** to target channel
- Uses the display's tuner (no parallel allocation)

---

### Optional Stream Middleware (Port <relay-port>)

**How Optional Middleware Works:**

1. **Client** requests: `http://192.0.2.10:<relay-port>/{serviceref}`
2. **Middleware** receives stream from **Enigma2:8001**
3. **Middleware** processes stream as configured
4. **Middleware** returns processed stream to client

**xg2g Integration:**  
xg2g asks OpenWebIF for the effective stream URL first. If that fails, the
deprecated `enigma2.streamPort` value is used to construct a direct fallback URL.

---

### FBC Tuner Scheduling (8-Pool Architecture)

Modern Enigma2 receivers with FBC support use **FBC (Flexible Band Concatenation)**:

**Tuner Pool Behavior:**

- **8 virtual tuners** per physical tuner module.
- Shared within the **same transponder frequency**.
- Independent allocation for **different transponders**.

#### Real-World Example (Verified 2026-01-22)

- **TV Output**: Watching ORF1 HD (Transponder `0x03EF`, 11302.750 MHz) on **Tuner A**.
- **Laptop Stream**: Streaming SAT.1 HD (Transponder `0x03F9`, 11464.250 MHz) on **Tuner C**.
- **Outcome**: The receiver automatically tuners to the second transponder using a free FBC tuner. **No zapping/switching occurs** because the FBC pool allows up to 8 independent transponders.

### The Stream Request Flow (Middleware Layer)

In setups with optional relay middleware, the flow is as follows:

1. **Client** → OpenWebif (Port 80) `/web/stream.m3u?ref=...`
2. **OpenWebif** → Determines if the stream requires relaying natively. OpenWebIF is purely the **API/URL-Generator**.
3. **M3U Response** → Returns a Relay-URL like `http://IP:<relay-port>/{serviceref}`.
4. **Client** → Port <relay-port> (relay endpoint provided by middleware).
5. **Middleware** → Fetches raw stream from `localhost:8001` (Enigma2 Native).
6. **Enigma2:8001** → Delivers MPEG-TS via Tuner.
7. **Middleware** → Processes the TS and returns it to the Client with `HTTP/1.0 200 OK` and `Server: stream_enigma2`.

**XG2G Integration:** Live playback follows this chain automatically through
`ResolveStreamURL`. `useWebIFStreams: true` forces the same receiver-resolved URL
style on OpenWebIF client surfaces that would otherwise build direct channel
URLs.

#### relay middleware Technical Constraints

When querying streams directly from relay middleware (`<relay-port>`), several strict networking constraints apply:

- **HTTP Methods:** It strictly requires `GET`. `HEAD` tests on these TS-Endpoints will often return "empty reply" or close the connection, even though `GET` streams perfectly.
- **Preemptive Authentication:** If Enigma2 is secured, some relay middleware may drop the connection instead of sending a `401 Unauthorized` challenge. Clients (like `ffprobe`) must send the `Authorization: Basic ...` header *preemptively*.
- **User-Agent Filtering:** The middleware drops connections instantly (0 bytes read) if the `User-Agent` is blank or non-standard. Spoofing standard player signatures (e.g., `VLC/3.0.21 LibVLC/3.0.21`) is required for probe tools mapping the stream.
- **Probe Timeouts:** Tuning and preparing the relay stream takes significant time (3-5 seconds). Probe timeouts must be unusually generous (e.g., `8s`), otherwise the receiver kills the pipe before the first TS packet arrives.
- **Rate Limiting:** Aggressive channel scanning (e.g., full EPG probing) on port `<relay-port>` can overload the relay process, resulting in `Connection refused` for subsequent streams. A delay of at least `1000ms` between consecutive stream requests is necessary to maintain stability.

---

### Configuration Truth: Why `StreamPort=8001`

**Historical Context:**

Before optional middleware:

```
Client → Enigma2:8001 (native stream)
```

With optional middleware:

```
Client → Middleware:<relay-port> → Enigma2:8001 (proxy)
```

**xg2g compatibility default:**

```go
// internal/config/registry.go
{Path: "enigma2.streamPort", Default: 8001, Status: StatusDeprecated}
```

**Why 8001 still exists:**

1. Optional middleware typically calls Enigma2:8001 in the background.
2. Direct streaming uses 8001 as the native port.
3. Keeping the default avoids breaking existing direct-port setups.

**Deprecation Note:**  
`StreamPort` is deprecated because:

- Modern live playback uses **receiver-side stream resolution** through OpenWebIF.
- Optional middleware abstracts the underlying port.
- Hardcoding 8001 in operator config hides whether the receiver actually
  selected a relay endpoint such as `<relay-port>`.

---

### Validation Proof: Production Test

**Setup:**

- Enigma2 receiver with FBC support (8 virtual tuners)
- Optional middleware enabled (if required)
- DVB-S2 provider subscription

**Test:**

```bash
# Open stream in VLC
vlc http://192.0.2.10/web/stream.m3u?ref=1:0:19:EF:1A:85:C00000:0:0:0:
```

**Expected Result:**
- Stream opens immediately.
- Main display continues on the current channel.
- Tuner allocation consumes one free FBC tuner.

**xg2g Implementation:**  
Calls `/web/stream.m3u?ref=...`, follows the receiver-returned URL, and only
falls back to `http://192.0.2.10:8001/{serviceref}` if OpenWebIF resolution
fails.

---

### Architectural Alignment: xg2g's Role

**What xg2g Does:**

- Calls Enigma2 OpenWebIF API (Port 80) for metadata (EPG, services, timers)
- Resolves live stream URLs through OpenWebIF, then falls back to the deprecated
  direct stream port only if resolution fails
- Converts MPEG-TS → HLS for Safari/iOS compatibility
- Manages session lifecycle (FFmpeg → HLS segmenter)

**What xg2g Does NOT Do:**

- Process encrypted streams (delegates to middleware if configured)
- Manage DVB tuners directly (delegates to Enigma2)
- Implement DVR scheduling (delegates to Enigma2 timers)

**Role Summary:**  
xg2g is an **orchestration layer**, not a replacement for Enigma2 or optional middleware.

---

### Key Takeaway

> **xg2g models reality, not abstractions.**

The streaming chain documented here is:

- **Field-proven** in DVB-S2 production deployments.
- **Mechanically enforced** by config registry defaults and stream-resolution tests.
- **Testable** through OpenWebIF-first and direct-fallback test cases.

**Consequence:**  
Do not remove the 8001 fallback or bypass FBC tuner allocation, but do not
force 8001 ahead of receiver-resolved stream URLs.

---

**References:**

- [Enigma2 OpenWebif API Wiki](https://github.com/E2OpenPlugins/e2openplugin-OpenWebif/wiki/OpenWebif-API-documentation)
- [Enigma2 Community Forums](https://dreambox.de/board/)

---
