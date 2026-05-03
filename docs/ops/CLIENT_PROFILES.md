# Browser Family Playback Matrix Policy

This document is the binding operational policy for browser-family playback
classification. It freezes the supported web playback families, the mandatory
source fixtures, and the rule for introducing new browser families.

The canonical fixture IDs live in:

- `backend/internal/domain/playbackprofile/client_matrix.go`
- `backend/internal/domain/playbackprofile/source_matrix.go`
- `backend/internal/control/http/v3/autocodec/client_av1_policy.go`

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
| `android_tv_browser` | Android/Fire TV browser paths, including Shield/Silk-style clients | `hls.js` | `mp4`, `ts`, `fmp4` | `h264` | `aac`, `mp3` | TV browser baseline. AV1/HEVC require runtime proof and AV1 still needs the hardware guard. |
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
- For live Safari families, a `direct` decision means "no video re-encode
  required", not "no server-side media process". `xg2g` may still run FFmpeg in
  copy/remux mode to package MPEG-TS HLS and normalize audio for delivery.

## AV1 Hardware Client Guard

AV1 is not a baseline browser-family claim. The backend may select AV1 only
when the client passes the dedicated hardware decode guard in
`ClientAV1PlaybackAllowed(...)`.

Required for every AV1 client:

- `videoCodecs` contains `av1`.
- `containers` contains `fmp4`.
- `clientCapsSource` is `runtime` or `runtime_plus_family`.
- The runtime codec signal for `av1` is supported.
- Family fallback alone is never enough.
- Native WebKit must use fMP4 for AV1; AV1 over MPEG-TS is not a supported
  Safari/iPhone transport.

Apple mobile policy:

- `ios_safari_native` requires iOS/iPadOS 17 or newer when the OS version is
  known.
- Known AV1-capable markers include `A17 Pro`, `A18`, `A19`, `iPhone 15 Pro`,
  `iPhone 15 Pro Max`, the iPhone 16 family including `iPhone 16e`, the iPhone
  17 family, `iPhone Air`, `iPad mini A17 Pro`, `iPad Air M3`, and
  `iPad Pro M4`.
- Native `hw.machine` style identifiers for those iPhone generations are also
  accepted when a native wrapper or trusted probe supplies them.
- Known pre-AV1 markers are blocked even if a generic runtime advertises AV1:
  `A16` and older mobile chips, non-Pro iPhone 15, iPhone 14/13/12/11/SE,
  iPad A16, iPad Air M1/M2, and iPad Pro M1/M2.

Apple desktop policy:

- `safari_native` on macOS requires macOS 14 or newer when the OS version is
  known.
- Apple Silicon `M3`, `M4`, and newer are treated as AV1-capable.
- `M1` and `M2` are blocked for AV1 hardware playback.
- If Safari does not expose a reliable model, the runtime AV1 probe must still
  prove support before AV1 can be selected.

Android and generic desktop policy:

- Android TV browser clients start from an H264-only baseline. AV1 is allowed
  only when runtime probing proves AV1 and either the OS is Android 14 / SDK 34
  or newer, or the device model is on the known hardware-AV1 allowlist.
- Known Android/Fire TV AV1 markers include Amazon build models `AFTCL001`,
  `AFTMA08C15`, `AFTCA002`, `AFTKRT`, `AFTKM`, `AFTKA`, `AFTGAZL`, plus Xiaomi
  TV Stick 4K markers such as `MDZ-27-AA` / `MiTV-AYFR0`.
- Known no-AV1 Android TV markers are blocked even with a generic smooth AV1
  browser signal. This includes NVIDIA Shield / Tegra X1+ markers such as
  `SHIELD Android TV`, `mdarcy`, and `foster`.
- Android 10-13 requires a stronger runtime signal: `smooth` or
  `powerEfficient`.
- Windows, Linux, and VM clients require `smooth` or `powerEfficient`; a plain
  `supported=true` signal is not enough.

Operational trace fields to inspect when AV1 is expected:

- `trace.clientCapsSource`
- `trace.clientFamily`
- `trace.selected.videoCodec`
- `trace.selected.container`
- `trace.targetProfile.hls.segmentContainer`
- `trace.autoCodec.selected`
