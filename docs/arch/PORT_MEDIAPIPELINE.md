# Port: MediaPipeline

## Purpose

Abstracs the mechanism of transcoding and streaming from the Session Lifecycle.
The Session Domain orchestrates **when** media flows; this Port handles **how**.

## Contract

1. **Start**: Guaranteed to return a `RunHandle` or error. The implementation handles all resource allocation (Process, Tuner Lock).
2. **Stop**: Guaranteed idempotent. Stopping a stopped handle is a no-op. Must clean up all resources (PIDs, Files) within a timeout.
3. **Health**: Provides an instantaneous view of "Liveness". Does not guarantee "Quality" (e.g., glitches), only that the process is alive and making progress.

## Invariants (Domain Level)

* **Leakage**: The Domain never sees a PID, CLI argument, or exit code.
* **Safety**: The Domain assumes a healthy `Start` return means resources are committed.
