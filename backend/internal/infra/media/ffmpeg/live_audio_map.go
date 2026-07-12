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
		Maps:         []string{defaultLiveAudioMap},
		AudioArgs:    appendLiveAudioArgs(nil, spec, 2),
		IsMultiAudio: true,
		VarStreamMap: "v:0,agroup:audio a:0,agroup:audio,default:yes,language:deu",
	}

	if spec.Mode != ports.ModeLive || spec.Format != ports.FormatHLS || strings.TrimSpace(inputURL) == "" {
		return defaultSel
	}
	if !spec.Profile.TranscodeVideo || !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return defaultSel
	}

	if !spec.Profile.EnableMultiAudio {
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "live_audio_probe_skipped").
			Msg("multi-audio disabled via settings; bypassing ffprobe latency entirely")
		return defaultSel
	}

	var err error
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
	// Apple HLS strictly requires audio and video to be separated in fMP4, even
	// if there is only one audio track. Multiplexing them into a single .m4s causes
	// a black screen with audio on Safari/iOS.
	// Therefore, we ALWAYS use the VarStreamMap logic below to separate the tracks,
	// generating a Master Playlist and independent audio/video media playlists.

	// Preserve original Enigma2 stream order so the primary track sent by Enigma2 is DEFAULT=YES
	ordered := audioStreams

	maps := make([]string, 0, len(ordered))
	maxChannels := 2
	for _, stream := range ordered {
		if stream.Channels > maxChannels {
			maxChannels = stream.Channels
		}
	}
	audioArgs := appendLiveAudioArgs(nil, spec, maxChannels)
	varMapParts := []string{"v:0,agroup:audio"}

	for idx, stream := range ordered {
		maps = append(maps, fmt.Sprintf("0:%d?", stream.Index))
		lang := extractAudioLanguage(stream.Tags)
		defaultFlag := "no"
		if idx == 0 {
			defaultFlag = "yes"
		}
		varMapParts = append(varMapParts, fmt.Sprintf("a:%d,agroup:audio,default:%s,language:%s", idx, defaultFlag, lang))
		title := extractAudioTitle(stream.Tags, idx, lang)
		audioArgs = append(audioArgs, fmt.Sprintf("-metadata:s:a:%d", idx), fmt.Sprintf("title=%s", title))
	}

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "live_multi_audio_selected").
		Int("audio_track_count", len(ordered)).
		Str("var_stream_map", strings.Join(varMapParts, " ")).
		Msg("selected multiple live audio streams for synchronized HLS Multi-Audio Master Playlist")

	return liveAudioSelection{
		Maps:         maps,
		AudioArgs:    audioArgs,
		IsMultiAudio: true,
		VarStreamMap: strings.Join(varMapParts, " "),
	}
}

func extractAudioLanguage(tags map[string]string) string {
	for k, v := range tags {
		if strings.EqualFold(k, "language") {
			vClean := strings.ToUpper(strings.TrimSpace(v))
			if len(vClean) >= 2 {
				return vClean
			}
		}
	}
	return "GER"
}

func extractAudioTitle(tags map[string]string, idx int, lang string) string {
	for k, v := range tags {
		if strings.EqualFold(k, "title") {
			vClean := strings.TrimSpace(v)
			if vClean != "" {
				return vClean
			}
		}
	}
	if idx == 0 {
		return fmt.Sprintf("Stereo (%s)", lang)
	}
	return fmt.Sprintf("Audio %d (%s)", idx+1, lang)
}

func (a *LocalAdapter) probeLiveAudioStreams(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]liveAudioStream, error) {
	if a.liveAudioProbeFn != nil {
		return a.liveAudioProbeFn(ctx, inputURL)
	}

	timeout := 8 * time.Second
	if isStreamRelayURL(inputURL) || spec.Source.Type == ports.SourceTuner {
		// DVB streams (Vu+) take a few seconds to tune. Give the context 10 seconds.
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
		} else {
			// Instead of 15000000 (15s), use 5000000 (5s) so the probe finishes much faster!
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
		"-fflags", "+discardcorrupt",
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
