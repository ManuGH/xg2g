package ffmpeg

import "testing"

// TestEncoderOpenFailureClassified guards the Q1 fix: ffmpeg emits the codec
// token (hevc_vaapi/av1_vaapi) on a separate component-prefix line from the
// actual "could not open encoder" / "error while opening encoder" message, so
// the backend matchers previously missed real hardware encoder-open failures
// and never demoted GPU->CPU. The call site is already backend-gated, so these
// substring matches only apply to the matching backend's failed sessions.
func TestEncoderOpenFailureClassified(t *testing.T) {
	failing := []string{
		"could not open encoder before eof",
		"[enc:hevc_vaapi] error while opening encoder - maybe incorrect parameters such as bit_rate, width or height",
		"[av1_vaapi] no usable encoding profile found",
		"cannot open encoder",
		"failed to open encoder",
	}
	for _, line := range failing {
		if !isVAAPIRuntimeFailureLine(line) {
			t.Errorf("VAAPI classifier should flag encoder-open failure: %q", line)
		}
		if !isNVENCRuntimeFailureLine(line) {
			t.Errorf("NVENC classifier should flag encoder-open failure: %q", line)
		}
	}

	benign := []string{
		"",
		"frame=  100 fps= 50 q=28.0 size=512kB time=00:00:04.00 bitrate=1048.6kbits/s",
		"opening 'index.m3u8' for writing",
		"[libx264 @ 0x0] using cpu capabilities: mmx2 sse2",
	}
	for _, line := range benign {
		if isVAAPIRuntimeFailureLine(line) {
			t.Errorf("VAAPI classifier should NOT flag benign line: %q", line)
		}
		if isNVENCRuntimeFailureLine(line) {
			t.Errorf("NVENC classifier should NOT flag benign line: %q", line)
		}
	}
}
