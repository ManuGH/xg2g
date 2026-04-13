# Playback Orchestrator Transitions

PR 1 keeps the existing feature behavior, but moves normative state transitions into a single reducer.

| Current state | Event | Guard | Next state | Side-effect driver |
| --- | --- | --- | --- | --- |
| any | `normative.playback.attempt.started` | `event.epoch >= state.epoch.playback` | reset contract/failure, set `playbackMode`, `status`, `epoch.playback` | none |
| current playback | `normative.playback.contract.resolved` | `event.epoch === state.epoch.playback` | store normalized contract truth | none |
| current playback | `normative.session.attempt.started` | `event.playbackEpoch === state.epoch.playback` | bump `epoch.session`, set `sessionPhase=starting` | session fetch/poll happens outside the reducer |
| current session | `normative.session.phase.changed` | `event.playbackEpoch === state.epoch.playback && event.sessionEpoch === state.epoch.session` | update `sessionPhase` and `traceId` | none |
| current playback | `normative.media.status.changed` | `event.epoch === state.epoch.playback` | update `status` and derived `mediaPhase` | media element reacts outside the reducer |
| current playback | `normative.playback.failure.raised` | `event.epoch === state.epoch.playback` | store structured failure truth | retry / teardown remains outside the reducer |
| any | `normative.playback.stopped` | `event.epoch >= state.epoch.playback` | set terminal stopped state, clear current contract | teardown happens before/around this transition |

Rule of thumb:

- Normative inputs may change reducer state.
- Advisory inputs may annotate state, but they must not become policy.
- Stale playback/session epochs are ignored rather than merged.
