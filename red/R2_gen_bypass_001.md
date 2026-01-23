# RED GEN BYPASS 001

**Severity:** HIGH
**Class:** Normalization Bug / Oracle Coupling
**Target:** `predicates.go` line 18 (`isMP4` check)
**Repro:** `Container: "MP4"` (uppercase)

---

## 1. Description

The `isMP4` predicate check uses raw string comparison (`==`), while `contains` (used for `CanContainer`) normalizes via `ToLower + TrimSpace`. This creates a case sensitivity mismatch.

---

## 2. Repro (Canonical JSON)

```json
{
  "source": {"c": "MP4", "v": "h264", "a": "aac", "br": 3000, "w": 1920, "h": 1080, "fps": 30},
  "caps": {"v": 1, "c": ["mp4"], "vc": ["h264"], "ac": ["aac"], "hls": true, "rng": true, "dev": "test"},
  "policy": {"tx": true},
  "api": "v3"
}
```

**Expected (Spec):**

- `CanContainer` = TRUE (normalized match: `"MP4"` -> `"mp4"`)
- `isMP4` = TRUE (Container is MP4 family)
- `DirectPlayPossible` = TRUE
- **Mode: DirectPlay**

**Observed (Engine):**

- `CanContainer` = TRUE
- `isMP4` = FALSE (raw `"MP4" != "mp4"`)
- `DirectPlayPossible` = FALSE
- **Mode: DirectStream** (or Transcode if HLS false)

---

## 3. Why Proof Failed

The proof system (Model and Engine) both share the same bug:

- Model: Uses `source.Container == "mp4"` (same case-sensitive logic).
- Engine: Uses identical logic.

Result: **Both are consistently wrong** → Model Consistency passes, but decision is incorrect.

---

## 4. Blast Radius

- Any client sending uppercase `Container` (e.g., from certain media extractors) will be unexpectedly routed to DirectStream instead of DirectPlay.
- Higher CPU usage (HLS packaging) for content that could have played directly.
- Violates ADR-009.1 intent (normalized inputs should match).

---

## 5. Fix Options

**A (Preferred):** Normalize before `isMP4` check.

```go
containerNorm := strings.ToLower(strings.TrimSpace(source.Container))
isMP4 := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v"
```

**B:** Normalize at input validation (upstream, requires schema change).

---

## 6. New Gate

**Add Property:** `TestProp_Normalization_DirectPlay`

- Generator: Inputs with uppercase containers (`MP4`, `MOV`, `M4V`).
- Assertion: If CanContainer is true and Range is true, DirectPlay should be possible.
