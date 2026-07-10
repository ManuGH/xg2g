package ffmpeg

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func avsyncContainsToken(args []string, tok string) bool {
	for _, a := range args {
		if a == tok {
			return true
		}
	}
	return false
}

func TestTransformArgsForAvsyncPipe(t *testing.T) {
	in := []string{
		"-fflags", "+discardcorrupt+flush_packets",
		"-ignore_unknown",
		"-avoid_negative_ts", "make_zero",
		"-user_agent", "Lavf",
		"-headers", "Icy-MetaData: 1\r\n",
		"-analyzeduration", "3000000",
		"-probesize", "20M",
		"-protocol_whitelist", "file,http,https,tcp,tls",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
		"-i", "http://10.10.55.64:17999/1:0:19:132F:3EF:1:C00000:0:0:0",
		"-progress", "pipe:2",
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ac", "2",
		"-ar", "48000",
		"-sn",
		"-f", "hls",
	}
	got := transformArgsForAvsyncPipeMode(in, 1.735, true, false)
	joined := strings.Join(got, " ")

	if !strings.Contains(joined, "-i pipe:0") {
		t.Fatalf("expected -i pipe:0, got: %s", joined)
	}
	if avsyncContainsToken(got, "-ss") {
		t.Fatalf("copy mode must not seek the input: %s", joined)
	}
	if strings.Contains(joined, "http://10.10.55.64") {
		t.Fatalf("input URL must be replaced: %s", joined)
	}
	for _, banned := range []string{
		"-headers", "-user_agent", "-protocol_whitelist",
		"-reconnect", "-reconnect_at_eof", "-reconnect_streamed",
		"-reconnect_delay_max", "-reconnect_on_network_error", "-reconnect_on_http_error",
	} {
		if avsyncContainsToken(got, banned) {
			t.Fatalf("HTTP-only flag %q must be stripped: %s", banned, joined)
		}
	}
	if !strings.Contains(joined, "-af aresample=async=1,atrim=start=1.735 -c:a aac") {
		t.Fatalf("atrim must precede the audio codec: %s", joined)
	}
	if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-ar 48000") {
		t.Fatalf("copy + audio-format args must survive: %s", joined)
	}
	if !strings.Contains(joined, "-probesize 20M") || !strings.Contains(joined, "-avoid_negative_ts make_zero") {
		t.Fatalf("probe/ts args must survive: %s", joined)
	}
}

func TestTransformArgsForAvsyncPipe_TranscodeUsesInputSeek(t *testing.T) {
	in := []string{
		"-avoid_negative_ts", "make_zero",
		"-user_agent", "Lavf",
		"-i", "http://x/y",
		"-c:v", "av1_vaapi",
		"-c:a", "aac",
		"-f", "mp4",
	}
	got := transformArgsForAvsyncPipeMode(in, 2.141, true, true)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-ss 2.141 -i pipe:0") {
		t.Fatalf("transcode mode must seek the input to the orphan point: %s", joined)
	}
	if strings.Contains(joined, "atrim") {
		t.Fatalf("transcode mode must not add an audio atrim: %s", joined)
	}
	if avsyncContainsToken(got, "-user_agent") {
		t.Fatalf("HTTP-only flags must be stripped: %s", joined)
	}
}

func TestTransformArgsForAvsyncPipe_TranscodeDiagnosticModeSkipsSeek(t *testing.T) {
	in := []string{"-i", "http://x/y", "-c:v", "av1_vaapi", "-c:a", "aac", "-f", "mp4"}
	got := transformArgsForAvsyncPipeMode(in, 2.141, false, true)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-i pipe:0") {
		t.Fatalf("expected pipe input: %s", joined)
	}
	if avsyncContainsToken(got, "-ss") || strings.Contains(joined, "atrim") {
		t.Fatalf("diagnostic mode must not correct anything: %s", joined)
	}
}

func TestTransformArgsForAvsyncPipe_NoAudioStreamDoesNotPanic(t *testing.T) {
	in := []string{"-i", "http://x/y", "-c:v", "copy", "-f", "hls"}
	got := transformArgsForAvsyncPipeMode(in, 1.0, true, false)
	if !avsyncContainsToken(got, "pipe:0") {
		t.Fatalf("expected pipe:0: %v", got)
	}
	if strings.Contains(strings.Join(got, " "), "atrim") {
		t.Fatalf("no audio output -> no atrim expected: %v", got)
	}
}

func TestTransformArgsForAvsyncPipe_AudioCopySkipsTrim(t *testing.T) {
	in := []string{"-i", "http://x/y", "-c:v", "copy", "-c:a", "copy", "-f", "hls"}
	got := transformArgsForAvsyncPipeMode(in, 1.735, true, false)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-i pipe:0") {
		t.Fatalf("expected pipe input: %s", joined)
	}
	if strings.Contains(joined, "atrim") {
		t.Fatalf("-c:a copy must prevent atrim insertion: %s", joined)
	}
}

func TestTransformArgsForAvsyncPipe_DiagnosticModeKeepsPipeWithoutTrim(t *testing.T) {
	in := []string{"-i", "http://x/y", "-c:v", "copy", "-c:a", "aac", "-f", "hls"}
	got := transformArgsForAvsyncPipeMode(in, 1.0, false, false)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-i pipe:0") {
		t.Fatalf("expected pipe input: %s", joined)
	}
	if strings.Contains(joined, "atrim") {
		t.Fatalf("diagnostic mode must not trim audio: %s", joined)
	}
}

func TestShouldAvsyncAtrimOnlyAllowsLiveFMP4(t *testing.T) {
	adapter := &LocalAdapter{LiveAvsyncAtrim: true}
	spec := ports.StreamSpec{
		Mode: ports.ModeLive,
		Profile: model.ProfileSpec{
			Container:      "fmp4",
			TranscodeVideo: false,
		},
		Source: ports.StreamSource{Type: ports.SourceTuner},
	}
	if !adapter.shouldAvsyncAtrim(spec) {
		t.Fatal("expected live fMP4 copy to enable orphan correction")
	}

	spec.Profile.TranscodeVideo = true
	if !adapter.shouldAvsyncAtrim(spec) {
		t.Fatal("expected live fMP4 transcode to enable orphan correction")
	}
	spec.Profile.TranscodeVideo = false
	spec.Profile.Container = "mpegts"
	if adapter.shouldAvsyncAtrim(spec) {
		t.Fatal("MPEG-TS copy must not enable orphan atrim")
	}
	spec.Profile.Container = "fmp4"
	spec.Source.Type = ports.SourceFile
	if adapter.shouldAvsyncAtrim(spec) {
		t.Fatal("file input must not enable live orphan atrim")
	}
}
