# Walkthrough: Step 8d – Protocol v6 & Canonical Timing Publication

Step 8d has been successfully implemented on branch `feat/timing-authority-8d` (PR [#1027](https://github.com/ManuGH/xg2g/pull/1027)) cut directly from `origin/main` (`da2ba177ff4010663b349918e042c77a6db7dd4b`).

---

## 1. Architectural Changes

### 1.1 Wire Protocol Bump to v6 (`media-core/src/ipc.rs` & `backend/internal/stream/ingest/remotecore/wire.go`)
- Bumped protocol version from 5 to 6 (`VERSION = 6` in Rust, `Version = 6` in Go).
- Fail-closed contract: Any handshake or frame with a version other than 6 is rejected with a fatal error; no legacy fallback shims.

### 1.2 Extensible Sectioned Result Envelope
Replaced the monolithic result body with a forward-compatible 4-section wire envelope:
```text
┌─────────────────────────────────────────────────────────────┐
│ ResultEnvelope Header (12 bytes)                           │
│ - status: u8 (STATUS_OK = 0)                                │
│ - coverage: u8 (wireCoverageComplete = 2 / ParseCoverageComplete) │
│ - processed_through: i64                                    │
│ - section_count: u16 (4)                                    │
├─────────────────────────────────────────────────────────────┤
│ Section 1: EVENTS (ID = 1, flags = CRITICAL)                │
│ - 32-bit count + encoded VideoEvent items                   │
├─────────────────────────────────────────────────────────────┤
│ Section 2: FACTS (ID = 2, flags = CRITICAL)                 │
│ - PSI facts, Video facts, Audio scrambling & observations  │
├─────────────────────────────────────────────────────────────┤
│ Section 3: ACTIVE_PSI (ID = 3, flags = CRITICAL)            │
│ - PAT raw sections + PMT raw sections                       │
├─────────────────────────────────────────────────────────────┤
│ Section 4: TIMING (ID = 4, flags = CRITICAL)                │
│ - has_epoch: u8 (0 or 1)                                    │
│ - active_epoch: u64 (0 if has_epoch == 0)                   │
│ - record_count: u32                                         │
│ - [TimingRecord items...]                                   │
└─────────────────────────────────────────────────────────────┘
```

- **Forward Compatibility & Fail-Closed Validation:**
  - Non-critical unknown sections (`flags & 1 == 0`) are safely skipped using their 32-bit section lengths.
  - Critical unknown sections (`flags & 1 != 0`) fail closed immediately.
  - Known sections (`EVENTS`, `FACTS`, `ACTIVE_PSI`, `TIMING`) must have `secFlags == SectionFlagCritical` (0x0001) exactly; flags `0x0000`, `0x0002`, `0x0003`, `0x8001` are rejected fail-closed.
  - PID bounds validation: PES PID <= 0x1FFF, PCR PID <= 0x1FFF, Track scope `1 <= track_pid <= 0x1FFE` (null packet 0x1FFF and zero 0 are rejected), Program scope `track_pid == 0`.
  - Duplicate or missing critical sections are rejected.
  - Zero-rules: Unset/absent fields MUST be strictly zero on wire (non-zero is rejected).
  - Signed offsets: Byte coordinates (`processed_through`, `observed_at`, `subject_at`) are validated non-negative.

### 1.3 Canonical Timing Publication
Encoded `TimingRecord` variants into Section 4:
1. **PES Timing Point (Record Type 1, 44 bytes):**
   - `epoch: u64`
   - `pid: u16`
   - `flags: u8` (`HAS_PTS = 1`, `HAS_DTS = 2`)
   - `observed_at: i64`
   - `subject_at: i64`
   - `pts_90k: i64` (signed 90 kHz extended timestamp; 0 if absent)
   - `dts_90k: i64` (signed 90 kHz extended timestamp; 0 if absent)
2. **PCR Point (Record Type 2, 27 bytes):**
   - `epoch: u64`
   - `pid: u16`
   - `observed_at: i64`
   - `pcr_27m: i64` (signed 27 MHz extended timestamp)
3. **Discontinuity Record (Record Type 3, 30 bytes):**
   - `scope_type: u8` (`PROGRAM = 1`, `TRACK = 2`)
   - `scope_pid: u16` (0 for program scope)
   - `reason: u8` (`PROGRAM_IDENTITY_CHANGED = 1`, `PCR_PID_CHANGED = 2`, `PCR_DISCONTINUITY_INDICATOR = 3`, `TRANSPORT_TIMING_LOSS = 4`)
   - `observed_at: i64`
   - `flags: u8` (`HAS_EPOCH_BEFORE = 1`, `HAS_EPOCH_AFTER = 2`)
   - `epoch_before: u64` (0 if absent)
   - `epoch_after: u64` (0 if absent)

### 1.4 Go Domain Model & Timing Authority (`mediafacts` & `remotecore`)
- Introduced `TimingAuthority` enum:
  - `TimingAuthorityUnknown = 0`
  - `TimingAuthorityNone = 1`
  - `TimingAuthorityCanonical = 2`
- `GoCore` retains `ParseCoverageComplete` (avoiding any breaking redesign of `MasterRing` or downstream variants) but explicitly sets `Timing.Authority = TimingAuthorityNone`.
- `RemoteCore v6` decodes Section 4 into `TimingResult` and marks `Authority = TimingAuthorityCanonical`.
- Go stores and exposes the canonical timeline without altering, unrolling, or recalculating any timestamps.

### 1.5 Shared Authored Timing Corpus (`testdata/timing-corpus/corpus.txt`)
- Created comprehensive shared corpus test fixture with 4 multi-step test cases covering all 18 specified scenarios:
  1. `canonical_timeline_lifecycle`: PAT before PMT, first PMT Epoch 0, PCR-first alignment, PTS+DTS & RAP binding, PTS only, negative/B-frame PTS, RAP invalidation via TEI, DI without PCR -> Epoch 1, next clean PCR, DI+PCR same packet -> Epoch 2, DTS-first alignment, video track CC jump reset, PMT version increment -> Epoch 3, audio track CC jump reset.
  2. `pts_wrap_33bit`: 33-bit PTS modular rollover ($2^{33}-500 \to 500$).
  3. `pcr_wrap_27mhz`: 27 MHz PCR modular rollover ($(2^{33}\times 300)-3000 \to 3000$).
  4. `re_anchor_beyond_half_modulus`: Re-anchor after long runtime ($5\times 10^9$ ticks > $M/2$) without backwards jump.
- Verified identically against:
  - Rust `VideoIngress` (`media-core/src/timing/corpus_test.rs`)
  - Go live IPC over Unix domain socket with `xg2g-media-core` binary (`backend/internal/stream/ingest/remotecore/timing_corpus_test.go`)

---

## 2. Verification & Test Evidence

### 2.1 Rust Automated Tests
Ran `cargo test --locked --manifest-path media-core/Cargo.toml`:
```text
test result: ok. 211 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 5.37s (lib)
test result: ok. 19 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.19s (main)
test result: ok. 2 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s (audio_hardware)
Total: 232 passed; 0 failed
```

### 2.2 Go Automated Tests & Real-Core Differential Tests
Ran tests with live core flags enabled (`go test -v -count=1 ./backend/internal/stream/ingest/...`):
- `remotecore`: Golden envelope tests (`goldenEmptyResult` [187B], `goldenFullResult` [352B]) match byte-for-byte between Rust and Go.
- `remotecore`: Adversarial wire tests (missing sections, duplicate sections, unknown critical/non-critical flags, known section flag validation `0x0000`/`0x0002`/`0x0003`/`0x8001`, invalid/null/zero PIDs, non-canonical zero violations, negative offsets) all pass.
- `remotecore/timing_corpus_test.go`: All 18 scenarios across 4 test cases pass identically against the real `xg2g-media-core` binary over UDS.
- `timing_differential_test.go`:
  - Starts live `xg2g-media-core` binary over Unix domain socket IPC.
  - Sets target program -> verifies active epoch initialized to 0.
  - Ingests clean video packet -> verifies PES timing record with 90 kHz unwrapped PTS/DTS.
  - Ingests PCR DI without PCR -> verifies `Discontinuity` record and active epoch advancing from 0 to 1.
  - Ingests subsequent PCR sample -> verifies PCR record in epoch 1 with 27 MHz unwrapped timestamp without second discontinuity.
- All differential tests (`TestVideoDifferential_TheRealRustCoreAgreesCallByCall`, `TestAudioDifferential_TheRealRustCoreAgreesCallByCall`, `TestPSIProcess_TheRoundTripCostOfAlignedChunks`) pass.
- Race detector passed: `go test -race -count=1 ./backend/internal/stream/ingest/remotecore/...` (**PASSED**).

### 2.3 Formatting, Clippy, Gosec, and Pre-Push Gates
- `cargo fmt --check --manifest-path media-core/Cargo.toml`: **PASSED**
- `cargo clippy --all-targets --locked --manifest-path media-core/Cargo.toml -- -D warnings`: **PASSED**
- Gosec G115 fixed: `audiobatch.go` byte-by-byte sign extension avoids integer overflow conversion.
- Staticcheck ST1022 fixed: exported comments formatted canonically.
- `make pre-push`: **PASSED**

### 2.4 Pull Request & CI Status
- **Branch:** `feat/timing-authority-8d` cut directly from `origin/main` (`da2ba177ff4010663b349918e042c77a6db7dd4b`)
- **PR:** [#1027](https://github.com/ManuGH/xg2g/pull/1027)
- **Scope check:** Zero iOS files touched. Confined strictly to:
  * `media-core/**`
  * `backend/internal/stream/ingest/**`
  * `testdata/timing-corpus/**`
  Step 8d is ready for merge.
