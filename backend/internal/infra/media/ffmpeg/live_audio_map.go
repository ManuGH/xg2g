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

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	infraffmpeg "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
)

const defaultLiveAudioMap = "0:a:0?"

type liveAudioStream struct {
	Index         int    `json:"index"`
	CodecType     string `json:"codec_type"`
	CodecName     string `json:"codec_name"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout"`
}

func (a *LocalAdapter) selectLiveAudioMap(ctx context.Context, spec ports.StreamSpec, inputURL string) string {
	if spec.Mode != ports.ModeLive || spec.Format != ports.FormatHLS || strings.TrimSpace(inputURL) == "" {
		return defaultLiveAudioMap
	}
	if !spec.Profile.TranscodeVideo || !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return defaultLiveAudioMap
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
		return defaultLiveAudioMap
	}

	selected, ok := preferredLiveAudioStream(streams)
	if !ok || selected.Index < 0 {
		return defaultLiveAudioMap
	}

	selectedMap := fmt.Sprintf("0:%d?", selected.Index)
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "live_audio_stream_selected").
		Str("audio_map", selectedMap).
		Int("input_stream_index", selected.Index).
		Int("input_audio_channels", selected.Channels).
		Str("input_audio_layout", strings.TrimSpace(selected.ChannelLayout)).
		Str("input_audio_codec", strings.TrimSpace(selected.CodecName)).
		Msg("selected live audio stream for AAC transcode")
	return selectedMap
}

func preferredLiveAudioStream(streams []liveAudioStream) (liveAudioStream, bool) {
	var first liveAudioStream
	haveFirst := false
	for _, stream := range streams {
		if !strings.EqualFold(strings.TrimSpace(stream.CodecType), "audio") {
			continue
		}
		if !haveFirst {
			first = stream
			haveFirst = true
		}
		if isStereoAudioStream(stream) {
			return stream, true
		}
	}
	return first, haveFirst
}

func isStereoAudioStream(stream liveAudioStream) bool {
	if stream.Channels == 2 {
		return true
	}
	layout := strings.ToLower(strings.TrimSpace(stream.ChannelLayout))
	return layout == "stereo" || strings.Contains(layout, "stereo")
}

func (a *LocalAdapter) probeLiveAudioStreams(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]liveAudioStream, error) {
	if a.liveAudioProbeFn != nil {
		return a.liveAudioProbeFn(ctx, inputURL)
	}

	timeout := 5 * time.Second
	if isStreamRelayURL(inputURL) {
		timeout = 8 * time.Second
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
	if isStreamRelayURL(inputURL) && spec.Profile.TranscodeVideo {
		if v := strings.TrimSpace(a.StreamRelayAnalyzeDuration); v != "" {
			analyzeDuration = v
		} else {
			analyzeDuration = "10000000"
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
		"-show_entries", "stream=index,codec_type,codec_name,channels,channel_layout",
		"-of", "json",
		inputURL,
	)
}
