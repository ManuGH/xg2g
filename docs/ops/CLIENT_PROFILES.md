# Browser Family Playback Matrix Policy

This document is the binding operational policy for browser-family playback
classification. It freezes the supported web playback families, the mandatory
source fixtures, and the rule for introducing new browser families.

The canonical fixture IDs live in:

- `backend/internal/domain/playbackprofile/client_matrix.go`
- `backend/internal/domain/playbackprofile/source_matrix.go`

These names are normative and must not drift from the documentation.

## Supported Browser Families

Web playback is classified by effective playback family, not by browser brand.
The family key is determined by the playback engine path and the conservative
codec/container set that `xg2g` may rely on.

| Family ID | Typical Clients | Playback Path | Containers | Video | Audio | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| `safari_native` | Safari on macOS | Native HLS | `mp4`, `ts` | `h264`, `hevc` | `aac`, `mp3`, `ac3` | Desktop WebKit path. |
| `ios_safari_native` | Safari on iPhone/iPad | Native HLS | `mp4`, `ts` | `h264`, `hevc` | `aac`, `mp3`, `ac3` | Touch/mobile WebKit path. |
| `firefox_hlsjs` | Firefox desktop web | `hls.js` | `mp4`, `ts`, `fmp4` | `h264` | `aac`, `mp3` | Conservative browser-safe web path. |
| `chromium_hlsjs` | Chrome, Edge, Brave, Opera desktop web | `hls.js` | `mp4`, `ts`, `fmp4` | `h264` | `aac`, `mp3` | Chromium-brand variants reuse this family until proven otherwise. |

### Binding Rules

- Safari on macOS and Safari on iOS remain separate families, even though both
  are WebKit, because their playback and control paths differ.
- `hevc` and `ac3` are Safari-family-only claims unless another family earns
  them through matrix coverage.
- Firefox/Chromium web playback remains conservative:
  `h264` + `aac/mp3` over `hls.js`.
- A browser brand must not get its own family unless its measured playback path
  differs materially from an existing family.

## Mandatory Source Fixtures

Every ladder, resolver, and frontend matrix must cover these source fixture IDs.

| Source ID | Canonical Shape | Why It Must Stay |
| --- | --- | --- |
| `h264_aac` | Clean progressive web-safe source | Baseline happy path. |
| `h264_ac3` | H.264 video with AC3 audio | Proves audio downgrade/transcode behavior. |
| `hevc_aac` | HEVC video with AAC audio | Proves Safari-only HEVC claims and video fallback. |
| `mpegts_interlaced` | Interlaced MPEG-TS source | Guards transport/interlace behavior. |
| `dvb_dirty` | Deliberately incomplete/dirty DVB truth | Guards fail-soft reality, not idealized samples. |

`dvb_dirty` is a required fixture. It must not be removed or silently
"cleaned up" in tests.

## Classification Policy For New Browsers

New browsers must be classified into an existing family unless there is a
measured reason to create a new one.

### Reuse An Existing Family When

- the browser shares the same effective playback engine path (`native_hls` vs
  `hls.js`);
- the conservative codec/container claims are identical; and
- fullscreen/mobile behavior does not require a different playback family.

Examples:

- Chromium brands (`Edge`, `Brave`, `Opera`) map to `chromium_hlsjs` while the
  measured playback path stays identical.
- A Gecko-based desktop browser maps to `firefox_hlsjs` while it remains an
  `hls.js` + `h264` + `aac/mp3` web path.

### Introduce A New Family Only When

- native HLS vs `hls.js` behavior differs materially from all existing families;
- codec claims differ materially, for example safe `hevc` or `ac3` support
  outside the Safari families;
- packaging constraints differ materially, for example `fmp4` vs `ts`; or
- mobile/touch/fullscreen behavior requires a separate playback path.

### Required Work To Add A New Family

Before a new family is accepted, all of the following must be updated in the
same change:

1. Add the fixture to `client_matrix.go`.
2. Cover it in the recording decision matrix tests.
3. Cover it in the live resolver matrix tests.
4. Cover it in the frontend browser-matrix tests.
5. Update this policy document.

## Best-Practice Guardrails

- Do not model individual browser brands when an engine family already captures
  the behavior.
- Do not widen codec claims from anecdotal playback success; widen them only
  from matrix-backed proof.
- Keep Live and Recordings on the same family vocabulary.
- Treat the browser-family matrix as the compatibility contract; ad-hoc browser
  exceptions are not policy.

## Operator Overrides

Browser-family classification defines the default compatibility truth.
Temporary incident controls must not be encoded as new browser families.

Use playback operator overrides only as runtime policy controls:

- `playback.operator.force_intent`
- `playback.operator.max_quality_rung`
- `playback.operator.disable_client_fallback`

When an override is active, the session trace exposes it under
`trace.operator.*`. This is an operational bias on top of the family matrix,
not a redefinition of the client family itself.

If a per-source rule matched, `trace.operator.ruleName` and
`trace.operator.ruleScope` tell you exactly which ordered rule won.

## Runtime Probing Contract

Runtime browser probing refines the family matrix. It does not replace it.

### Precedence

The binding merge order is:

1. runtime browser probe
2. browser-family fallback
3. existing conservative backend behavior if neither side yields more truth

This means:

- runtime-probed codec, container and engine claims win when they are present;
- family fallback fills only missing or generic fields, such as `deviceType=web`
  or absent HLS engine metadata;
- the server must not widen capability claims beyond what runtime probe or the
  chosen family fixture actually says.

### Trace Semantics

The playback-info decision trace exposes this merge under:

- `trace.clientCapsSource=runtime`
- `trace.clientCapsSource=family_fallback`
- `trace.clientCapsSource=runtime_plus_family`
- `trace.clientFamily=<family id>`

The playback trace also separates ladder decisions by media kind:

- `trace.audioQualityRung`
- `trace.videoQualityRung`

When video is actively transcoded, the exposed target profile is expected to
carry the effective encoder shaping under:

- `trace.targetProfile.video.crf`
- `trace.targetProfile.video.preset`

Interpretation:

- `runtime`: runtime probe was complete enough; the family added nothing.
- `family_fallback`: no usable runtime probe truth was present; the family
  supplied the operative capability identity.
- `runtime_plus_family`: runtime probe stayed primary, but the family filled a
  missing or generic field.

### Guardrails

- Missing runtime probe is not an error; it should degrade to family fallback.
- Unknown family fallback must not invent extra capability truth.
- Desktop Safari and iOS Safari remain distinct even under family-only fallback.
- Firefox and Chromium stay conservative unless runtime probe proves more.
