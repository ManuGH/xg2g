package ffmpeg

import (
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"strconv"
	"strings"
)

func usesLegacyCPUDefaults(spec ports.StreamSpec, outputCodec string) bool {
	prof := spec.Profile
	return prof.Name == "" && prof.VideoCodec == "" && !prof.TranscodeVideo && outputCodec == "h264"
}

// appendVideoGOPArgs appends the GOP/keyframe cadence shared verbatim by every
// hardware and CPU encoder path. Kept identical so forced segment boundaries
// align across transcode modes.
func appendVideoGOPArgs(args []string, gop, segmentSec int) []string {
	return append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
	)
}

// vaapiEncoderForCodec maps the requested output codec to its VAAPI encoder name.
// Uses normalizeRequestedCodec to handle aliases (e.g. "h265" → "hevc_vaapi")
// and case/whitespace variations, consistent with other codec switches in this file.
func vaapiEncoderForCodec(outputCodec string) string {
	switch normalizeRequestedCodec(outputCodec) {
	case "hevc":
		return "hevc_vaapi"
	case "av1":
		return "av1_vaapi"
	default:
		return "h264_vaapi"
	}
}

func (a *LocalAdapter) buildVaapiVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.targetRateK", prof.VideoTargetRateK).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi")

	filters := make([]string, 0, 2)
	if prof.Deinterlace {
		filters = append(filters, "deinterlace_vaapi")
	}
	if prof.VideoMaxWidth > 0 {
		filters = append(filters, fmt.Sprintf("scale_vaapi=w=%d:h=-2", prof.VideoMaxWidth))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	args = append(args, "-c:v", vaapiEncoderForCodec(outputCodec))
	args = appendVaapiRateControlArgs(args, prof, outputCodec, a.Config)
	args = appendConservativeHEVCVAAPIArgs(args, spec, outputCodec)

	args = appendVideoGOPArgs(args, gop, segmentSec)

	if normalizeRequestedCodec(outputCodec) != "av1" {
		args = append(args, "-profile:v", "main")
	} else {
		args = appendAV1VAAPILevelArgs(args)
	}
	args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709")
	return args
}

// appendAV1VAAPILevelArgs pins the AV1 seq_level_idx to 5.0. The AMD VAAPI
// encoder derives the level from picture size alone and ignores frame rate:
// 1920x1088@50 gets level 4.1 (max ~70.8M samples/s) although it needs ~104M.
// macOS decoders tolerate the violation; the iPhone hardware decoder does not
// and never outputs a frame (endless buffering, then a decode error). Level
// 5.0 covers 1088p50 10-bit with headroom.
func appendAV1VAAPILevelArgs(args []string) []string {
	return append(args, "-level", "5.0")
}

func (a *LocalAdapter) buildVaapiEncodeOnlyVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi_encode_only").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi encode only")

	filter := a.vaapiEncodeOnlyFilter(spec, outputCodec)
	args = append(args, "-vf", filter)

	args = append(args, "-c:v", vaapiEncoderForCodec(outputCodec))
	args = appendVaapiRateControlArgs(args, prof, outputCodec, a.Config)
	args = appendConservativeHEVCVAAPIArgs(args, spec, outputCodec)

	args = appendVideoGOPArgs(args, gop, segmentSec)
	if normalizeRequestedCodec(outputCodec) != "av1" {
		args = append(args, "-profile:v", "main")
	} else {
		args = appendAV1VAAPILevelArgs(args)
	}
	args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709")
	return args
}

func (a *LocalAdapter) vaapiEncodeOnlyFilter(spec ports.StreamSpec, outputCodec string) string {
	parts := make([]string, 0, 4)
	if spec.Profile.Deinterlace {
		parts = append(parts, a.deinterlaceFilterForProfile(spec))
	}
	if spec.Profile.VideoMaxWidth > 0 {
		parts = append(parts, softwareScaleWidthFilter(spec.Profile.VideoMaxWidth))
	}
	isAV1 := normalizeRequestedCodec(outputCodec) == "av1"
	if isAV1 {
		parts = append(parts, av1VAAPIGeometryPadFilter())
	}
	// AV1 encodes 10-bit (p010 -> AV1 Main, which covers 8/10-bit). The extra
	// precision reduces encoder-introduced banding on gradients even from an
	// 8-bit source — the same quality rationale as the AV1 upscale above.
	// H.264/HEVC stay 8-bit (nv12) for broad client-decode compatibility.
	// Optional software-domain enhancement chain at final resolution, before
	// hwupload — "clean, then sharpen". Denoise strips broadcast compression grain
	// so the sharpener enhances real edges instead of noise; deband smooths gradient
	// banding (paired with the 10-bit output); the sharpener (luma unsharp) makes
	// edges visibly crisper, mimicking the edge enhancement a TV applies. These are
	// software filters and
	// roughly halve encoder headroom (verified ~2.4x -> ~1.3x realtime on 1080i
	// sports — still real-time for one session, thinner for concurrent ones), so
	// they default conservatively and are env-tunable. Only transcode paths reach
	// here — copy passthrough stays bit-exact and untouched.
	if f := transcodeDenoiseFilter(a.Config.TranscodeDenoise); f != "" {
		parts = append(parts, f)
	}
	if f := transcodeDebandFilter(a.Config.TranscodeDeband); f != "" {
		parts = append(parts, f)
	}
	if f := transcodeSharpenFilter(a.Config.TranscodeSharpen); f != "" {
		parts = append(parts, f)
	}
	uploadFormat := "nv12"
	if isAV1 {
		uploadFormat = "p010le"
	}
	parts = append(parts, "format="+uploadFormat, "hwupload")
	return strings.Join(parts, ",")
}

// transcodeSharpenFilter returns a luma unsharp-mask expression for the transcode
// chain, or "" when disabled. XG2G_TRANSCODE_SHARPEN is the unsharp luma amount
// (0 disables, default 1.5, capped at 3.0). Unsharp gives more visible, "TV-like"
// edge crispness than CAS — broadcast TVs apply aggressive edge enhancement by
// default — and was verified clean on 1080i wide shots up to ~1.5; chroma is left
// untouched to avoid colour fringing. It adds perceived crispness on edges/lines
// but cannot restore fine detail the source or the re-encode already discarded.
func transcodeSharpenFilter(amount float64) string {
	if amount <= 0 {
		return ""
	}
	return fmt.Sprintf("unsharp=5:5:%.2f:5:5:0.0", amount)
}

// transcodeDenoiseFilter returns an hqdn3d denoise expression for the transcode
// chain, or "" when disabled. XG2G_TRANSCODE_DENOISE scales a conservative base
// (0 disables, default 0.6, capped at 1.5); spatial and temporal strengths scale
// together. Encoder cost is fixed regardless of strength, so the strength is a
// pure quality knob (lower = gentler, preserves more fine detail).
func transcodeDenoiseFilter(s float64) string {
	if s <= 0 {
		return ""
	}
	return fmt.Sprintf("hqdn3d=%.1f:%.1f:%.1f:%.1f", 4*s, 3*s, 6*s, 4*s)
}

// transcodeDebandFilter returns a deband expression for the transcode chain, or
// "" when disabled via XG2G_TRANSCODE_DEBAND=false (default on; deband is gentle).
func transcodeDebandFilter(enabled bool) string {
	if !enabled {
		return ""
	}
	return "deband"
}

func av1VAAPIGeometryPadFilter() string {
	// Two geometry corrections, AV1-only, applied in the software domain before
	// hwupload:
	//
	//  1. Upscale sub-720p sources to at least 720 lines. Apple's M-series AV1
	//     hardware decoder renders HD AV1 (>= 720p) correctly but produces black
	//     video for SD-resolution AV1 (e.g. 720x576) while audio keeps playing.
	//     We upscale instead of downgrading to H.264 because AV1 keeps the
	//     quality advantage (less banding) even on SD content. The target width
	//     is derived from the input display aspect ratio so the result has square
	//     pixels (sar 1:1) and the DAR is preserved; HD sources (ih >= 720) pass
	//     through unscaled. The escaped commas keep max(720,ih) inside the scale
	//     expression rather than splitting the filtergraph.
	//
	//  2. Current AMD VAAPI AV1 encoders can emit 1080p bitstreams that decode as
	//     1082-line frames. Padding to a 16-line height keeps the decoded
	//     geometry stable for native HLS clients; SAR is adjusted before padding
	//     so the display aspect ratio is unchanged after 1088p.
	return "scale=w=trunc(max(720\\,ih)*dar/2)*2:h=max(720\\,ih):flags=lanczos," +
		"setsar=sar=sar*ceil(h/16)*16/h:max=1000," +
		"pad=iw:ceil(ih/16)*16:0:(oh-ih)/2:black"
}

func appendVaapiRateControlArgs(args []string, prof ports.ProfileSpec, outputCodec string, cfg AdapterConfig) []string {
	isAV1 := normalizeRequestedCodec(outputCodec) == "av1"
	if prof.VideoQP > 0 {
		args = append(args,
			"-rc_mode", "CQP",
			"-qp", strconv.Itoa(prof.VideoQP),
		)
		if prof.VideoMaxRateK > 0 {
			args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		}
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		if isAV1 {
			args = append(args, "-async_depth", "1")
		}
		return args
	}

	if prof.VideoMaxRateK > 0 {
		// AMD VAAPI AV1 (Phoenix3 / VCN4) stalls the VCN ring when -b:v == -maxrate.
		// Use a 25% target headroom (-b:v = 75% of -maxrate) to keep the ring stable.
		bV := prof.VideoTargetRateK
		if bV <= 0 {
			bV = prof.VideoMaxRateK
		}
		if isAV1 && prof.VideoTargetRateK <= 0 {
			bV = max((prof.VideoMaxRateK*3)/4, 1)
		}
		// AV1 QVBR: quality-targeted encode that still honours -maxrate as a hard
		// ceiling. Verified on this AMD stack (Mesa 25.0.7 / VCN4): QVBR holds the
		// cap, is sustained-stable, and is immune to the b:v==maxrate ring-stall
		// that constrains plain VBR. QVBR REQUIRES -b:v ("Bitrate must be set for
		// QVBR RC mode"), which is set above. Disable with XG2G_AV1_QVBR=false to
		// fall back to implicit VBR; tune the quality target with
		// XG2G_AV1_QVBR_QUALITY (AV1 scale 0-255, lower = higher quality).
		av1QVBR := isAV1 && cfg.AV1QVBR
		if av1QVBR {
			args = append(args, "-rc_mode", "QVBR")
		}
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", bV),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		if av1QVBR {
			// Default 90 (sharpened from 110): a higher AV1 quality target that
			// the VideoMaxRateK ceiling still bounds, so it spends the available
			// bitrate on visibly cleaner motion. Lower XG2G_AV1_QVBR_QUALITY for
			// even higher quality (more bitrate); raise it to save bandwidth.
			args = append(args, "-global_quality", strconv.Itoa(cfg.AV1QVBRQuality))
		}
		if isAV1 {
			args = append(args, "-async_depth", "1")
		}
		return args
	}

	if isAV1 {
		args = append(args, "-rc_mode", "CQP", "-global_quality", "28", "-async_depth", "1")
		return args
	}
	return append(args, "-global_quality", "23")
}

func (a *LocalAdapter) buildNVENCVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "nvenc").
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.targetRateK", prof.VideoTargetRateK).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: nvenc")

	filters := make([]string, 0, 2)
	if prof.Deinterlace {
		filters = append(filters, a.deinterlaceFilterForProfile(spec))
	}
	if prof.VideoMaxWidth > 0 {
		filters = append(filters, softwareScaleWidthFilter(prof.VideoMaxWidth))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	encoder := "h264_nvenc"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_nvenc"
	case "av1":
		encoder = "av1_nvenc"
	}
	args = append(args, "-c:v", encoder)
	args = appendNVENCRateControlArgs(args, prof)
	args = appendVideoGOPArgs(args, gop, segmentSec)
	if outputCodec != "av1" {
		args = append(args, "-profile:v", "main")
	}
	return args
}

func appendNVENCRateControlArgs(args []string, prof ports.ProfileSpec) []string {
	if prof.VideoQP > 0 {
		args = append(args,
			"-rc", "constqp",
			"-qp", strconv.Itoa(prof.VideoQP),
		)
		if prof.VideoMaxRateK > 0 {
			args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		}
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		return args
	}

	if prof.VideoMaxRateK > 0 {
		bV := prof.VideoTargetRateK
		if bV <= 0 {
			bV = prof.VideoMaxRateK
		}
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", bV),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		return args
	}

	return append(args, "-cq", "23")
}

func appendConservativeHEVCVAAPIArgs(args []string, spec ports.StreamSpec, outputCodec string) []string {
	if !useConservativeHEVCVAAPILivePreset(spec, outputCodec) {
		return args
	}
	// iOS-native HEVC fMP4 attaches are sensitive to open-GOP recovery behavior.
	// Keep the VAAPI bitstream conservative so every forced segment boundary lands
	// on a full IDR, avoid B-frame reordering, and include AUD markers for
	// stricter Apple decoders.
	return append(args,
		"-bf", "0",
		"-aud", "1",
		"-idr_interval", "1",
		"-tier", "main",
	)
}

func useConservativeHEVCVAAPILivePreset(spec ports.StreamSpec, outputCodec string) bool {
	if spec.Mode != ports.ModeLive {
		return false
	}
	if normalizeRequestedCodec(outputCodec) != "hevc" {
		return false
	}
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4")
}

func (a *LocalAdapter) buildCopyVideoArgs(args []string, spec ports.StreamSpec, inputURL string) []string {
	liveCopy := spec.Source.Type != ports.SourceFile
	// Harden every LIVE copy, not just the force-copy allowlist: broadcast/relay
	// H.264 carries SPS/PPS sparsely (often only in the PMT / first IDR), so a
	// copied HLS segment can start with slices referencing parameter sets the
	// client has not seen yet -> a frozen/garbled opening frame until the next
	// in-band SPS/PPS arrives (observed as `non-existing PPS 0` / `no frame!`).
	// dump_extra=freq=keyframe repeats the extradata before every keyframe so
	// each segment is independently decodable; it is a no-op when the parameter
	// sets are already present. The allowlist still covers VOD/file copy.
	//
	// fMP4 is excluded: its codec config lives in the init segment (hvcC), so the
	// parameter sets are already available to every segment. Forcing them in-band
	// per keyframe contradicts the hvc1 sample entry and re-injects HEVC/HLG SEI
	// on every keyframe, which makes the HDR decoder re-initialise -> a periodic
	// flash. dump_extra is an Annex-B/MPEG-TS hardening only.
	isFMP4 := strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4")
	hardenedBitstream := (liveCopy || shouldHardenSafariCopyBitstream(spec, inputURL, a.Config)) && !isFMP4

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "copy").
		Str("video.codec", "copy").
		Bool("bitstream_hardened", hardenedBitstream).
		Msg("pipeline video: copy")

	args = append(args, "-c:v", "copy")
	// -enc_time_base:v demux forces the muxer to derive video timestamps from the
	// demuxer timebase that igndts+genpts have already cleaned. It is ONLY correct
	// while genpts is regenerating PTS. With genpts disabled we pass the source's
	// own PTS/DTS through untouched; adding demux then shifts the copied video
	// relative to the transcoded (AAC) audio, so the audio lags by a fixed amount.
	// Couple it to genpts so video and audio share one faithful source timeline.
	// planInput applies the same fallback when IngestFFlags is empty; mirror it
	// here so the coupling is correct for the default +genpts config too.
	fflags := strings.TrimSpace(a.IngestFFlags)
	if fflags == "" {
		fflags = "+genpts+discardcorrupt+flush_packets"
	}
	if liveCopy && strings.Contains(fflags, "genpts") {
		args = append(args, "-enc_time_base:v", "demux")
	}
	if hardenedBitstream {
		// Repeat H.264 extradata on keyframes so the client can recover SPS/PPS
		// at every segment boundary while staying in copy mode.
		args = append(args, "-bsf:v", "dump_extra=freq=keyframe")
	}
	return args
}

func (a *LocalAdapter) buildCPUVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	legacy := usesLegacyCPUDefaults(spec, outputCodec)

	codec := "libx264"
	preset := "ultrafast"
	crf := 20
	deinterlace := true

	if !legacy {
		if outputCodec == "hevc" {
			codec = "libx265"
		} else if outputCodec == "av1" {
			codec = "libsvtav1"
		} else if outputCodec != "" && outputCodec != "h264" {
			codec = outputCodec
		}
		if prof.Preset != "" {
			preset = prof.Preset
		} else {
			preset = "superfast"
		}
		if prof.VideoCRF > 0 {
			crf = prof.VideoCRF
		}
		deinterlace = prof.Deinterlace
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "cpu").
		Str("video.codec", codec).
		Int("video.crf", crf).
		Bool("deinterlace", deinterlace).
		Bool("legacy_defaults", legacy).
		Msg("pipeline video: cpu")

	filters := make([]string, 0, 2)
	if deinterlace {
		filters = append(filters, a.deinterlaceFilterForProfile(spec))
	}
	if prof.VideoMaxWidth > 0 {
		filters = append(filters, softwareScaleWidthFilter(prof.VideoMaxWidth))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}

	args = append(args, "-c:v", codec)
	args = append(args, "-preset", preset)
	tune := "zerolatency"
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") {
		tune = strings.TrimSpace(a.SafariDirtyX264Tune)
	}
	if tune != "" {
		args = append(args, "-tune", tune)
	}
	if prof.PlannerBound && prof.VideoTargetRateK > 0 {
		args = append(args, "-b:v", fmt.Sprintf("%dk", prof.VideoTargetRateK))
	} else {
		args = append(args, "-crf", strconv.Itoa(crf))
	}

	if !legacy && prof.VideoMaxRateK > 0 {
		args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	}

	if codec == "libx264" {
		args = append(args, "-x264-params", fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0", gop, gop))
	}
	args = appendVideoGOPArgs(args, gop, segmentSec)
	args = append(args,
		"-pix_fmt", "yuv420p",
		"-profile:v", "main",
	)
	return args
}

func softwareScaleWidthFilter(width int) string {
	return fmt.Sprintf("scale=w=%d:h=-2:flags=lanczos", width)
}

func (a *LocalAdapter) deinterlaceFilterForProfile(spec ports.StreamSpec) string {
	deinterlaceFilter := "yadif"
	if spec.Mode == ports.ModeLive && spec.Format == ports.FormatHLS && effectiveLiveRuntimeMode(spec.Profile) == ports.RuntimeModeHQ25 {
		deinterlaceFilter = "bwdif=mode=send_frame:parity=auto:deint=all"
	} else if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") && strings.TrimSpace(a.SafariDirtyFilter) != "" {
		deinterlaceFilter = strings.TrimSpace(a.SafariDirtyFilter)
	} else if spec.Mode == ports.ModeLive && spec.Format == ports.FormatHLS {
		// Generic live HLS transcodes should preserve sports motion on interlaced
		// broadcast sources instead of collapsing them to 25p.
		deinterlaceFilter = "bwdif=mode=send_field:parity=auto:deint=all"
	}
	return deinterlaceFilter
}
