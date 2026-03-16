# INCIDENT PLAYBOOK: Playback Decision Engine

**Target Triage Time:** < 10 Minutes
**System:** Recordings Playback Decision (Internal)
**Version:** v3.1 (ADR-009 compliant)

---

## 1. The Decision ID (InputHash)

Every decision incident MUST be identified by its **InputHash**.

- If two users have the same InputHash, they MUST get the same Decision.
- If they don't, you have a **Determinism Breach** (Severity: Critical).

### How to get the InputHash

- From Client Logs: Look for `trace.inputHash` in the response body.
- From Server Logs: `decision_input_hash` field.

---

## 2. Replay & Reproduction

We do not debug "on live requests". We use the **Canonical JSON** (Replay Artifact).

### Steps

1. Obtain the **Canonical JSON** from logs/telemetry.
2. Run the offline replay tool:

   ```bash
   ./tools/decision-replay --input ./incident_repro.json
   ```

3. Verify if the reproduction matches the reported behavior.

---

## 3. Triage Matrix (Symptom → Root Cause)

| Symptom | Source of Truth | Check | Action |
|---------|-----------------|-------|--------|
| **HTTP 400 (Ambiguous)** | `Problem.Code` | Check `source.c`, `source.v`, `source.a` | Missing metadata from Scanner/Extractor. Fix ingest pipeline. |
| **HTTP 500 (Invariant)** | `Problem.Detail` | Logic breach detected. | **Stop the line.** Internal Engine bug. Escalation required. |
| **Unexpected Deny** | `reasons[]` | Check `policy.tx` and `caps.vc` | Client does not support the Source codec and server disallowed transcoding. |
| **DirectPlay Fail (Range)**| `reasons[]` | Check `caps.rng` | Client claims Range support but transport layer (Proxy/VPN) stripped it. |
| **Hash Mismatch** | `adr/009.2` | Check whitespace/nulls in input. | Check if client sent `null` vs `false` for SupportsRange. (Engine normalizes this, but check if bypass exists). |

---

## 4. Operator Overrides First

Before changing fixtures, client classification, or transcode policy, inspect
`trace.operator`.

- `forcedIntent` means the playback path was intentionally biased by an active
  operator override.
- `maxQualityRung` means the selected rung may be capped below the normal
  client/source optimum by operations policy.
- `clientFallbackDisabled=true` means a client-reported playback error will not
  trigger the usual restart/fallback path via `/sessions/{id}/feedback`.
- `ruleName` tells you which exact per-source override rule matched.
- `ruleScope` tells you whether that rule matched in `live`, `recording`, or
  `any` scope.

If `trace.operator.overrideApplied=true`, treat the observed output as an
operator-forced result first, not as an autonomous ladder decision.

---

## 5. Client Capability Provenance

Before blaming the ladder, check how the server classified the client truth.

- `trace.clientCapsSource=runtime`
  Runtime browser probing supplied the effective truth.
- `trace.clientCapsSource=family_fallback`
  Runtime probe was absent or insufficient; the browser-family matrix supplied
  the operative truth.
- `trace.clientCapsSource=runtime_plus_family`
  Runtime probe stayed primary, but the family filled a missing or generic
  field such as `deviceType=web`.

Use `trace.clientFamily` to see which family fixture the server used as the
fallback identity.

Triage rule:

- If `trace.clientCapsSource=runtime`, start with browser/runtime capability
  debugging.
- If `trace.clientCapsSource=family_fallback`, start with family-matrix policy
  and fixture coverage.
- If `trace.clientCapsSource=runtime_plus_family`, check which missing field the
  family had to supply before changing ladder policy.

---

## 6. Understanding Reasons

Reasons are **normative**. They are not just logs.

- `directplay_match`: Perfect compatibility found.
- `policy_denies_transcode`: Playback path found but blocked by server policy.
- `unsupported_container`: Source container not in client `caps.c`.
- `unsupported_video_codec`: Source video codec not in client `caps.vc`.

## 7. Media Preflight Wins Over Client Guessing

If a failed or gone session carries `trace.preflightReason`, treat that as the
authoritative startup diagnosis for the media path.

- `trace.preflightReason=timeout` means the source timed out before startup.
- `trace.preflightReason=invalid_ts` means transport stream sync was not
  detected on startup.
- `trace.preflightReason=corrupt_input` means bytes arrived but the startup
  sample was too broken to trust.
- `trace.preflightReason=no_video` means startup reached media inspection but
  did not find usable video.

Use `trace.preflightDetail` for the low-level adapter hint (`sync_miss`,
`short_read`, `http_status_401`, etc.). If `trace.stopClass=input` and
`trace.preflightReason` is present, do **not** start with player/fallback
debugging. Triage the upstream source first.

## 8. Ladder And Output Verification

Before changing intent policy, check whether the server chose a different
audio/video rung than the one you expected.

- `trace.qualityRung` is the legacy summary view.
- `trace.audioQualityRung` is the explicit audio ladder result.
- `trace.videoQualityRung` is the explicit video ladder result.

For video transcodes, confirm that the output profile matches the chosen rung:

- `trace.targetProfile.video.mode=transcode`
- `trace.targetProfile.video.codec=h264`
- `trace.targetProfile.video.crf=<expected rung CRF>`
- `trace.targetProfile.video.preset=<expected rung preset>`

If `trace.videoQualityRung` says `compatible_video_h264_crf23_fast`, but the
target profile does not show `crf=23` and `preset=fast`, treat that as a
builder/trace drift, not a client-capability incident.

---

## 9. Escalation Path

1. **Category A: Logic Bug** (Decision internal state drift). Record the RequestID and InputHash.
2. **Category B: Spec Gap** (Producing Deny for a path that should be valid). Update `proof_test.go` with a new Property.
3. **Category C: Data Quality** (Scanner returned garbage). Triage the source file.
