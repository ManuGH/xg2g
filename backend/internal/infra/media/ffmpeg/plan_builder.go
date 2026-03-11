package ffmpeg

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	codecdecision "github.com/ManuGH/xg2g/internal/decision"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	infraffmpeg "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type inputPlan struct {
	args     []string
	inputURL string
}

type codecPlan struct {
	resolvedCodec string
	useVAAPI      bool
	fullVAAPI     bool
	preInputArgs  []string
}

type outputPlan struct {
	args []string
}

func (a *LocalAdapter) buildArgs(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]string, error) {
	codecPhase, err := a.planCodec(spec)
	if err != nil {
		return nil, err
	}

	inputPhase, err := a.planInput(spec, inputURL)
	if err != nil {
		return nil, err
	}

	args := append([]string{}, codecPhase.preInputArgs...)
	args = append(args, inputPhase.args...)
	args = append(args, "-progress", "pipe:2")

	if spec.Mode == ports.ModeLive {
		liveOutput, err := a.planLiveOutput(ctx, spec, inputPhase, codecPhase)
		if err != nil {
			return nil, err
		}
		args = append(args, liveOutput.args...)
	}

	return args, nil
}

func (a *LocalAdapter) planCodec(spec ports.StreamSpec) (codecPlan, error) {
	useHWPath := profiles.IsGPUBackedProfile(spec.Profile.HWAccel)
	if isPreferHWProfile(spec.Profile.Name) {
		useHWPath = true
	}

	hardVAAPIRequest := profiles.IsGPUBackedProfile(spec.Profile.HWAccel) && !isPreferHWProfile(spec.Profile.Name)
	decisionIn := codecdecision.Input{
		Profile:        spec.Profile.Name,
		RequestedCodec: spec.Profile.VideoCodec,
		RequireHW:      hardVAAPIRequest,
		Server: codecdecision.ServerCapabilities{
			HWAccelAvailable:  useHWPath,
			SupportedHWCodecs: a.supportedHWCodecs(),
		},
	}
	neg := codecdecision.Decide(decisionIn)
	decisionInSummary := decisionIn.Summary()
	decisionOutSummary := neg.Summary()
	metrics.RecordDecisionSummary(
		decisionInSummary.Profile,
		decisionOutSummary.Path,
		decisionOutSummary.OutputCodec,
		decisionOutSummary.UseHWAccel,
		decisionOutSummary.Reason,
	)

	a.Logger.Info().
		Str("event", "decision.summary").
		Str("profile", decisionInSummary.Profile).
		Str("requested_codec", decisionInSummary.RequestedCodec).
		Strs("supported_hw_codecs", decisionInSummary.SupportedHWCodecs).
		Bool("hwaccel_available", decisionInSummary.HWAccelAvailable).
		Str("path", decisionOutSummary.Path).
		Str("output_codec", decisionOutSummary.OutputCodec).
		Bool("use_hwaccel", decisionOutSummary.UseHWAccel).
		Str("reason", decisionOutSummary.Reason).
		Msg("decision summary")

	if neg.Path == codecdecision.PathReject && !hardVAAPIRequest {
		return codecPlan{}, fmt.Errorf("codec negotiation rejected (profile=%s codec=%s reason=%s)", spec.Profile.Name, spec.Profile.VideoCodec, neg.Reason)
	}

	resolvedCodec := neg.OutputCodec
	if resolvedCodec == "" && hardVAAPIRequest {
		resolvedCodec = normalizeRequestedCodec(spec.Profile.VideoCodec)
	}
	if resolvedCodec == "" {
		resolvedCodec = "h264"
	}

	useVAAPI := neg.Path == codecdecision.PathTranscodeHW
	if hardVAAPIRequest {
		useVAAPI = true
	}
	fullVAAPI := profiles.IsFullVAAPIProfile(spec.Profile.HWAccel)

	preInputArgs := make([]string, 0, 6)
	if useVAAPI {
		if a.VaapiDevice == "" {
			return codecPlan{}, fmt.Errorf("vaapi requested by profile but no vaapi device configured on adapter")
		}
		reqEncoder, ok := codecToVAAPIEncoder(resolvedCodec)
		if !ok {
			return codecPlan{}, fmt.Errorf("unsupported vaapi codec resolved by decision engine: %s", resolvedCodec)
		}
		if !a.VaapiEncoderVerified(reqEncoder) {
			return codecPlan{}, fmt.Errorf("vaapi encoder %s not verified by preflight (device=%s, deviceErr=%v)", reqEncoder, a.VaapiDevice, a.vaapiDeviceErr)
		}
		preInputArgs = append(preInputArgs, "-vaapi_device", a.VaapiDevice)
		if fullVAAPI {
			preInputArgs = append(preInputArgs,
				"-hwaccel", "vaapi",
				"-hwaccel_output_format", "vaapi",
			)
		}
	}

	return codecPlan{
		resolvedCodec: resolvedCodec,
		useVAAPI:      useVAAPI,
		fullVAAPI:     fullVAAPI,
		preInputArgs:  preInputArgs,
	}, nil
}

func (a *LocalAdapter) planInput(spec ports.StreamSpec, inputURL string) (inputPlan, error) {
	fflags := strings.TrimSpace(a.IngestFFlags)
	if fflags == "" {
		fflags = "+genpts+discardcorrupt+flush_packets"
	}
	analyzeDuration := strings.TrimSpace(a.AnalyzeDuration)
	probeSize := strings.TrimSpace(a.ProbeSize)
	baseInputArgs := make([]string, 0, 20)
	if v := strings.TrimSpace(a.IngestErrDetect); v != "" {
		baseInputArgs = append(baseInputArgs, "-err_detect", v)
	}
	if v := strings.TrimSpace(a.IngestMaxErrorRate); v != "" {
		baseInputArgs = append(baseInputArgs, "-max_error_rate", v)
	}
	baseInputArgs = append(baseInputArgs, "-ignore_unknown")
	if spec.Source.Type != ports.SourceFile {
		if liveAnalyze := strings.TrimSpace(a.LiveAnalyzeDuration); liveAnalyze != "" {
			analyzeDuration = liveAnalyze
		}
		if liveProbe := strings.TrimSpace(a.LiveProbeSize); liveProbe != "" {
			probeSize = liveProbe
		}
		if !strings.Contains(fflags, "igndts") {
			fflags += "+igndts"
		}
		if a.LiveNoBuffer && !strings.Contains(fflags, "nobuffer") {
			fflags += "+nobuffer"
		}

		headers := "Icy-MetaData: 1\r\n"
		if u, err := url.Parse(inputURL); err == nil && u.User != nil {
			if pwd, ok := u.User.Password(); ok {
				auth := u.User.Username() + ":" + pwd
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			} else {
				auth := u.User.Username() + ":"
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			}
		}

		baseInputArgs = append(baseInputArgs,
			"-avoid_negative_ts", "make_zero",
			"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",
			"-headers", headers,
		)
		if v := strings.TrimSpace(a.IngestFlags2); v != "" {
			baseInputArgs = append(baseInputArgs, "-flags2", v)
		}
	}
	baseInputArgs = append([]string{"-fflags", fflags}, baseInputArgs...)
	if analyzeDuration != "" {
		baseInputArgs = append(baseInputArgs, "-analyzeduration", analyzeDuration)
	}
	if probeSize != "" {
		baseInputArgs = append(baseInputArgs, "-probesize", probeSize)
	}

	netInputArgs := append([]string{}, baseInputArgs...)
	netInputArgs = append(netInputArgs,
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
	)

	phase := inputPlan{inputURL: inputURL}
	switch spec.Source.Type {
	case ports.SourceTuner:
		if phase.inputURL == "" {
			return inputPlan{}, fmt.Errorf("missing stream url for tuner source")
		}
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceURL:
		if phase.inputURL == "" {
			phase.inputURL = spec.Source.ID
		}
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceFile:
		phase.args = append(phase.args, baseInputArgs...)
		phase.args = append(phase.args, "-re", "-i", spec.Source.ID)
	default:
		return inputPlan{}, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}

	return phase, nil
}

func (a *LocalAdapter) planLiveOutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan) (outputPlan, error) {
	segmentDurationSec := a.SegmentSeconds
	initSegmentDurationSec := 0
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") && segmentDurationSec > safariDirtyHLSTimeSec {
		segmentDurationSec = safariDirtyHLSTimeSec
		initSegmentDurationSec = safariDirtyHLSInitTimeSec
		if initSegmentDurationSec > segmentDurationSec {
			initSegmentDurationSec = segmentDurationSec
		}
	}
	if segmentDurationSec <= 0 {
		return outputPlan{}, fmt.Errorf("invalid hls segment seconds: %d", segmentDurationSec)
	}
	fps := a.FPSFallback
	if fps <= 0 {
		fps = 25
	}
	if (spec.Source.Type != ports.SourceTuner && !isStreamRelayURL(input.inputURL)) && !spec.Profile.Deinterlace {
		fps = 30
	}
	if spec.Profile.Deinterlace || strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") {
		fps = a.FPSFallbackInter
	}
	if fps <= 0 {
		fps = 25
	}

	sourceKey := fpsCacheKey(spec.Source, input.inputURL)
	skipProbe := false
	if isStreamRelayURL(input.inputURL) {
		skipProbe = true
		logEvt := a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "fps_probe_skipped_streamrelay").
			Str("source_key", sourceKey).
			Str("input_url", sanitizeURLForLog(input.inputURL))
		if sourceKey != "" {
			if cachedFPS, ok := a.getLastKnownFPS(sourceKey); ok && cachedFPS >= a.FPSMin && cachedFPS <= a.FPSMax {
				fps = cachedFPS
				logEvt.Int("cached_fps", cachedFPS).
					Msg("skipping fps probe for stream relay input; using cached fps")
			} else {
				logEvt.Int("fallback_fps", fps).
					Msg("skipping fps probe for stream relay input; using fallback fps")
			}
		} else {
			logEvt.Int("fallback_fps", fps).
				Msg("skipping fps probe for stream relay input; using fallback fps")
		}
	}
	if !skipProbe && sourceKey != "" {
		if cachedFPS, ok := a.getLastKnownFPS(sourceKey); ok && cachedFPS >= a.FPSMin && cachedFPS <= a.FPSMax {
			a.Logger.Info().
				Str("session_id", spec.SessionID).
				Str("startup_phase", "fps_cache_available").
				Str("source_key", sourceKey).
				Int("cached_fps", cachedFPS).
				Msg("fps cache available before probe")
			if a.SkipFPSProbeOnCache {
				warmupSucceeded := true
				if a.SkipFPSProbeWarmup > 0 && isHTTPInputURL(input.inputURL) {
					a.Logger.Info().
						Str("session_id", spec.SessionID).
						Str("startup_phase", "fps_probe_warmup_started").
						Str("source_key", sourceKey).
						Str("input_url", sanitizeURLForLog(input.inputURL)).
						Dur("warmup_duration", a.SkipFPSProbeWarmup).
						Msg("starting cache-hit stream warmup")
					warmupResult, warmupErr := a.warmupInputStream(ctx, input.inputURL, a.SkipFPSProbeWarmup)
					warmupEvt := a.Logger.Info().
						Str("session_id", spec.SessionID).
						Str("startup_phase", "fps_probe_warmup_finished").
						Str("source_key", sourceKey).
						Str("input_url", sanitizeURLForLog(input.inputURL)).
						Int("warmup_bytes", warmupResult.bytes).
						Int("http_status", warmupResult.httpStatus).
						Int64("latency_ms", warmupResult.latencyMs)
					if warmupErr != nil {
						warmupEvt = warmupEvt.Err(warmupErr)
						warmupSucceeded = false
					}
					warmupEvt.Msg("cache-hit stream warmup finished")
				}
				if warmupSucceeded {
					fps = cachedFPS
					skipProbe = true
					a.Logger.Info().
						Str("session_id", spec.SessionID).
						Str("startup_phase", "fps_probe_skipped").
						Str("source_key", sourceKey).
						Int("cached_fps", cachedFPS).
						Msg("skipping fps probe because cached fps is available")
				} else {
					a.Logger.Warn().
						Str("session_id", spec.SessionID).
						Str("startup_phase", "fps_probe_skip_aborted").
						Str("source_key", sourceKey).
						Int("cached_fps", cachedFPS).
						Msg("cache-hit stream warmup failed, falling back to fps probe")
				}
			}
		}
	}
	if !skipProbe {
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "fps_probe_started").
			Str("source_key", sourceKey).
			Str("input_url", sanitizeURLForLog(input.inputURL)).
			Msg("fps probe started")
		detected, basis, err := a.probeFPS(ctx, input.inputURL)
		probeEvt := a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "fps_probe_finished").
			Str("source_key", sourceKey).
			Str("input_url", sanitizeURLForLog(input.inputURL))
		if err != nil {
			probeEvt = probeEvt.Err(err)
		} else {
			probeEvt = probeEvt.Int("detected_fps", detected).Str("fps_basis", basis)
		}
		probeEvt.Msg("fps probe finished")
		if err == nil && detected >= a.FPSMin && detected <= a.FPSMax {
			fps = detected
			if sourceKey != "" {
				a.setLastKnownFPS(sourceKey, detected)
			}
			a.Logger.Debug().
				Str("sessionId", spec.SessionID).
				Int("fps", fps).
				Str("fps_basis", basis).
				Str("url", sanitizeURLForLog(input.inputURL)).
				Msg("detected input fps")
		} else {
			if err == nil {
				err = fmt.Errorf("detected fps out of range: %d", detected)
			}
			if sourceKey != "" {
				if cachedFPS, ok := a.getLastKnownFPS(sourceKey); ok && cachedFPS >= a.FPSMin && cachedFPS <= a.FPSMax {
					fps = cachedFPS
					a.Logger.Warn().
						Str("sessionId", spec.SessionID).
						Err(err).
						Int("cached_fps", cachedFPS).
						Str("fps_basis", "last_known_source").
						Str("source_key", sourceKey).
						Int("fps_min", a.FPSMin).
						Int("fps_max", a.FPSMax).
						Str("url", sanitizeURLForLog(input.inputURL)).
						Msg("fps detection failed, using last known source fps")
				} else {
					a.Logger.Warn().
						Str("sessionId", spec.SessionID).
						Err(err).
						Int("fallback_fps", fps).
						Int("fps_min", a.FPSMin).
						Int("fps_max", a.FPSMax).
						Str("url", sanitizeURLForLog(input.inputURL)).
						Msg("fps detection failed, using fallback")
				}
			} else {
				a.Logger.Warn().
					Str("sessionId", spec.SessionID).
					Err(err).
					Int("fallback_fps", fps).
					Int("fps_min", a.FPSMin).
					Int("fps_max", a.FPSMax).
					Str("url", sanitizeURLForLog(input.inputURL)).
					Msg("fps detection failed, using fallback")
			}
		}
	}

	gop := fps * segmentDurationSec
	listSize := 10
	if a.DVRWindow > 0 {
		listSize = int(math.Ceil(a.DVRWindow.Seconds() / float64(segmentDurationSec)))
		if listSize < 3 {
			listSize = 3
		}
	}

	out := outputPlan{}
	out.args = append(out.args,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
	)

	if a.shouldPreferSafariRuntimeRemux(ctx, spec, input.inputURL) {
		spec.Profile.TranscodeVideo = false
		spec.Profile.Deinterlace = false
		spec.Profile.Container = "fmp4"
		if spec.Profile.AudioBitrateK <= 0 {
			spec.Profile.AudioBitrateK = 192
		}
	}

	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, codec.resolvedCodec) {
		out.args = a.buildCopyVideoArgs(out.args, spec)
	} else if codec.useVAAPI {
		if codec.fullVAAPI {
			out.args = a.buildVaapiVideoArgs(out.args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		} else {
			out.args = a.buildVaapiEncodeOnlyVideoArgs(out.args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		}
	} else {
		out.args = a.buildCPUVideoArgs(out.args, spec, codec.resolvedCodec, gop, segmentDurationSec)
	}

	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	out.args = append(out.args,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-ac", "2",
		"-ar", "48000",
		"-sn",
		"-f", "hls",
	)

	segmentType := "mpegts"
	segmentFilename := filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "seg_%06d.ts")
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		segmentType = "fmp4"
		segmentFilename = filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "seg_%06d.m4s")
	}

	out.args = append(out.args,
		"-hls_time", strconv.Itoa(segmentDurationSec),
		"-hls_list_size", strconv.Itoa(listSize),
		"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
		"-hls_segment_type", segmentType,
		"-hls_segment_filename", segmentFilename,
	)
	if segmentType == "fmp4" {
		out.args = append(out.args, "-hls_fmp4_init_filename", "init.mp4")
	}
	if initSegmentDurationSec > 0 {
		out.args = append(out.args, "-hls_init_time", strconv.Itoa(initSegmentDurationSec))
	}

	outputPath := filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "index.m3u8")
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "output_dir_ready").
		Str("output_path", outputPath).
		Msg("output directory ready")
	out.args = append(out.args, outputPath)

	return out, nil
}

func (a *LocalAdapter) shouldPreferSafariRuntimeRemux(ctx context.Context, spec ports.StreamSpec, inputURL string) bool {
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Name), profiles.ProfileSafari) {
		return false
	}
	if !spec.Profile.TranscodeVideo {
		return false
	}
	if strings.TrimSpace(inputURL) == "" {
		return false
	}

	probeTimeout := a.SafariRuntimeProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 6 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var (
		info *vod.StreamInfo
		err  error
	)
	if a.streamProbeFn != nil {
		info, err = a.streamProbeFn(probeCtx, inputURL)
	} else {
		info, err = infraffmpeg.ProbeWithBin(probeCtx, a.FFprobeBin, inputURL)
	}
	if err != nil {
		a.Logger.Info().
			Err(err).
			Str("sessionId", spec.SessionID).
			Str("input_url", sanitizeURLForLog(inputURL)).
			Dur("probe_timeout", probeTimeout).
			Msg("safari runtime probe failed; keeping transcode path")
		return false
	}

	if !strings.EqualFold(strings.TrimSpace(info.Video.CodecName), "h264") || info.Video.Interlaced {
		return false
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("video_codec", info.Video.CodecName).
		Bool("interlaced", info.Video.Interlaced).
		Str("container", info.Container).
		Msg("safari runtime probe selected remux path")
	return true
}

func usesLegacyCPUDefaults(spec ports.StreamSpec, outputCodec string) bool {
	prof := spec.Profile
	return prof.Name == "" && prof.VideoCodec == "" && !prof.TranscodeVideo && outputCodec == "h264"
}

func (a *LocalAdapter) buildVaapiVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi")

	if prof.Deinterlace {
		args = append(args, "-vf", "deinterlace_vaapi")
	}

	encoder := "h264_vaapi"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_vaapi"
	case "av1":
		encoder = "av1_vaapi"
	}
	args = append(args, "-c:v", encoder)

	if prof.VideoMaxRateK > 0 {
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", prof.VideoMaxRateK),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	} else {
		args = append(args, "-global_quality", "23")
	}

	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
	)

	args = append(args, "-profile:v", "main")
	return args
}

func (a *LocalAdapter) buildVaapiEncodeOnlyVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi_encode_only").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi encode only")

	filter := "format=nv12,hwupload"
	if prof.Deinterlace {
		filter = a.deinterlaceFilterForProfile(spec) + "," + filter
	}
	args = append(args, "-vf", filter)

	encoder := "h264_vaapi"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_vaapi"
	case "av1":
		encoder = "av1_vaapi"
	}
	args = append(args, "-c:v", encoder)

	if prof.VideoMaxRateK > 0 {
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", prof.VideoMaxRateK),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	} else {
		args = append(args, "-global_quality", "23")
	}

	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
		"-profile:v", "main",
	)
	return args
}

func (a *LocalAdapter) buildCopyVideoArgs(args []string, spec ports.StreamSpec) []string {
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "copy").
		Str("video.codec", "copy").
		Msg("pipeline video: copy")

	return append(args, "-c:v", "copy")
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

	deinterlaceFilter := a.deinterlaceFilterForProfile(spec)
	if deinterlace {
		args = append(args, "-vf", deinterlaceFilter)
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
	args = append(args, "-crf", strconv.Itoa(crf))

	if !legacy && prof.VideoMaxRateK > 0 {
		args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	}

	if codec == "libx264" {
		args = append(args, "-x264-params", fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0", gop, gop))
	}
	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
		"-pix_fmt", "yuv420p",
		"-profile:v", "main",
	)
	return args
}

func (a *LocalAdapter) deinterlaceFilterForProfile(spec ports.StreamSpec) string {
	deinterlaceFilter := "yadif"
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") && strings.TrimSpace(a.SafariDirtyFilter) != "" {
		deinterlaceFilter = strings.TrimSpace(a.SafariDirtyFilter)
	}
	return deinterlaceFilter
}

func (a *LocalAdapter) supportedHWCodecs() []string {
	codecs := make([]string, 0, 3)
	if a.VaapiEncoderVerified("h264_vaapi") {
		codecs = append(codecs, "h264")
	}
	if a.VaapiEncoderVerified("hevc_vaapi") {
		codecs = append(codecs, "hevc")
	}
	if a.VaapiEncoderVerified("av1_vaapi") {
		codecs = append(codecs, "av1")
	}
	return codecs
}

func isPreferHWProfile(profileName string) bool {
	p := strings.ToLower(strings.TrimSpace(profileName))
	return p == "av1_hw" || strings.HasSuffix(p, "_hw") || strings.HasSuffix(p, "_hw_ll")
}

func codecToVAAPIEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		return "h264_vaapi", true
	case "hevc":
		return "hevc_vaapi", true
	case "av1":
		return "av1_vaapi", true
	default:
		return "", false
	}
}

func normalizeRequestedCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch c {
	case "", "h264", "avc", "avc1", "libx264", "h264_vaapi":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return c
	}
}

func fpsCacheKey(source ports.StreamSource, inputURL string) string {
	if source.Type == ports.SourceTuner {
		if ref := normalizeServiceRef(source.ID); ref != "" {
			return "service_ref:" + ref
		}
	}
	if ref := extractServiceRefFromURL(inputURL); ref != "" {
		return "service_ref:" + ref
	}
	return ""
}

func normalizeServiceRef(raw string) string {
	ref := strings.TrimSpace(raw)
	ref = strings.Trim(ref, "/")
	if ref == "" {
		return ""
	}
	if !isLikelyServiceRef(ref) {
		return ""
	}
	return ref
}

func extractServiceRefFromURL(inputURL string) string {
	u, err := url.Parse(strings.TrimSpace(inputURL))
	if err != nil {
		return ""
	}
	if ref := normalizeServiceRef(u.Query().Get("ref")); ref != "" {
		return ref
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return normalizeServiceRef(path)
}

func isLikelyServiceRef(value string) bool {
	return strings.Count(value, ":") >= 5
}
