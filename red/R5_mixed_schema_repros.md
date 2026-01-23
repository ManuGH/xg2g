# R5 Mixed Schema Repros (Fail-Closed)

These canonical repro cases verify the "Mix is 400" rule (Condition 1 & 2).

### 1. Top-Level Mix

**Scenario**: Malicious/ambiguous probe sending both object keys.

```json
{
  "source": {"c":"mp4"},
  "Source": {"container":"ts"},
  "api": "v3"
}
```

**Expected**: `400 Bad Request`, `Problem.Code: capabilities_invalid`.

### 2. Nested Source Mix

**Scenario**: Internal field overlap within compact source.

```json
{
  "source": {
    "c": "mp4",
    "container": "mp4",
    "v": "h264"
  },
  "api": "v3"
}
```

**Expected**: `400 Bad Request`, Detail: "nested source: both c and container present".

### 3. Capabilities Mix

**Scenario**: Overlap in boolean capability claims.

```json
{
  "caps": {
    "rng": true,
    "supportsRange": true,
    "v": 1
  },
  "api": "v3"
}
```

**Expected**: `400 Bad Request`, Code: `capabilities_invalid`.
