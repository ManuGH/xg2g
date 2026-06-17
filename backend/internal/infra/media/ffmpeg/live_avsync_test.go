package ffmpeg

import (
	"strings"
	"testing"
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
	got := transformArgsForAvsyncPipe(in, 1.735)
	joined := strings.Join(got, " ")

	if !strings.Contains(joined, "-i pipe:0") {
		t.Fatalf("expected -i pipe:0, got: %s", joined)
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
	// The leading-audio trim must sit immediately before the audio encoder.
	if !strings.Contains(joined, "-af atrim=start=1.735 -c:a aac") {
		t.Fatalf("atrim must precede the audio codec: %s", joined)
	}
	// Video stays copy; audio output format args survive.
	if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-ar 48000") {
		t.Fatalf("copy + audio-format args must survive: %s", joined)
	}
	// Probe args that are valid for pipe input must be preserved.
	if !strings.Contains(joined, "-probesize 20M") || !strings.Contains(joined, "-avoid_negative_ts make_zero") {
		t.Fatalf("probe/ts args must survive: %s", joined)
	}
}

func TestTransformArgsForAvsyncPipe_NoAudioStreamDoesNotPanic(t *testing.T) {
	in := []string{"-i", "http://x/y", "-c:v", "copy", "-f", "hls"}
	got := transformArgsForAvsyncPipe(in, 1.0)
	if !avsyncContainsToken(got, "pipe:0") {
		t.Fatalf("expected pipe:0: %v", got)
	}
	if strings.Contains(strings.Join(got, " "), "atrim") {
		t.Fatalf("no audio output -> no atrim expected: %v", got)
	}
}
