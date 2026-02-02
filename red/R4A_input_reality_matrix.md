# RED R4-A: Input Reality Attack Matrix

**Status:** IN PROGRESS
**Phase:** R4-A (Operator Reality Attack)
**Goal:** Verify engine handles real-world extractor garbage correctly

---

## Class 1: Missing / Empty Fields (Expected: 422 `decision_ambiguous`)

**Semantik:** syntaktisch valide, aber semantisch unentscheidbar (P4-2). Missing fields sind **kein** "Bad Request", sondern fehlende Truth.

**Why not 400?**
- 400 = syntaktisch/formatmäßig falsch.
- 422 = formal ok, aber nicht deterministisch entscheidbar.

### Repro M1: Empty Container

```json
{"source":{"c":"","v":"h264","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 422, Problem.Code = "decision_ambiguous"

### Repro M2: Empty VideoCodec

```json
{"source":{"c":"mp4","v":"","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 422

### Repro M3: Empty AudioCodec

```json
{"source":{"c":"mp4","v":"h264","a":"","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 422

### Repro M4: Whitespace-only fields

```json
{"source":{"c":"   ","v":"h264","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 422 (TrimSpace makes it empty)

---

## Class 2: Unrecognized Values (Expected: Transcode if allowed)

### Repro U1: Unknown Codec String "av1"

```json
{"source":{"c":"mp4","v":"av1","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264","hevc"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = Transcode (NOT Deny)

### Repro U2: RFC6381-style codec "avc1.4d401f"

```json
{"source":{"c":"mp4","v":"avc1.4d401f","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = Transcode (string doesn't match caps)

### Repro U3: Composite string "h264 (avc1)"

```json
{"source":{"c":"mp4","v":"h264 (avc1)","a":"aac","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = Transcode

### Repro U4: Future codec "vvc"

```json
{"source":{"c":"mp4","v":"vvc","a":"opus","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = Transcode

---

## Class 3: Dirty but Plausible (Expected: Correct handling)

### Repro D1: Mixed case "H264"

```json
{"source":{"c":"MP4","v":"H264","a":"AAC","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = DirectPlay (normalized match)

### Repro D2: Trailing whitespace " h264 "

```json
{"source":{"c":" mp4 ","v":" h264 ","a":" aac ","br":3000,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200, Mode = DirectPlay (TrimSpace normalized)

### Repro D3: Zero bitrate

```json
{"source":{"c":"mp4","v":"h264","a":"aac","br":0,"w":1920,"h":1080,"fps":30},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200 (bitrate is not decision-critical in current scope)

### Repro D4: Very high values

```json
{"source":{"c":"mp4","v":"h264","a":"aac","br":1000000,"w":7680,"h":4320,"fps":120},"caps":{"v":1,"c":["mp4"],"vc":["h264"],"ac":["aac"],"hls":true,"rng":true,"dev":"test"},"policy":{"tx":true},"api":"v3"}
```

**Expected:** Status 200 (no bitrate/resolution limits in current scope)

---

## Properties to Implement

1. **Prop_MissingFields_Always422**: Missing/empty source fields → 422 (`decision_ambiguous`)
2. **Prop_UnrecognizedValues_NeverCauseDenyIfTranscodeAllowed**: Unrecognized codec + AllowTranscode → Transcode, never Deny
