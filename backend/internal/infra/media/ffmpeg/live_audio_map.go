package ffmpeg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	infraffmpeg "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
)

const defaultLiveAudioMap = "0:a:0?"

type liveAudioDisposition struct {
	Default         int `json:"default"`
	VisualImpaired  int `json:"visual_impaired"`
	HearingImpaired int `json:"hearing_impaired"`
	CleanEffects    int `json:"clean_effects"`
	Descriptions    int `json:"descriptions"`
}

type liveAudioStream struct {
	Index         int                  `json:"index"`
	ID            string               `json:"id"`
	CodecType     string               `json:"codec_type"`
	CodecName     string               `json:"codec_name"`
	Channels      int                  `json:"channels"`
	ChannelLayout string               `json:"channel_layout"`
	Tags          map[string]string    `json:"tags"`
	Disposition   liveAudioDisposition `json:"disposition"`
}

type liveAudioSelection struct {
	Maps         []string
	AudioArgs    []string
	IsMultiAudio bool
	VarStreamMap string
}

func (a *LocalAdapter) planLiveAudioSelection(ctx context.Context, spec ports.StreamSpec, inputURL string) liveAudioSelection {
	defaultSel := liveAudioSelection{
		Maps:      []string{defaultLiveAudioMap},
		AudioArgs: appendLiveAudioArgs(nil, spec, 2),
	}

	if spec.Mode != ports.ModeLive || spec.Format != ports.FormatHLS || strings.TrimSpace(inputURL) == "" {
		return defaultSel
	}
	if !spec.Profile.TranscodeVideo || !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return defaultSel
	}
	if isAndroidTVNativeSpec(spec) {
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "live_audio_probe_skipped_android_tv_native").
			Str("audio_map", defaultLiveAudioMap).
			Msg("using primary live audio stream for native Android TV")
		return defaultSel
	}

	streams, err := a.probeLiveAudioStreams(ctx, spec, inputURL)
	if err != nil {
		a.Logger.Debug().
			Err(err).
			Str("session_id", spec.SessionID).
			Str("startup_phase", "live_audio_probe_failed").
			Str("input_url", sanitizeURLForLog(inputURL)).
			Str("fallback_map", defaultLiveAudioMap).
			Msg("live audio stream probe failed; using first audio stream")
		return defaultSel
	}

	var audioStreams []liveAudioStream
	for _, stream := range streams {
		if strings.EqualFold(strings.TrimSpace(stream.CodecType), "audio") {
			audioStreams = append(audioStreams, stream)
		}
	}
	if len(audioStreams) == 0 {
		return defaultSel
	}

	// Build ProbeTrackObservations for audiotopology domain
	probeObs := make([]audiotopology.ProbeTrackObservation, len(audioStreams))
	streamByPID := make(map[uint16]liveAudioStream, len(audioStreams))

	for i, s := range audioStreams {
		var pid uint16
		if s.ID != "" {
			var p uint64
			if strings.HasPrefix(strings.ToLower(s.ID), "0x") {
				if _, err := fmt.Sscanf(s.ID, "0x%x", &p); err == nil {
					pid = uint16(p)
				}
			} else {
				if _, err := fmt.Sscanf(s.ID, "%d", &p); err == nil {
					pid = uint16(p)
				}
			}
		}
		if pid == 0 {
			pid = uint16(s.Index + 1)
		}
		streamByPID[pid] = s

		probeObs[i] = audiotopology.ProbeTrackObservation{
			StreamIndex:                 s.Index,
			PID:                         pid,
			Codec:                       s.CodecName,
			Channels:                    s.Channels,
			ChannelLayout:               s.ChannelLayout,
			Language:                    s.Tags["language"],
			DispositionVisualImpaired:   s.Disposition.VisualImpaired > 0,
			DispositionHearingImpaired:  s.Disposition.HearingImpaired > 0,
			DispositionCleanEffects:     s.Disposition.CleanEffects > 0,
			DispositionDescriptions:     s.Disposition.Descriptions > 0,
			DispositionBroadcastDefault: s.Disposition.Default > 0,
		}
	}

	clientCaps := audiotopology.ClientAudioCapabilities{
		SupportsAAC: true,
	}
	if len(a.Config.LiveAudioLanguages) > 0 {
		for _, l := range a.Config.LiveAudioLanguages {
			norm := audiotopology.NormalizeLanguage(l)
			if norm.ISO639_2 != "" && !norm.IsUndefined {
				clientCaps.PreferredLanguage = norm.ISO639_2
				break
			}
		}
	}
	clientFam := strings.ToLower(spec.ClientFamily)
	if strings.Contains(clientFam, "ios") || strings.Contains(clientFam, "safari") || strings.Contains(clientFam, "apple") {
		clientCaps.SupportsAC3 = true
		clientCaps.SupportsEAC3 = true
		clientCaps.SupportsSpatial51 = true
		clientCaps.PrefersPassthrough = true
	}

	topo := audiotopology.BuildTopology(spec.Source.ID, nil, probeObs, nil, time.Now())
	multiPlan := audiotopology.PlanMultiAudioOutput(topo, clientCaps)

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "audio_topology_resolved").
		Uint64("structural_revision", multiPlan.StructuralRevision).
		Uint64("metadata_revision", topo.MetadataRevision).
		Str("presence", string(multiPlan.Presence)).
		Int("tracks_count", len(multiPlan.Tracks)).
		Msg("evaluated audio topology and output policy")

	if len(multiPlan.Tracks) > 1 {
		var maps []string
		var audioArgs []string

		for i, tp := range multiPlan.Tracks {
			matchedStream, ok := streamByPID[tp.PID]
			if !ok {
				matchedStream = audioStreams[0]
			}
			mapArg := fmt.Sprintf("0:%d?", matchedStream.Index)
			maps = append(maps, mapArg)

			if tp.Strategy == audiotopology.CodecStrategyPassthrough || !spec.Profile.TranscodesAudio() {
				audioArgs = append(audioArgs, fmt.Sprintf("-c:a:%d", i), "copy")
			} else {
				encoderCodec := tp.EncoderCodec
				if encoderCodec == "" {
					encoderCodec = spec.Profile.ResolvedAudioCodec()
				}
				bitrateKbps := tp.BitrateKbps
				if bitrateKbps <= 0 {
					bitrateKbps = 192
				}
				channels := tp.Channels
				if channels <= 0 {
					channels = 2
				}
				audioArgs = append(audioArgs,
					fmt.Sprintf("-c:a:%d", i), encoderCodec,
					fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", bitrateKbps),
					fmt.Sprintf("-ac:a:%d", i), fmt.Sprintf("%d", channels),
					fmt.Sprintf("-ar:a:%d", i), "48000",
				)
			}

			a.Logger.Info().
				Str("session_id", spec.SessionID).
				Str("startup_phase", "live_multi_audio_track_planned").
				Int("audio_index", i).
				Str("audio_map", mapArg).
				Str("audio_strategy", string(tp.Strategy)).
				Uint16("pid", tp.PID).
				Str("encoder_codec", tp.EncoderCodec).
				Str("hls_codec", tp.HLSCodec).
				Int("channels", tp.Channels).
				Int("bitrate_kbps", tp.BitrateKbps).
				Str("track_name", tp.Name).
				Bool("is_default", tp.IsDefault).
				Msg("configured multi-audio rendition")
		}
		audioArgs = append(audioArgs, "-sn")

		vsm := multiPlan.BuildVarStreamMap()
		return liveAudioSelection{
			Maps:         maps,
			AudioArgs:    audioArgs,
			IsMultiAudio: true,
			VarStreamMap: vsm,
		}
	}

	var selectedPlan audiotopology.TrackPlan
	if len(multiPlan.Tracks) > 0 {
		selectedPlan = multiPlan.Tracks[0]
	}

	matchedStream, ok := streamByPID[selectedPlan.PID]
	if !ok {
		matchedStream = audioStreams[0]
	}
	mapArg := fmt.Sprintf("0:%d?", matchedStream.Index)

	if selectedPlan.Strategy == audiotopology.CodecStrategyUnsupported {
		a.Logger.Warn().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "live_audio_strategy_unsupported").
			Uint16("pid", selectedPlan.PID).
			Str("input_codec", string(selectedPlan.InputCodec)).
			Msg("track plan strategy unsupported by client capabilities; applying safe stereo transcode fallback")
		selectedPlan.Strategy = audiotopology.CodecStrategyTranscode
		selectedPlan.EncoderCodec = "aac"
		selectedPlan.Channels = 2
		selectedPlan.BitrateKbps = 192
	}

	audioArgs := appendPlannedAudioArgs(nil, spec, selectedPlan)

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "live_audio_stream_selected").
		Str("audio_map", mapArg).
		Str("audio_strategy", string(selectedPlan.Strategy)).
		Uint16("pid", selectedPlan.PID).
		Str("encoder_codec", selectedPlan.EncoderCodec).
		Str("hls_codec", selectedPlan.HLSCodec).
		Int("channels", selectedPlan.Channels).
		Int("bitrate_kbps", selectedPlan.BitrateKbps).
		Str("track_name", selectedPlan.Name).
		Msg("selected live audio stream for playback pipeline")

	return liveAudioSelection{
		Maps:      []string{mapArg},
		AudioArgs: audioArgs,
	}
}

func appendPlannedAudioArgs(args []string, spec ports.StreamSpec, plan audiotopology.TrackPlan) []string {
	if plan.Strategy == audiotopology.CodecStrategyPassthrough || !spec.Profile.TranscodesAudio() {
		return append(args, "-c:a", "copy", "-sn")
	}

	encoderCodec := plan.EncoderCodec
	if encoderCodec == "" {
		encoderCodec = spec.Profile.ResolvedAudioCodec()
	}

	bitrateKbps := plan.BitrateKbps
	if spec.Profile.AudioBitrateK > 0 {
		bitrateKbps = spec.Profile.AudioBitrateK
	} else if bitrateKbps <= 0 {
		bitrateKbps = 192
	}

	channels := plan.Channels
	if channels <= 0 {
		channels = 2
	}

	return append(args,
		"-c:a", encoderCodec,
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-ac", fmt.Sprintf("%d", channels),
		"-ar", "48000",
		"-sn",
	)
}

func (a *LocalAdapter) probeLiveAudioStreams(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]liveAudioStream, error) {
	if a.liveAudioProbeFn != nil {
		return a.liveAudioProbeFn(ctx, inputURL)
	}

	timeout := 5 * time.Second
	if isStreamRelayURL(inputURL) || spec.Source.Type == ports.SourceTuner {
		timeout = 10 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ffprobeBin := strings.TrimSpace(a.FFprobeBin)
	if ffprobeBin == "" {
		ffprobeBin = "ffprobe"
	}

	args := a.buildLiveAudioProbeArgs(spec, inputURL)
	// #nosec G204 -- ffprobe bin path is trusted from config; args are fixed literals plus the source URL.
	cmd := exec.CommandContext(probeCtx, ffprobeBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var parsed struct {
			Streams []liveAudioStream `json:"streams"`
		}
		if len(out) > 0 && json.Unmarshal(out, &parsed) == nil && len(parsed.Streams) > 0 {
			a.Logger.Warn().Err(err).Msg("probeLiveAudioStreams exited non-zero but returned valid streams json")
			return parsed.Streams, nil
		}
		return nil, decorateProbeError(err, stderr.String())
	}

	var parsed struct {
		Streams []liveAudioStream `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}
	return parsed.Streams, nil
}

func (a *LocalAdapter) buildLiveAudioProbeArgs(spec ports.StreamSpec, inputURL string) []string {
	headers := "Connection: close\r\nIcy-MetaData: 1\r\n"
	if u, err := url.Parse(inputURL); err == nil && u.User != nil {
		pwd, _ := u.User.Password()
		auth := u.User.Username() + ":" + pwd
		headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
		u.User = nil
		inputURL = u.String()
	}

	analyzeDuration := strings.TrimSpace(a.LiveAnalyzeDuration)
	if analyzeDuration == "" {
		analyzeDuration = strings.TrimSpace(a.AnalyzeDuration)
	}
	probeSize := strings.TrimSpace(a.LiveProbeSize)
	if probeSize == "" {
		probeSize = strings.TrimSpace(a.ProbeSize)
	}
	if isStreamRelayURL(inputURL) || spec.Source.Type == ports.SourceTuner {
		if v := strings.TrimSpace(a.StreamRelayAnalyzeDuration); v != "" {
			analyzeDuration = v
		} else if spec.Profile.TranscodeVideo {
			analyzeDuration = "15000000"
		} else {
			analyzeDuration = "5000000"
		}
		if v := strings.TrimSpace(a.StreamRelayProbeSize); v != "" {
			probeSize = v
		} else {
			probeSize = "20M"
		}
	}

	args := []string{
		"-v", "error",
		"-headers", headers,
	}
	if whitelist, ok := infraffmpeg.InputProtocolWhitelist(inputURL); ok {
		args = append(args, "-protocol_whitelist", whitelist)
	}
	if analyzeDuration != "" {
		args = append(args, "-analyzeduration", analyzeDuration)
	}
	if probeSize != "" {
		args = append(args, "-probesize", probeSize)
	}
	return append(args,
		"-select_streams", "a",
		"-show_entries", "stream=index,id,codec_type,codec_name,channels,channel_layout,tags,disposition",
		"-of", "json",
		inputURL,
	)
}
