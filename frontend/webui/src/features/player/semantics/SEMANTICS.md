# Playback Semantics

The player exposes five semantic classes and only these five:

- `auth`: the user is unauthorized or forbidden. This blocks playback and is terminal for the current attempt.
- `session`: the playback contract was valid, but the runtime session is gone, expired, or invalid. This blocks playback but is usually recoverable through reconnect/session restart.
- `contract`: the normalized playback contract is invalid, unsupported, or fail-closed. This blocks playback and is not healed by media retry.
- `media`: the media element or HLS runtime failed after a valid contract existed. This may be recoverable or retryable depending on the failure code.
- `advisory`: warning-only context. Advisory never blocks playback, never becomes terminal, and never overrides another failure class.

Telemetry follows the same semantic truth:

- `playback_auth_blocked`
- `playback_session_failed`
- `playback_contract_blocked`
- `playback_media_error`
- `playback_advisory`

Legacy telemetry remains mapped from the same semantic source of truth. No JSX branch or view detail is allowed to emit its own competing interpretation.
