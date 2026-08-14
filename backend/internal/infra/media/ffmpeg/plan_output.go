package ffmpeg

import (
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *LocalAdapter) planLiveOutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan) (outputPlan, error) {
	layout, err := a.planLiveSegmentLayout(spec)
	if err != nil {
		return outputPlan{}, err
	}
	spec.Profile.VideoCodec = codec.resolvedCodec

	probeURL := input.authURL
	if probeURL == "" {
		probeURL = input.inputURL
	}
	fps := a.resolveLiveFPS(ctx, spec, probeURL)
	fps = a.adjustLiveFPSForRuntimeServiceOverride(spec, input.inputURL, fps)
	targetOutputFPS := targetLiveOutputFPS(spec)
	gopFPS := fps
	if targetOutputFPS > 0 {
		gopFPS = targetOutputFPS
	} else if effectiveLiveRuntimeMode(spec.Profile) == ports.RuntimeModeHQ50 {
		gopFPS = 50
	}
	gop := gopFPS * layout.segmentDurationSec

	// Route to Source-Aware ABR Output Planners when EnableABR is active
	if spec.Profile.EnableABR && spec.Profile.TranscodeVideo {
		if codec.useHW && codec.hwBackend == profiles.GPUBackendVAAPI {
			return a.planLiveVAAPIABROutput(ctx, spec, input, codec, layout, gop, targetOutputFPS)
		}
		if !codec.useHW || codec.hwBackend == "" || codec.hwBackend == profiles.GPUBackendNone {
			return a.planLiveABROutput(ctx, spec, input, codec, layout, gop, targetOutputFPS)
		}
	}

	audioSelection := a.planLiveAudioSelection(ctx, spec, probeURL)

	out := outputPlan{
		effectiveProfile: spec.Profile,
		primaryPlaylist:  "index.m3u8",
	}
	out.args = append(out.args, "-map", "0:v:0?")
	for _, m := range audioSelection.Maps {
		out.args = append(out.args, "-map", m)
	}
	if targetOutputFPS > 0 {
		out.args = append(out.args, "-r", strconv.Itoa(targetOutputFPS))
	}

	out.args = a.buildLiveVideoOutputArgs(out.args, spec, input.inputURL, codec, gop, layout.segmentDurationSec)
	out.args = appendLiveVideoContainerTags(out.args, spec, codec.resolvedCodec)
	out.args = append(out.args, audioSelection.AudioArgs...)
	if a.useCMAFSegmenter(spec) {
		out.args = appendLiveCMAFStreamArgs(out.args)
		out.cmafSegment = true
		out.cmafTargetDurSec = layout.segmentDurationSec
		out.listSize = layout.listSize
	} else {
		out.args = append(out.args, "-f", "hls")
		out.args = a.appendLiveHLSArgs(out.args, spec, layout, audioSelection)
		out.args = append(out.args, a.prepareLiveOutputPath(spec.SessionID, spec.Profile.DVRWindowSec, audioSelection.IsMultiAudio))
	}

	out.effectiveProfile.HWAccel = resolvedExecutedHWAccel(codec)

	return out, nil
}

// planLiveABROutput constructs a 3-tier or 2-tier Source-Aware CPU/libx264 ABR pipeline.
func (a *LocalAdapter) planLiveABROutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan, layout liveSegmentLayout, gop int, targetOutputFPS int) (outputPlan, error) {
	out := outputPlan{
		effectiveProfile: spec.Profile,
		primaryPlaylist:  "master.m3u8",
	}

	// Source-Aware Ladder Selection:
	// - SourceHeight > 720 (1080i / 1080p): 3-Tier Ladder (1080p / 720p / 480p)
	// - SourceHeight <= 720 or 0 (unspecified): 2-Tier Ladder (720p / 480p) - Zero fake 1080p upscaling!
	sourceHeight := spec.Profile.VideoSourceHeight
	is3Tier := sourceHeight > 720

	var filterComplex string
	var varStreamMap string
	if is3Tier {
		filterComplex = "[0:v:0]split=3[v1080in][v720in][v480in]; [v1080in]null[v1080]; [v720in]scale=1280:720[v720]; [v480in]scale=854:480[v480]; [0:a:0]asplit=3[a1080][a720][a480]"
		varStreamMap = "v:0,a:0,name:1080p v:1,a:1,name:720p v:2,a:2,name:480p"
	} else {
		filterComplex = "[0:v:0]split=2[v720in][v480in]; [v720in]null[v720]; [v480in]scale=854:480[v480]; [0:a:0]asplit=2[a720][a480]"
		varStreamMap = "v:0,a:0,name:720p v:1,a:1,name:480p"
	}

	out.args = append(out.args, "-filter_complex", filterComplex)

	if targetOutputFPS > 0 {
		out.args = append(out.args, "-r", strconv.Itoa(targetOutputFPS))
	}

	if is3Tier {
		out.args = append(out.args,
			"-map", "[v1080]",
			"-c:v:0", "libx264",
			"-b:v:0", "4500k", "-maxrate:v:0", "5200k", "-bufsize:v:0", "9000k",

			"-map", "[v720]",
			"-c:v:1", "libx264",
			"-b:v:1", "2000k", "-maxrate:v:1", "2400k", "-bufsize:v:1", "4000k",

			"-map", "[v480]",
			"-c:v:2", "libx264",
			"-b:v:2", "900k", "-maxrate:v:2", "1100k", "-bufsize:v:2", "2000k",

			"-map", "[a1080]",
			"-c:a:0", "aac",
			"-b:a:0", "128k", "-ac:a:0", "2", "-ar:a:0", "48000",

			"-map", "[a720]",
			"-c:a:1", "aac",
			"-b:a:1", "128k", "-ac:a:1", "2", "-ar:a:1", "48000",

			"-map", "[a480]",
			"-c:a:2", "aac",
			"-b:a:2", "128k", "-ac:a:2", "2", "-ar:a:2", "48000",
		)
	} else {
		out.args = append(out.args,
			"-map", "[v720]",
			"-c:v:0", "libx264",
			"-b:v:0", "2000k", "-maxrate:v:0", "2400k", "-bufsize:v:0", "4000k",

			"-map", "[v480]",
			"-c:v:1", "libx264",
			"-b:v:1", "900k", "-maxrate:v:1", "1100k", "-bufsize:v:1", "2000k",

			"-map", "[a720]",
			"-c:a:0", "aac",
			"-b:a:0", "128k", "-ac:a:0", "2", "-ar:a:0", "48000",

			"-map", "[a480]",
			"-c:a:1", "aac",
			"-b:a:1", "128k", "-ac:a:1", "2", "-ar:a:1", "48000",
		)
	}

	// Dynamic GOP and synchronized Keyframe interval calculation
	forceKeyExpr := fmt.Sprintf("expr:gte(t,n_forced*%d)", layout.segmentDurationSec)
	out.args = append(out.args,
		"-g", strconv.Itoa(gop),
		"-keyint_min", strconv.Itoa(gop),
		"-force_key_frames", forceKeyExpr,
		"-preset", "ultrafast",
		"-sc_threshold", "0",
	)

	sessionDir := ports.SessionHLSDirForPolicy(a.HLSRoot, spec.SessionID, spec.Profile.DVRWindowSec)
	_ = os.MkdirAll(sessionDir, 0750)
	varPath := filepath.Join(sessionDir, "%v", "index.m3u8")

	out.args = append(out.args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", "delete_segments+independent_segments",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", varStreamMap,
		varPath,
	)

	return out, nil
}

// planLiveVAAPIABROutput constructs a 3-tier or 2-tier Source-Aware Intel VAAPI Hardware ABR pipeline.
func (a *LocalAdapter) planLiveVAAPIABROutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan, layout liveSegmentLayout, gop int, targetOutputFPS int) (outputPlan, error) {
	out := outputPlan{
		effectiveProfile: spec.Profile,
		primaryPlaylist:  "master.m3u8",
	}

	sourceHeight := spec.Profile.VideoSourceHeight
	is3Tier := sourceHeight > 720

	out.args = append(out.args, codec.preInputArgs...)

	gpuHead := "[0:v:0]format=nv12,hwupload"
	if codec.fullVAAPI {
		gpuHead = "[0:v:0]null"
	}
	if spec.Profile.Deinterlace {
		gpuHead += "," + vaapiDeinterlaceFilter(spec)
	}
	gpuHead += "[v_gpu]"

	var filterComplex string
	var varStreamMap string
	if is3Tier {
		filterComplex = gpuHead + "; [v_gpu]split=3[v1080][v720in][v480in]; [v720in]scale_vaapi=w=1280:h=720[v720]; [v480in]scale_vaapi=w=854:h=480[v480]; [0:a:0]asplit=3[a1080][a720][a480]"
		varStreamMap = "v:0,a:0,name:1080p v:1,a:1,name:720p v:2,a:2,name:480p"
	} else {
		filterComplex = gpuHead + "; [v_gpu]split=2[v720in][v480in]; [v720in]scale_vaapi=w=1280:h=720[v720]; [v480in]scale_vaapi=w=854:h=480[v480]; [0:a:0]asplit=2[a720][a480]"
		varStreamMap = "v:0,a:0,name:720p v:1,a:1,name:480p"
	}

	out.args = append(out.args, "-filter_complex", filterComplex)

	if targetOutputFPS > 0 {
		out.args = append(out.args, "-r", strconv.Itoa(targetOutputFPS))
	}

	vaapiEncoder := vaapiEncoderForCodec(codec.resolvedCodec)

	if is3Tier {
		out.args = append(out.args,
			"-map", "[v1080]",
			"-c:v:0", vaapiEncoder,
			"-b:v:0", "4500k", "-maxrate:v:0", "5200k", "-bufsize:v:0", "9000k",

			"-map", "[v720]",
			"-c:v:1", vaapiEncoder,
			"-b:v:1", "2000k", "-maxrate:v:1", "2400k", "-bufsize:v:1", "4000k",

			"-map", "[v480]",
			"-c:v:2", vaapiEncoder,
			"-b:v:2", "900k", "-maxrate:v:2", "1100k", "-bufsize:v:2", "2000k",

			"-map", "[a1080]",
			"-c:a:0", "aac",
			"-b:a:0", "128k", "-ac:a:0", "2", "-ar:a:0", "48000",

			"-map", "[a720]",
			"-c:a:1", "aac",
			"-b:a:1", "128k", "-ac:a:1", "2", "-ar:a:1", "48000",

			"-map", "[a480]",
			"-c:a:2", "aac",
			"-b:a:2", "128k", "-ac:a:2", "2", "-ar:a:2", "48000",
		)
	} else {
		out.args = append(out.args,
			"-map", "[v720]",
			"-c:v:0", vaapiEncoder,
			"-b:v:0", "2000k", "-maxrate:v:0", "2400k", "-bufsize:v:0", "4000k",

			"-map", "[v480]",
			"-c:v:1", vaapiEncoder,
			"-b:v:1", "900k", "-maxrate:v:1", "1100k", "-bufsize:v:1", "2000k",

			"-map", "[a720]",
			"-c:a:0", "aac",
			"-b:a:0", "128k", "-ac:a:0", "2", "-ar:a:0", "48000",

			"-map", "[a480]",
			"-c:a:1", "aac",
			"-b:a:1", "128k", "-ac:a:1", "2", "-ar:a:1", "48000",
		)
	}

	forceKeyExpr := fmt.Sprintf("expr:gte(t,n_forced*%d)", layout.segmentDurationSec)
	out.args = append(out.args,
		"-g", strconv.Itoa(gop),
		"-keyint_min", strconv.Itoa(gop),
		"-force_key_frames", forceKeyExpr,
	)

	sessionDir := ports.SessionHLSDirForPolicy(a.HLSRoot, spec.SessionID, spec.Profile.DVRWindowSec)
	_ = os.MkdirAll(sessionDir, 0750)
	varPath := filepath.Join(sessionDir, "%v", "index.m3u8")

	out.args = append(out.args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", "delete_segments+independent_segments",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", varStreamMap,
		varPath,
	)

	out.effectiveProfile.HWAccel = resolvedExecutedHWAccel(codec)
	return out, nil
}

func (a *LocalAdapter) planLiveSegmentLayout(spec ports.StreamSpec) (liveSegmentLayout, error) {
	readySegs := a.ReadySegments
	if readySegs <= 0 {
		readySegs = config.DefaultHLSReadySegments
	}
	minSize := readySegs + 2
	if minSize < 4 {
		minSize = 4 // HLS safety floor for sliding windows
	}
	layout := liveSegmentLayout{
		segmentDurationSec: a.SegmentSeconds,
		listSize:           30, // enforced minimum to prevent stuttering during network retries
	}
	if a.LowLatencyHLS && strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") && layout.segmentDurationSec > llhlsSegmentSeconds {
		// LL-HLS: short segments keep the playlist window tight; parts are
		// cut inside them via frag_duration (see appendLiveHLSArgs). GOP
		// derives from the segment duration above, so keyframes stay
		// aligned with segment boundaries.
		layout.segmentDurationSec = llhlsSegmentSeconds
	}
	if shouldUseShortFMP4StartupSegments(spec) && layout.segmentDurationSec > safariDirtyHLSTimeSec {
		layout.segmentDurationSec = safariDirtyHLSTimeSec
		layout.initSegmentDurationSec = min(safariDirtyHLSInitTimeSec, layout.segmentDurationSec)
	}
	if isAndroidTVNativeSpec(spec) && spec.Mode == ports.ModeLive && spec.Profile.TranscodeVideo &&
		strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") && layout.segmentDurationSec > 1 {
		// A one-second keyframe cadence publishes the first complete native-app
		// segment sooner. Copy streams retain their source GOP cadence, while web
		// clients retain their existing segment layout.
		layout.segmentDurationSec = 1
	}
	if layout.segmentDurationSec <= 0 {
		return liveSegmentLayout{}, fmt.Errorf("invalid hls segment seconds: %d", layout.segmentDurationSec)
	}
	if a.DVRWindow > 0 {
		dvrSize := int(math.Ceil(a.DVRWindow.Seconds() / float64(layout.segmentDurationSec)))
		layout.listSize = max(dvrSize, layout.listSize, minSize)
	}
	return layout, nil
}

func isAndroidTVNativeSpec(spec ports.StreamSpec) bool {
	return strings.EqualFold(strings.TrimSpace(spec.ClientFamily), "android_tv_native")
}

func shouldUseShortFMP4StartupSegments(spec ports.StreamSpec) bool {
	if spec.Mode != ports.ModeLive || !spec.Profile.TranscodeVideo {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return false
	}

	switch profiles.NormalizeRequestedProfileID(spec.Profile.Name) {
	case profiles.ProfileSafariDirty,
		profiles.ProfileSafariHEVCHW,
		profiles.ProfileSafariHEVCHWLL,
		profiles.ProfileAV1HW:
		// Native iOS fMP4 transcodes benefit from a denser startup GOP cadence.
		// The default 6-second layout makes first attach visibly sluggish.
		return true
	default:
		return false
	}
}

func (a *LocalAdapter) buildLiveVideoOutputArgs(args []string, spec ports.StreamSpec, inputURL string, codec codecPlan, gop, segmentDurationSec int) []string {
	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, codec.resolvedCodec) {
		return a.buildCopyVideoArgs(args, spec, inputURL)
	}
	if codec.useHW {
		switch codec.hwBackend {
		case profiles.GPUBackendVAAPI:
			if codec.fullVAAPI {
				return a.buildVaapiVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
			}
			return a.buildVaapiEncodeOnlyVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		case profiles.GPUBackendNVENC:
			return a.buildNVENCVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		}
	}
	return a.buildCPUVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
}

func appendLiveAudioArgs(args []string, spec ports.StreamSpec, channels int) []string {
	if !spec.Profile.TranscodesAudio() {
		return append(args, "-c:a", "copy", "-sn")
	}
	audioCodec := spec.Profile.ResolvedAudioCodec()
	audioBitrate := "320k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	return append(args,
		"-c:a", audioCodec,
		"-b:a", audioBitrate,
		"-ac", "2",
		"-ar", "48000",
		"-sn",
	)
}

// useCMAFSegmenter reports whether this session runs in LL-HLS pipe mode.
// Restricted to transcode so the 2s IDR cadence is under our control;
// copy sources with unknown GOPs fall back to the hls muxer (the HasParts
// gate then keeps the playlist plain).
func (a *LocalAdapter) useCMAFSegmenter(spec ports.StreamSpec) bool {
	return false
}

// appendLiveCMAFStreamArgs emits a single fragmented-MP4 stream on stdout:
// frag_keyframe guarantees every 2s IDR starts a fragment (clean segment
// rotation points), frag_duration cuts ~500ms parts between them, and
// flush_packets defeats the 32KB AVIO buffer so fragments reach the
// segmenter with encode latency instead of buffer-fill latency.
func appendLiveCMAFStreamArgs(args []string) []string {
	return append(args,
		"-f", "mp4",
		"-movflags", "empty_moov+default_base_moof+skip_trailer+frag_keyframe+delay_moov",
		"-frag_duration", strconv.Itoa(llhlsPartTargetMs*1000),
		"-flush_packets", "1",
		"pipe:1",
	)
}

func appendLiveVideoContainerTags(args []string, spec ports.StreamSpec, outputCodec string) []string {
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return args
	}
	if !strings.EqualFold(strings.TrimSpace(outputCodec), "hevc") {
		return args
	}
	return append(args, "-tag:v", "hvc1")
}

// LL-HLS layout: 2s segments fragmented into 500ms parts. Mirrored by the
// playlist packager in internal/hls/llhls.
const (
	llhlsSegmentSeconds = 2
	llhlsPartTargetMs   = 500
)

func (a *LocalAdapter) appendLiveHLSArgs(args []string, spec ports.StreamSpec, layout liveSegmentLayout, audioSel ...liveAudioSelection) []string {
	var sel liveAudioSelection
	if len(audioSel) > 0 {
		sel = audioSel[0]
	}
	// TODO(SPEC_MODERNIZATION_2026 §R3): TS segment packaging path is superseded by R3 CMAF/fMP4 delivery.
	segmentType := "mpegts"
	sessionDir := ports.SessionHLSDirForPolicy(a.HLSRoot, spec.SessionID, spec.Profile.DVRWindowSec)
	segmentFilename := filepath.Join(sessionDir, "seg_%06d.ts")
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		segmentType = "fmp4"
		if sel.IsMultiAudio {
			segmentFilename = filepath.Join(sessionDir, "seg_%v_%06d.m4s")
		} else {
			segmentFilename = filepath.Join(sessionDir, "seg_%06d.m4s")
		}
	}
	if a.inMemoryIngest && a.ingestPort > 0 && !a.LowLatencyHLS {
		if segmentType == "fmp4" {
			if sel.IsMultiAudio {
				segmentFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/seg_%%v_%%06d.m4s", a.ingestPort, spec.SessionID)
			} else {
				segmentFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/seg_%%06d.m4s", a.ingestPort, spec.SessionID)
			}
		} else {
			segmentFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/seg_%%06d.ts", a.ingestPort, spec.SessionID)
		}
	}
	hlsFlags := "delete_segments+append_list+program_date_time"
	if spec.Profile.TranscodeVideo {
		hlsFlags += "+independent_segments"
	}
	// Always use temp_file to ensure atomic segment creation,
	// since the internal LL-HLS packager is currently disabled.
	// This prevents Safari from downloading partially written segments.
	hlsFlags += "+temp_file"

	args = append(args,
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", hlsFlags,
		"-hls_segment_type", segmentType,
	)
	if a.inMemoryIngest && a.ingestPort > 0 && !a.LowLatencyHLS {
		args = append(args, "-method", "PUT")
	}
	args = append(args, "-hls_segment_filename", segmentFilename)
	if segmentType == "fmp4" {
		initFilename := "init.mp4"
		if sel.IsMultiAudio {
			initFilename = "init_%v.mp4"
		}
		if a.inMemoryIngest && a.ingestPort > 0 && !a.LowLatencyHLS {
			if sel.IsMultiAudio {
				initFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/init_%%v.mp4", a.ingestPort, spec.SessionID)
			} else {
				initFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/init.mp4", a.ingestPort, spec.SessionID)
			}
		}
		args = append(args, "-hls_fmp4_init_filename", initFilename)
		segmentOptions := make([]string, 0, 2)
		if !spec.Profile.TranscodeVideo {
			// Copied DVB H.264 may start an fMP4 fragment with reordered B-frames.
			// Version-1 CTTS preserves their negative composition offsets instead of
			// clamping them to a duplicate PTS at every segment boundary.
			segmentOptions = append(segmentOptions, "movflags=+negative_cts_offsets")
		}
		if a.LowLatencyHLS {
			// Fragment each segment on the part-target grid so the LL-HLS
			// packager (internal/hls/llhls) can advertise EXT-X-PART byte
			// ranges. FFmpeg 8.x leaks the first segment into init.mp4 in
			// this mode; the packager repairs that before serving.
			segmentOptions = append(segmentOptions, fmt.Sprintf("frag_duration=%d", llhlsPartTargetMs*1000))
		}
		if len(segmentOptions) > 0 {
			args = append(args, "-hls_segment_options", strings.Join(segmentOptions, ":"))
		}
	}
	if layout.initSegmentDurationSec > 0 {
		args = append(args, "-hls_init_time", strconv.Itoa(layout.initSegmentDurationSec))
	}
	if sel.IsMultiAudio && strings.TrimSpace(sel.VarStreamMap) != "" {
		args = append(args,
			"-master_pl_name", "index.m3u8",
			"-var_stream_map", sel.VarStreamMap,
		)
	}
	return args
}

func (a *LocalAdapter) prepareLiveOutputPath(sessionID string, dvrWindowSec int, isMultiAudio ...bool) string {
	multi := len(isMultiAudio) > 0 && isMultiAudio[0]
	filename := "index.m3u8"
	if multi {
		filename = "stream_%v.m3u8"
	}
	outputPath := filepath.Join(ports.SessionHLSDirForPolicy(a.HLSRoot, sessionID, dvrWindowSec), filename)
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	if markerPath := ports.SessionFirstFrameMarkerPath(a.HLSRoot, sessionID); markerPath != "" {
		_ = os.Remove(markerPath)
	}
	if a.inMemoryIngest && a.ingestPort > 0 && !a.LowLatencyHLS {
		if multi {
			outputPath = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/stream_%%v.m3u8", a.ingestPort, sessionID)
		} else {
			outputPath = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/index.m3u8", a.ingestPort, sessionID)
		}
	}
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "output_dir_ready").
		Str("output_path", outputPath).
		Bool("is_multi_audio", multi).
		Msg("output directory ready")
	return outputPath
}
