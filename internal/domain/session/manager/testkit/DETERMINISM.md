Deterministic Test Contract

- No Sleeps / No Eventually
- No wall-clock oracles
- Barriers define phases
- Oracles only: State + Reason + Error class + explicit side-effects (leases/active/stopcount)

Cancellation Truth Table

- ctx canceled (no stop intent) -> SessionCancelled + R_CANCELLED (+ detail "context canceled")
- explicit stop -> STOPPED + R_CLIENT_STOP (wins even if cancel/timeout happens)
- deadline exceeded -> FAILED + R_TUNE_TIMEOUT (+ detail "deadline exceeded")
