# Safari AirPlay correction contract

- Fixed: opening or cancelling the picker must not restart playback; recording media needs its own read-only ticket; recording target restarts carry the absolute playhead; seamless handoff requires both H.264 and AAC; ticket failures must be visible.
- Improved: Safari with the WebKit route picker negotiates native H.264/AAC from startup. This avoids offering an MSE blob to a URL-only receiver and keeps the live DVR session and position during normal handoff.
- New: authenticated recording playback-info responses for native playback contain an opaque recording-scoped media ticket. Playlist rewriting propagates it to local recording artifacts. No new API endpoint or schema.
- Removed: eager playback restart on picker invocation and silent native ticket failure.
- Unchanged: non-WebKit capability negotiation, household recording access, recording target/variant validation, live session ticket scope, production runtime, and the user's original worktree.
- Risks: Safari local playback now selects native H.264/AAC instead of HEVC/MSE. Recording tickets expire after four hours; browser suspension and real receiver routing require hardware validation. A new incompatible live session cannot reconstruct earlier DVR history; normal WebKit startup avoids that transition.
- Acceptance: negative controls fail on the original AirPlay source; targeted tests cover picker cancel/failure, live/recording handoff, AAC, absolute recording position, ticket failure, cookie-free recording playlist/init/segment access, cross-resource/API denial, and expiry. Build, player tests, Go race/vet, generated-config verification, and clean committed `make ci-pr` pass before integration.
- Exit condition: no temporary migration path. Manuel runs the hardware checklist before declaring hardware validation or production readiness.

## Lifecycle matrix

| Case | Coverage |
| --- | --- |
| Mount, including an already wireless element | Chrome route-state synchronization; native WebKit negotiation |
| Rerender / callback or executor replacement | Latest callback receives events without duplicate subscriptions; start command retains recording position |
| Unmount / in-flight ticket | Listeners removed; late ticket cannot attach media |
| StrictMode off/on | Chrome and orchestrator handoff cases parameterized |
| Parent/child effects | Existing controller layout/passive-effect matrix retained; no executor lifecycle changes |
| Target local → wireless → local → wireless | Compatible session retained; return to local does not force a restart |

## Hardware checklist (not executed by automated tests)

Safari on iPhone/iPad and macOS: start live and recording playback; cancel picker;
select a receiver; return to local; select again; test a recording with a nonzero
anchor and live DVR; lock the phone for longer than the lease TTL; check picture,
audio, playlist traffic and session identity. Test unavailable receivers and expiry.

## Implementation evidence

- Browser regression suite exercises StrictMode off/on, picker failure/cancel,
  native live and recording handoff, direct MP4 handoff, AAC conversion with
  a nonzero recording anchor, missing tickets, and late answers after unmount.
- Re-running that suite with the original AirPlay orchestrator is a negative
  control; disabling recording ticket authentication fails the cookie-free
  playlist authentication case. Logs are kept outside the public repository.
- Cookie-free recording handler tests fetch both in-memory and file-backed
  playlists, init files and segments. GET/HEAD representation lengths agree.
  Credentials are never propagated to another recording or host. Recording
  API routes and non-read HTTP methods reject media tickets.
- Existing controller executor and parent/child-effect lifecycle tests remain
  applicable; the command now passes the recording playhead through to startup.
- No hardware run or deployment is implied by these automated results.
- The isolated branch imports only the original AirPlay source, not the
  unrelated IPTV/channel-switcher commit. The original dirty checkout is untouched.
