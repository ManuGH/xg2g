# RED R2-A: Generator Support Gap Matrix

**Status:** COMPLETE
**Severity:** Multiple HIGH findings
**Discovered By:** Team Red (Phase R2)

---

## Executive Summary

`GenValidDecisionInput` and related generators cover only a narrow "happy path" subset of the actual input space. Several dimensions are either impossible or highly improbable to generate, yet they are legal inputs that could trigger edge-case behavior.

---

## Gap Matrix

| Dimension | Generator Can? | Example | Expected Impact |
|---|---|---|---|
| **SupportsRange = nil** | ❌ NO | `nil` vs `false` | Tri-state semantics. Engine treats `nil` as `false`. Hash normalizes to `omitempty`. Different JSON outputs for semantically identical inputs. |
| **Empty Caps Slices** | ⚠️ RARE | `Containers: []` | `subset()` can return empty, but only if `k==0`. Not explicitly tested. |
| **Nil Caps Slices** | ❌ NO | `Containers: nil` | Go `nil` vs `[]` has JSON difference (`null` vs `[]`). CanonicalJSON handles, but predicates don't distinguish. |
| **Duplicates in Caps** | ❌ NO | `["h264","h264"]` | `sortedUnique` dedupes for hash, but `contains` checks original slice. Hash can differ from logic input. |
| **Whitespace/Case Variants** | ❌ NO | `" H264 "` | `contains` normalizes (trim+lower). Hash normalizes. But if normalization is inconsistent, silent mismatch. |
| **isMP4 Check Case-Sensitive** | ⚠️ BUG | `Container: "MP4"` | `isMP4` uses `==` (case-sensitive), but `contains` is case-insensitive. `CanContainer=true`, `isMP4=false` → DirectPlay blocked for valid MP4. |
| **Unicode Normalization** | ❌ NO | `"h264\u00A0"` (NBSP) | `TrimSpace` only trims ASCII whitespace. NBSP not trimmed. Silent mismatch. |
| **Slice Order Permutations** | ❌ NO | `["h264","hevc"]` vs `["hevc","h264"]` | Hash sorts (canonical). `contains` is order-agnostic. No difference if correctly implemented. |
| **Very Long Slices** | ❌ NO | 1000 codecs | No explicit test. May have performance implications. |
| **Policy Toggle Mid-Pair** | ❌ NO | Monotonic pair with different Policy | `GenMonotonicPair` keeps policy constant. Policy monotonicity not fully tested. |

---

## Critical Finding: isMP4 Case Sensitivity

**Severity:** HIGH
**Class:** Oracle Coupling / Normalization Bug
**Target:** `predicates.go` line 18

### Description

`isMP4` check uses raw `==` comparison:

```go
isMP4 := source.Container == "mp4" || source.Container == "mov" || source.Container == "m4v"
```

But `contains` (line 44) normalizes:

```go
item = strings.ToLower(strings.TrimSpace(item))
```

If input has `Container: "MP4"` (uppercase):

- `canContainer` = TRUE (normalized match)
- `isMP4` = FALSE (case-sensitive mismatch)
- **Result:** DirectPlay blocked for valid MP4 container.

### Proof

```go
// This input would produce unexpected behavior:
input := DecisionInput{
    Source: Source{Container: "MP4", VideoCodec: "h264", AudioCodec: "aac"},
    Capabilities: Capabilities{
        Containers: []string{"mp4"},
        VideoCodecs: []string{"h264"},
        AudioCodecs: []string{"aac"},
        SupportsHLS: true,
        SupportsRange: boolPtr(true),
    },
}
// Expected: DirectPlay (Container matches, Range ok, MP4)
// Actual: DirectStream (isMP4 false due to case)
```

### Fix Options

- **A (Preferred):** Normalize `source.Container` before `isMP4` check.
- **B:** Normalize at input validation time (upstream).

---

## Secondary Finding: SupportsRange Tri-State

**Severity:** MEDIUM
**Class:** Generator Gap

`boolPtr(r.Intn(2) == 0)` always produces `*bool` with value. Never produces `nil`.

`nil` semantics in `hasRange`:

```go
hasRange := caps.SupportsRange != nil && *caps.SupportsRange
```

`nil` → `false`. But `nil` and `*false` might have different JSON representations (`omitempty` removes `nil`).

---

## Recommendations

1. **Immediate:** Fix `isMP4` case sensitivity (Gate-blocking).
2. **Add Generator:** `GenCaseMismatchInput` for case variant testing.
3. **Add Generator:** `GenNilRangeInput` for tri-state testing.
4. **Add Property:** `Prop_Normalization_Consistency` (CanonicalJSON hash must be stable across case/whitespace variants).
