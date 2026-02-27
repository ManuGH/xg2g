# Duration Truth (VOD/Recordings)

## Objective

`durationMs` for recordings is resolved once in backend, transported as structured truth, and consumed by WebUI without local duration guessing.

## Single Source Of Truth

Authoritative DTO fields (`PlaybackInfo`):

- `durationMs` (authoritative finite duration)
- `durationSource` (`source_metadata | ffprobe | container | heuristic | unknown`)
- `durationConfidence` (`high | medium | low`)
- `durationReasons[]` (frozen reason vocabulary)
- `isSeekable` / `seekable` (server-authoritative seek policy)

Compatibility field:

- `durationSeconds` remains derived from `durationMs`.

## Resolution Priority

Implemented resolver order (`internal/control/recordings/duration_truth.go`):

1. `source_metadata` (primary)
2. `ffprobe` / `container` (secondary)
3. `heuristic` (tertiary, degraded)
4. `unknown` (no finite duration truth)

Hard guards:

- invalid/zero/negative durations are rejected
- overflow/extreme values are clamped (`duration_inconsistent_clamped`)
- unknown duration emits `duration_unknown_denied_seek`

## Backend Wiring

- Domain model and reason SSOT:
  - `internal/control/recordings/duration_truth.go`
- Resolver tests:
  - `internal/control/recordings/duration_truth_resolver_test.go`
- Playback DTO mapping and seek policy:
  - `internal/control/http/v3/handlers_playback_info.go`
- Contract allowlist:
  - `internal/control/http/v3/schema_compliance_test.go`

## Resume/Seek Semantics

- Resume position is server-clamped to `[0, durationSeconds]` when finite duration exists.
- Clamp emits reason `resume_clamped_to_duration`.
- Finite VOD seek is denied when duration is unknown or low-confidence.

## Reason Vocabulary

- `duration_from_source_metadata`
- `duration_from_ffprobe`
- `duration_from_container`
- `duration_from_heuristic`
- `duration_primary_missing`
- `duration_probe_failed`
- `duration_container_missing`
- `duration_inconsistent_clamped`
- `duration_unknown_denied_seek`
- `resume_clamped_to_duration`

## Mechanical Gates

- Gate F (No UI guessing):
  - `webui/tests/contracts/v3player.duration-truth-gate.test.ts`
- Gate K (No Duration Guessing, repo-wide UI policy):
  - `npm --prefix webui run gate:no-duration-guessing`
  - `webui/scripts/no-duration-guessing.mjs`
- Gate G/H (deterministic resolver + clamp):
  - `internal/control/recordings/duration_truth_resolver_test.go`
- Gate I (DTO schema stability):
  - `internal/control/http/v3/schema_compliance_test.go`
- Gate J (resume clamp safety):
  - `internal/control/http/v3/handlers_playback_info_duration_truth_test.go`
