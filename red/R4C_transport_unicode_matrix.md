# RED R4-C: Transport & Unicode Reality Matrix

**Status:** IN PROGRESS
**Phase:** R4-C (Transport & Unicode Reality)
**Goal:** Address real-world production noise at the boundaries.

---

## Attack C1: Unicode Whitespace (NBSP, ZWSP)

**System Assumption:** Extractor strings are clean ASCII.
**Reality:** Metadata often contains non-breaking spaces (NBSP, `\u00A0`) or zero-width joiners from copy-paste or weird database encodings.

### Repro C1.1: NBSP in Container

```json
{"source":{"c":"mp4\u00a0","v":"h264","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Decision Requirement:** MUST be normalized to "mp4" and produce same hash as pure "mp4".
**Status:** ✅ Implemented (`robustNorm` + `TestProp_UnicodeWhitespace_Equivalence`).

---

## Attack C2: Transport Reality (Range-Strip)

**System Assumption:** Capability `rng: true` means Range works.
**Reality:** Safari Private Relay or certain Carrier Proxies strip `Range` headers even if the client *claims* support.

### Repro C2.1: Safari iOS Cellular Profile

**Attack:** Use `safari_ios_cellular` profile which has `rng: false`.
**Expected:** ModeDirectStream (HLS) or ModeTranscode, but NEVER ModeDirectPlay (which requires Range).
**Finding:** Already enforced by Invariant #9, but we verify profile integration here.

---

## Attack C3: Encoding/Normalization Boundaries

**System Assumption:** Input is already UTF-8 and normalized.
**Reality:** Boundary systems might send precomposed vs decomposed Unicode.

### Normalization Policy (Design Decision)

1. **ASCII Trim:** Covered by `robustNorm` (via `unicode.IsSpace`).
2. **Unicode Trim:** **IMPLEMENTED** via `robustNorm` (NBSP, ZWSP, BOM, etc.).
3. **Casing:** Already implemented (lowercase).
4. **Emoji/Symbols:** **OUT OF SCOPE** (treated as unrecognized characters).

---

## Deliverables

- [x] Implement `robustNorm` helper (`normalize.go`).
- [x] Property: `Prop_UnicodeWhitespace_Equivalence`.
- [x] Logic Gate: `Decide` uses normalization before predicates (`NormalizeInput`).
