SESSION LIFECYCLE (CANCEL/STOP SEMANTICS)

Purpose
- Make cancellation semantics explicit and stable for Control/API/WebUI/SRE.
- Prevent regressions where context cancellation is misclassified as pipeline failure.

Truth Table (Contract)
1) Plain context cancel (no explicit stop intent)
   - State: CANCELLED
   - Reason: R_CANCELLED
   - Detail: "context canceled"
   - Error class: ErrSessionCanceled

2) Explicit stop intent (client/system stop request)
   - State: STOPPED
   - Reason: R_CLIENT_STOP
   - Detail: (empty)
   - Error class: ErrSessionCanceled
   - Invariant: stop intent wins even if cancel/timeout happens in parallel.

3) Tune timeout (deadline exceeded during start)
   - State: FAILED
   - Reason: R_TUNE_TIMEOUT
   - Detail: "deadline exceeded"
   - Error class: ErrPipelineFailure

Invariants
- CANCELLED is a terminal state.
- STOPPED is a terminal state and always represents explicit stop intent.
- R_CANCELLED must never be mapped to pipeline failure.
- R_CLIENT_STOP must never carry cancel details (avoid observability lies).
- ReasonDetail is part of the contract when listed above.

Client/UI/CLI expectations
- Treat CANCELLED as a terminal state (not retryable by default).
- Display STOPPED as user/system intent; CANCELLED as system abort (not pipeline fault).

Do Not
- Do not map context.Canceled to pipeline failure.
- Do not map explicit stop to CANCELLED.
