# Strategic Mandate (Q1 2026) - The "Best Team" Protocol

> [!IMPORTANT]
> **Executive Order**: This document supersedes previous tactical plans.
> **Philosophy**: Reliability > Scalability > Features.
> **Exit Criteria for Q1**: `verify_deps` Whitelist = 0.

## 1. The 3 Customer Guarantees (Measurable & Testable)

To be the best, we guarantee these three things under chaos:

### Guarantee 1: Strict Reservation Integrity (The "Recording Promise")

* **Promise**: "If we accepted your Recording, it *happens*, regardless of Live TV load."
* **Technical Proof**: Tuner Leases are logically partitioned. Live TV requests receive `409 Conflict` instantly if they would cannibalize a Recording slot.
* **Test**: `LeaseIntegrationTest`: Flood Live TV requests while maxing recordings. 0% interruption allowed.

### Guarantee 2: Self-Healing Liveness (The "No Hang" Promise)

* **Promise**: "The system never stays broken. If a stream dies, we restart it or fail explicitely within 10s."
* **Technical Proof**: The Orchestrator `Heartbeat` and `Sweeper` loops run independently of API traffic. Dead PIDs are reaped. Stalled streams are retried or transitioned to `Error`.
* **Test**: `ZombieKillTest`: `kill -9` the ffmpeg process. System must detect and transition state < 10s.

### Guarantee 3: State Consistency (The "Single Truth" Promise)

* **Promise**: "The system view *is* the reality. No 'Phantom' sessions."
* **Technical Proof**: The FSM is the Single Writer. API handlers **never** write to the Store directly.
* **Test**: `SingleWriterTest`: Attempt concurrent writes. Verify CAS (Compare-And-Swap) or FSM serialization prevents divergence.

---

## 2. Step 4: Playback Decoupling (Strict Mode)

**Objective**: Remove `internal/domain` dependence on `internal/pipeline/exec`.
**Constraint**: `ALLOWED_VIOLATIONS` count must hit **0**.

### The Rules

1. **Port First**: The Interface (`MediaPipeline`) is defined in Domain *before* any implementation code.
2. **Abstraction Gravity**: The Session Domain must not know "FFmpeg" exists.
    * No `ffmpeg_bin` paths.
    * No `-c:v copy` args in Domain objects.
    * Domain Object: `StreamSpec` (e.g., `Resolution: High`, `Format: HLS`).
3. **Adapter Isolation**: Code in `internal/infrastructure/media/ffmpeg` translates `StreamSpec` -> `[]string{"-i", ...}`.

---

## 3. "Crown Jewels" Verification (Pre-Feature Gate)

Before merging Step 4 features:

* [ ] **One-Way-Write Rule**: CI check preventing Store writes from `api/v3`.
* [ ] **Lease Panics**: Unit tests verifying `TryAcquire` panics on double-booking.
* [ ] **Zombie Killer**: Integration test spawning a "rogue" process and asserting cleanup.

---

## 4. Security Red Team (Immediate Action)

* **Config Lockdown**: Validate `Bin` paths and `URLs` on startup. Reject unsafe values.
* **Exploit Sim**: Create a test case that attempts to load a config pointing to `/bin/rm` as FFmpeg.

## 5. Pruning (Kill-List)

1. **Shadow Pipeline**: Remove `APIServerSetter` injection of Legacy components into V3 context.
2. **Legacy Config**: Deprecate "Shadow" mode options.
