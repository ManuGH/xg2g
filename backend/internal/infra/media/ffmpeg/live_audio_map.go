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
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	Tags          map[string]string `json:"tags"`
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
	if len(audioStreams) == 1 {
		selected := audioStreams[0]
		mapArg := fmt.Sprintf("0:%d?", selected.Index)
		codecName := strings.ToLower(strings.TrimSpace(selected.CodecName))
		audioArgs := appendLiveAudioArgs(nil, spec, selected.Channels)

		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "live_audio_stream_selected").
			Str("audio_map", mapArg).
			Str("audio_action", "transcode_aac").
			Int("input_stream_index", selected.Index).
			Int("input_audio_channels", selected.Channels).
			Str("input_audio_layout", strings.TrimSpace(selected.ChannelLayout)).
			Str("input_audio_codec", codecName).
			Msg("selected single live audio stream for synchronized AAC transcode")

		return liveAudioSelection{
			Maps:      []string{mapArg},
			AudioArgs: audioArgs,
		}
	}

	// FFmpeg 8 cannot emit AUTOSELECT=YES for HLS audio renditions. Safari/HLS.js
	// consequently leaves the external audio group unloaded. Keep live playback
	// audible by muxing ONE track into the A/V playlist until the master-playlist
	// writer can produce a standards-compliant rendition group.
	//
	// Which one is a language question, not a channel-count question. Picking the
	// first 2-channel stream looked like a compatibility measure but was not one:
	// appendLiveAudioArgs pins -ac 2 unconditionally, so a 5.1 source is
	// downmixed either way. All that rule did was skip the broadcaster's primary
	// track whenever it carried surround - a German channel with deu 5.1 plus eng
	// stereo played out in English.
	selected := audioStreams[0]
	if preferred, ok := preferredAudioStream(audioStreams, a.Config.LiveAudioLanguages); ok {
		selected = preferred
	}
	mapArg := fmt.Sprintf("0:%d?", selected.Index)
	audioArgs := appendLiveAudioArgs(nil, spec, selected.Channels)
	a.Logger.Warn().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "live_multi_audio_compatibility_fallback").
		Int("source_audio_track_count", len(audioStreams)).
		Int("selected_input_stream_index", selected.Index).
		Str("audio_map", mapArg).
		Msg("muxing one AAC track into live HLS playlist because external audio renditions are not Safari-compatible")

	return liveAudioSelection{
		Maps:      []string{mapArg},
		AudioArgs: audioArgs,
	}
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
		"-show_entries", "stream=index,codec_type,codec_name,channels,channel_layout,tags",
		"-of", "json",
		inputURL,
	)
}

// preferredAudioStream picks the first stream whose language tag matches the
// operator's preference list, in preference order. Without a configured
// preference the caller keeps the broadcaster's first track, which is the
// primary language by DVB convention.
func preferredAudioStream(streams []liveAudioStream, languages []string) (liveAudioStream, bool) {
	for _, want := range languages {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		for _, stream := range streams {
			if strings.ToLower(strings.TrimSpace(stream.Tags["language"])) == want {
				return stream, true
			}
		}
	}
	return liveAudioStream{}, false
}
