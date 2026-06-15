package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// TestParseFFmpegVersion is L4's core unit: the parser must return the raw token after
// "version" across the real-world formats (distro suffix, git tag, plain release), NOT a
// normalized semver — the suffix is informative and a strict parse would drop it. The exec
// itself is platform-dependent (like L18) and not the testable part; this is.
func TestParseFFmpegVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain release", "ffmpeg version 7.0.2 Copyright (c) 2000-2024\nbuilt with gcc\n", "7.0.2"},
		{"ubuntu distro suffix", "ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright (c) 2000-2021\n", "4.4.2-0ubuntu0.22.04.1"},
		{"git build tag", "ffmpeg version n6.1 Copyright\n", "n6.1"},
		{"static build hash", "ffmpeg version N-109888-gd13a5d8b1c-static https://johnvansickle.com/ffmpeg/\n", "N-109888-gd13a5d8b1c-static"},
		{"no version keyword", "garbage output without the keyword\n", ""},
		{"empty", "", ""},
		{"version keyword last token", "ffmpeg version", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseFFmpegVersion(tc.in); got != tc.want {
				t.Errorf("parseFFmpegVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDetectFFmpegVersion_MissingBinReturnsEmpty covers the failure path: a missing/empty
// binary yields "" (not a fabricated value), which ffmpegVersion surfaces as "unknown".
func TestDetectFFmpegVersion_MissingBinReturnsEmpty(t *testing.T) {
	if got := detectFFmpegVersion(context.Background(), ""); got != "" {
		t.Errorf("empty bin: got %q, want empty", got)
	}
	if got := detectFFmpegVersion(context.Background(), "/nonexistent/xg2g-no-such-ffmpeg"); got != "" {
		t.Errorf("missing bin: got %q, want empty", got)
	}
}

// TestStatusHandler_ReportsRealGoAndUnknownFFmpeg pins the L4 fix end to end: Go must be the
// REAL runtime.Version() (not a literal), and with no ffmpeg binary the FFmpeg field is
// "unknown" (not the fabricated "8.1"). The failure path is also NOT cached (Variant 1): the
// field stays empty so a later request retries.
func TestStatusHandler_ReportsRealGoAndUnknownFFmpeg(t *testing.T) {
	h := NewStatusHandler(nil, "") // no ffmpeg bin -> unknown

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	var resp StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Runtime.Go != runtime.Version() {
		t.Errorf("Go version = %q, want the real runtime.Version() %q (not a hardcoded literal)", resp.Runtime.Go, runtime.Version())
	}
	if resp.Runtime.FFmpeg != "unknown" {
		t.Errorf("FFmpeg version = %q, want \"unknown\" when no binary is configured (not a fabricated literal)", resp.Runtime.FFmpeg)
	}

	h.mu.Lock()
	cached := h.ffmpegVer
	h.mu.Unlock()
	if cached != "" {
		t.Errorf("failed probe was cached (%q); Variant 1 must cache only success so a later request retries", cached)
	}
}
