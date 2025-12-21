# xg2g v3.0 Roadmap: Path to 2026

> [!NOTE]
> Strategic plan to replace the synchronous v2 proxy with the v3 asynchronous event-driven architecture.

## Phase 1: Foundation (Q4 2025)

*Goal: Compilable, unit-tested core without side effects.*

- [x] **Core Architecture (PR A)**: `model`, `store`, `bus`, `fsm` contracts.
- [x] **Worker Skeleton (PR B)**: `Orchestrator`, `IntentHandler` (API), `MemoryBus`.
- [ ] **Integration Test**: Proven breakdown of "Intent -> Bus -> Worker -> Store" flow.
- [ ] **Worker Logic**: Implement `Enigma2Client` and `FFmpeg` spawning inside `Orchestrator`.

## Phase 2: Shadow Canary (Q1 2026)

*Goal: Run v3 in production receiving real traffic, but *not* serving users.*

1. **Dual Ingress**: Update v2 Proxy to *also* send Intents to v3 API (fire-and-forget).
2. **Shadow Workers**: v3 Workers acquire leases and "simulate" tuning/transcoding (or actually do it on idle tuners).
3. **Validation**: Compare v2 success rate vs v3 FSM state transitions.
4. **Load Testing**: Verify Bus/Store performance under peak concurrency.

## Phase 3: Production Switch (Q2 2026)

*Goal: v3 becomes the authoritative path.*

1. **Read-Path Switch**: `/stream/` endpoints look up v3 `Store` for HLS playlists.
2. **Write-Path Switch**: Clients talk directly to v3 Control API (bypassing v2 logic).
3. **V2 Deprecation**: Remove `internal/proxy` synchronous code.
4. **Scale Out**: Move from `MemoryBus`/`Badger` to `NATS`/`Redis` (if multi-node needed).

## 2026 Production Criteria

- **Zero-Blocking**: API P99 < 50ms.
- **Crash Recovery**: Worker restart recovers state from Store < 2s.
- **Observability**: Full trace id support from Ingress to Segment Serve.
