# AV1 iOS Blocker (2026-04-21)

## Summary

The AV1 blocker on iPhone/iOS Safari was not a generic "iPhone cannot do AV1"
problem and not a backend codec-selection problem.

The blocker was a malformed AV1 output geometry on the current AMD VAAPI path
used for live transcoding. In the failing case, a nominal 1080-line AV1 stream
was encoded in a way that decoded as 1082 lines on the verification side. iOS
Safari accepted the session, played audio, and reported playback progress, but
video remained black/dark.

## User-Visible Symptom

- audio present
- video black or very dark on iPhone
- native HLS session reached `playing`, but visible video was missing
- issue reproduced on live AV1 playback for broadcast TV content such as Pro7

## Root Cause

The failing path was the AMD VAAPI AV1 live transcode at 1080 height.

Observed behavior:

- encoded init segment advertised AV1 Main at 1920x1080
- verification decode showed frames at 1920x1082 instead of 1920x1080
- this mismatch was sufficient to produce broken rendering on iOS Safari

The important conclusion is that the stream was not empty and not actually
"all black" at the encoder level. Server-side decode and signal analysis showed
real image data. The failure was in the produced AV1 bitstream shape as
consumed by Safari.

## Hard Evidence

Before the fix:

- AV1 VAAPI 1080p output decoded as `1920x1082`
- iPhone reported audio playback but no usable picture

After the fix:

- AV1 init segment reports `1920x1088`, progressive
- iPhone session selected `autoCodecSelected=av1`
- playback feedback reached:
  - `code=200` (`playing`)
  - `code=230` (`native_render stage=playing`)
  - `code=231` (`native_render stage=stable`)
- render telemetry reported visible output with `vw=1920 vh=1088`

## Fix

AV1 was moved to an encode-only VAAPI path so geometry normalization can happen
in software before `hwupload`.

Applied changes:

- force AV1 hardware profile to `vaapi_encode_only`
- disable full VAAPI for resolved AV1 plans even if requested upstream
- pad AV1 input to a 16-line boundary before upload:
  - `pad=iw:ceil(ih/16)*16:0:(oh-ih)/2:black`

For 1080-line content this means:

- `1080 -> 1088`

This avoids the malformed `1080 -> 1082` decode behavior seen on the broken
AMD VAAPI AV1 path.

## Code Reference

- `backend/internal/pipeline/profiles/resolve.go`
  - `ProfileAV1HW` resolves to `vaapi_encode_only`
- `backend/internal/infra/media/ffmpeg/plan_builder.go`
  - AV1 disables full VAAPI
  - `av1VAAPIGeometryPadFilter()` pads height before `hwupload`

## Non-Conclusion

This incident does not prove that AV1 is generally unreliable on iOS.

It proves a narrower point:

- the previous AV1 live-transcode path on this AMD VAAPI stack produced
  geometry that Safari did not render correctly

It is also not the same issue as the older HEVC Safari behavior. HEVC has had
separate packaging/runtime constraints, while this AV1 incident was specifically
a malformed live VAAPI output geometry problem.
