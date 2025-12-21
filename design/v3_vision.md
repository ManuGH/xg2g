# xg2g v3.0 Architecture Vision

> [!NOTE]
> Derived from **PR #109** (v3 skeleton).
> Implements "Zero-blocking ingress", "State Store as Source of Truth", and "Async Worker" pattern.

## 1. Core Principles

1. **Synchronous Ingress Death**: API requests (Play, Stop) only interact with the `State Store` (Intent/Ticket). They *never* block on receivers.
2. **State-Driven**: Components poll or subscribe to state changes. `Preflight` becomes a simple DB lookup.
3. **Explicit Lifecycles**: Strict FSMs for Sessions (User view) and Pipelines (Worker view).

## 2. Session FSM (User/API View)

*Coarse-grained state visible to clients. Manages permissions, TTL, and logical availability.*

```mermaid
stateDiagram-v2
    direction LR
    [*] --> NEW: API Intent Accepted
    NEW --> STARTING: Lease Acquired
    STARTING --> READY: Pipeline Ready
    STARTING --> FAILED: Pipeline Failed / Timeout
    
    READY --> DRAINING: API Stop
    STARTING --> CANCELLED: API Cancel
    
    DRAINING --> STOPPING: Grace Expired
    STOPPING --> EXPIRED: Worker Stopped
    
    FAILED --> [*]
    EXPIRED --> [*]
    CANCELLED --> [*]

    state STARTING {
        [*] --> Acquired
        Acquired --> Tuning
        Tuning --> Verifying
    }
```

## 3. Pipeline FSM (Worker/Internal View)

*Fine-grained state managing the "Metal" (Enigma2, FFmpeg, DVR).*

```mermaid
stateDiagram-v2
    direction TB
    [*] --> INIT: Create
    INIT --> LEASED: Lease Lock OK
    INIT --> FAIL: Lease Denied/Busy
    
    LEASED --> TUNE_REQUESTED: Zap Receiver
    TUNE_REQUESTED --> TUNE_VERIFYING: Polling Signals
    TUNE_VERIFYING --> FAIL: Timeout
    
    TUNE_VERIFYING --> FFMPEG_STARTING: Signal Locked
    FFMPEG_STARTING --> PACKAGER_READY: Process Up & Probing
    FFMPEG_STARTING --> FAIL: Exit Code > 0
    
    PACKAGER_READY --> SERVING: Segments Flowing
    
    SERVING --> STOPPING: Stop Requested
    STOPPING --> STOPPED: Cleanup
    STOPPED --> [*]
    
    FAIL --> [*]
```

## 4. Logical Components

```mermaid
graph TD
    Client(HLS Client) --> Origin(Origin Proxy)
    API(Control API)
    
    subgraph "State Layer (Consistent)"
        Store[(BadgerDB / Redis)]
        Bus((Event Bus))
    end
    
    subgraph "Worker Layer (Async)"
        Orch(Orchestrator)
        Receiver(Enigma2 Receiver)
        FFmpeg(FFmpeg Transcoder)
    end
    
    API -- "Put Intent" --> Store
    API -- "Publish Intent" --> Bus
    
    Origin -- "Read State" --> Store
    Origin -- "Serve Placeholder/Segments" --> Client
    
    Bus -- "Subscribe Intents" --> Orch
    
    Orch -- "Acquire Lease" --> Store
    Orch -- "Zap/Poll" --> Receiver
    Orch -- "Spawn/Monitor" --> FFmpeg
    
    FFmpeg -- "Write HLS" --> Origin
    Orch -- "Update Pipeline State" --> Store
```

## 5. Migration Strategy

1. **Skeleton Merge** (Done via PR #109 logic).
2. **Shadow Mode**: Run v3 Workers alongside v2 Proxy. v2 Proxy acts as "Client" to v3 Store.
3. **Origin Switch**: Switch `/stream/` to read from v3 Store.
4. **Deprecation**: Remove v2 synchronous paths.
