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
	// 50p (HQ50) promotion is decided in FinalizePlan's adaptive_transcode_quality
	// evaluator (host-benchmark gated: weak hosts stay 25p); by the time we plan
	// the output the Profile already carries the final EffectiveRuntimeMode.
	// Use the pre-sanitisation URL so ffprobe/warmup probes can authenticate
	// against protected sources.  adjustLiveFPSForRuntimeServiceOverride only
	// extracts the service ref from the URL structure and works correctly with
	// either version.
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
	}
	gop := gopFPS * layout.segmentDurationSec

	audioSelection := a.planLiveAudioSelection(ctx, spec, probeURL)

	out := outputPlan{effectiveProfile: spec.Profile}
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
		// LL-HLS pipe mode: one fragmented MP4 stream on stdout; the cmaf
		// segmenter writes init/segments/playlist so the open segment grows
		// on disk fragment by fragment (the hls muxer buffers fMP4 segments
		// in memory and would only publish them completed).
		out.args = appendLiveCMAFStreamArgs(out.args)
		out.cmafSegment = true
		out.cmafTargetDurSec = layout.segmentDurationSec
		out.listSize = layout.listSize
	} else {
		out.args = append(out.args, "-f", "hls")
		out.args = a.appendLiveHLSArgs(out.args, spec, layout, audioSelection)
		out.args = append(out.args, a.prepareLiveOutputPath(spec.SessionID, audioSelection.IsMultiAudio))
	}

	// One source of truth: record the hwAccel the emitted argv actually reflects,
	// so the profile-derived predicted FFmpeg plan matches execution. The planner
	// can downgrade full VAAPI -> encode-only (unverified interlaced HEVC/AV1,
	// forced AV1) or fall back off the GPU; without this writeback the prediction
	// (profile.HWAccel) would diverge from the real argv, causing a spurious
	// plan_mismatch warning. VideoCodec is already synced to codec.resolvedCodec.
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
	if layout.segmentDurationSec <= 0 {
		return liveSegmentLayout{}, fmt.Errorf("invalid hls segment seconds: %d", layout.segmentDurationSec)
	}
	if a.DVRWindow > 0 {
		dvrSize := int(math.Ceil(a.DVRWindow.Seconds() / float64(layout.segmentDurationSec)))
		layout.listSize = max(dvrSize, layout.listSize, minSize)
	}
	return layout, nil
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
	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	return append(args,
		"-c:a", "aac",
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
	segmentType := "mpegts"
	sessionDir := ports.SessionHLSDir(a.HLSRoot, spec.SessionID)
	segmentFilename := filepath.Join(sessionDir, "seg_%06d.ts")
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		segmentType = "fmp4"
		if sel.IsMultiAudio {
			segmentFilename = filepath.Join(sessionDir, "seg_%v_%06d.m4s")
		} else {
			segmentFilename = filepath.Join(sessionDir, "seg_%06d.m4s")
		}
	}
	if a.inMemoryIngest && a.ingestPort > 0 {
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
	hlsFlags := "delete_segments+append_list+independent_segments+program_date_time"
	if !a.LowLatencyHLS || segmentType != "fmp4" {
		// temp_file hides the growing segment behind a .tmp rename. The LL-HLS
		// packager (internal/hls/llhls) must scan exactly that open segment to
		// cut EXT-X-PART entries, so the flag stays off on the LL path;
		// playlist consistency there is covered by the atomic playlist
		// publication guard instead.
		hlsFlags += "+temp_file"
	}
	args = append(args,
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", hlsFlags,
		"-hls_segment_type", segmentType,
	)
	if a.inMemoryIngest && a.ingestPort > 0 {
		args = append(args, "-method", "PUT")
	}
	args = append(args, "-hls_segment_filename", segmentFilename)
	if segmentType == "fmp4" {
		initFilename := "init.mp4"
		if sel.IsMultiAudio {
			initFilename = "init_%v.mp4"
		}
		if a.inMemoryIngest && a.ingestPort > 0 {
			if sel.IsMultiAudio {
				initFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/init_%%v.mp4", a.ingestPort, spec.SessionID)
			} else {
				initFilename = fmt.Sprintf("http://127.0.0.1:%d/ingest/%s/init.mp4", a.ingestPort, spec.SessionID)
			}
		}
		args = append(args, "-hls_fmp4_init_filename", initFilename)
		if a.LowLatencyHLS {
			// Fragment each segment on the part-target grid so the LL-HLS
			// packager (internal/hls/llhls) can advertise EXT-X-PART byte
			// ranges. FFmpeg 8.x leaks the first segment into init.mp4 in
			// this mode; the packager repairs that before serving.
			args = append(args, "-hls_segment_options", fmt.Sprintf("frag_duration=%d", llhlsPartTargetMs*1000))
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

func (a *LocalAdapter) prepareLiveOutputPath(sessionID string, isMultiAudio ...bool) string {
	multi := len(isMultiAudio) > 0 && isMultiAudio[0]
	filename := "index.m3u8"
	if multi {
		filename = "stream_%v.m3u8"
	}
	outputPath := filepath.Join(ports.SessionHLSDir(a.HLSRoot, sessionID), filename)
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	if markerPath := ports.SessionFirstFrameMarkerPath(a.HLSRoot, sessionID); markerPath != "" {
		_ = os.Remove(markerPath)
	}
	if a.inMemoryIngest && a.ingestPort > 0 {
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
