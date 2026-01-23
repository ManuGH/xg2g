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

## 4. Understanding Reasons

Reasons are **normative**. They are not just logs.

- `directplay_match`: Perfect compatibility found.
- `policy_denies_transcode`: Playback path found but blocked by server policy.
- `unsupported_container`: Source container not in client `caps.c`.
- `unsupported_video_codec`: Source video codec not in client `caps.vc`.

---

## 5. Escalation Path

1. **Category A: Logic Bug** (Decision internal state drift). Record the RequestID and InputHash.
2. **Category B: Spec Gap** (Producing Deny for a path that should be valid). Update `proof_test.go` with a new Property.
3. **Category C: Data Quality** (Scanner returned garbage). Triage the source file.
