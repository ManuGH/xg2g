# Strategic Architecture Review (Q1 2026)

## 1. Playback Decoupling: Is FFmpeg irreplaceable?

**No, but the execution model is.**
FFmpeg is the industry standard for transcoding, but our *tight coupling* to `exec.Command("ffmpeg", ...)` is a liability. It prevents us from running transcoding on:

* Remote worker fleets (Kubernetes Jobs).
* Hardware appliances (dedicated transcoders).
* Mock runners for high-speed value stream testing.

**Architecture Fix (Step 4 Preview)**:

* **Port**: `MediaPipeline` interface (Start, Stop, Health, Progress).
* **Adapter**: `LocalFFmpegAdapter` (current implementation).
* **Benefit**: The Orchestrator strictly manages *lifecycle* (when to start/stop), implying the *how* is an implementation detail.

## 2. Session Invariants: The "Crown Jewels"

If these break, the product is broken. They must hold under concurrency and attack.

1. **Single Writer Principle**: Only the `SessionManager` (via FSM) transitions state.
    * *Risk*: API handlers bypassing FSM to write to DB directly.
2. **Resource Lease Integrity**: A hardware tuner (Slot 0..N) is never double-booked.
    * *Risk*: Race condition between `TryAcquireLease` and `StartSession`.
3. **Zombie Cleanup**: If a session is "Dead", its resources (FFmpeg PID) MUST be released within `Sweeper.IdleTimeout`.
    * *Risk*: Panics in the Orchestrator loop leaving orphan processes.

## 3. Security Red-Team: Where do we break in 24h?

**Attack Vector 1: Command Injection via Config**

* We use `exec.CommandContext(ctx, binary, args...)`.
* If `AppConfig.FFmpeg.Bin` or `AppConfig.Enigma2.BaseURL` can be manipulated (e.g., via a compromised config reload endpoint), an attacker gains RCE.
* **Fix**: Whitelist allowed binaries and strict URL validation in `config` domain.

**Attack Vector 2: Path Traversal in HLS**

* Recent fixes (G304) addressed `os.ReadFile`.
* *Remaining Risk*: Symlink attacks in the HLS segments directory (`/tmp/hls/...`). If we write to a symlinked directory created by an attacker, we might overwrite system files.

## 4. "Kill-the-App": Simplification Opportunities

To gain velocity, we should delete:

1. **Legacy Config Options**: `config.go` is ~1100 lines. Features like "Shadow Mode", multiple "Delivery Policies", and "Legacy Auth" add testing permutations that slow us down.
2. **The "Shadow" Pipeline**: If V3 is canonical, stop running V2 components in parallel. It doubles the memory footprint and log noise.
3. **Direct DB Implementations**: We have `BoltStore`, `BadgerStore`, `MemoryStore`. Supporting 3 backends for a low-volume session DB is over-engineering. Pick one (Bolt or SQLite) + Memory for tests.

---
**Recommendation**: Proceed with **Option 1 (Playback Decoupling)** to pay down the Whitelist debt, but perform a **Config Pruning** spike in parallel.
