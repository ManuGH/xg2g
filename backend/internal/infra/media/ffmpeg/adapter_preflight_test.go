package ffmpeg

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/rs/zerolog"
)

func TestDeriveVAAPIEncoderCapabilities_UsesRelativeStartupCost(t *testing.T) {
	t.Parallel()

	caps := deriveVAAPIEncoderCapabilities(map[string]time.Duration{
		"h264_vaapi": 100 * time.Millisecond,
		"hevc_vaapi": 160 * time.Millisecond,
		"av1_vaapi":  320 * time.Millisecond,
	}, 1.75, 2.50)

	if !caps["h264_vaapi"].Verified || !caps["h264_vaapi"].AutoEligible {
		t.Fatalf("expected h264_vaapi to stay auto-eligible: %#v", caps["h264_vaapi"])
	}
	if !caps["hevc_vaapi"].Verified || !caps["hevc_vaapi"].AutoEligible {
		t.Fatalf("expected hevc_vaapi to be auto-eligible within ratio budget: %#v", caps["hevc_vaapi"])
	}
	if !caps["av1_vaapi"].Verified {
		t.Fatalf("expected av1_vaapi to be verified: %#v", caps["av1_vaapi"])
	}
	if caps["av1_vaapi"].AutoEligible {
		t.Fatalf("expected av1_vaapi to be excluded from auto ladder when above ratio budget: %#v", caps["av1_vaapi"])
	}
}

func TestDeriveVAAPIEncoderCapabilities_FallsBackToFastestVerifiedBaseline(t *testing.T) {
	t.Parallel()

	caps := deriveVAAPIEncoderCapabilities(map[string]time.Duration{
		"hevc_vaapi": 180 * time.Millisecond,
		"av1_vaapi":  500 * time.Millisecond,
	}, 1.75, 2.50)

	if !caps["hevc_vaapi"].AutoEligible {
		t.Fatalf("expected fastest verified encoder to seed auto ladder: %#v", caps["hevc_vaapi"])
	}
	if caps["av1_vaapi"].AutoEligible {
		t.Fatalf("expected slower av1_vaapi to stay out of auto ladder: %#v", caps["av1_vaapi"])
	}
}

func TestPreflightTS_SyncOK(t *testing.T) {
	buf := make([]byte, 188*3)
	buf[0] = 0x47
	buf[188] = 0x47
	buf[376] = 0x47

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	result, err := adapter.preflightTS(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected preflight success, got error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected preflight ok, got false (reason=%s)", result.Reason)
	}
	if result.Bytes < 188*3 {
		t.Fatalf("expected at least %d bytes, got %d", 188*3, result.Bytes)
	}
}

func TestPreflightTS_SyncMiss(t *testing.T) {
	buf := make([]byte, 188*3)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	result, err := adapter.preflightTS(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected preflight error, got nil")
	}
	if result.Reason != ports.PreflightReasonInvalidTS {
		t.Fatalf("expected invalid_ts, got %q", result.Reason)
	}
	if result.Detail != "sync_miss" {
		t.Fatalf("expected sync_miss detail, got %q", result.Detail)
	}
}

func TestPreflightTS_ShortRead(t *testing.T) {
	buf := make([]byte, 100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	result, err := adapter.preflightTS(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected preflight error, got nil")
	}
	if result.Reason != ports.PreflightReasonCorruptInput {
		t.Fatalf("expected corrupt_input, got %q", result.Reason)
	}
	if result.Detail != "short_read" {
		t.Fatalf("expected short_read detail, got %q", result.Detail)
	}
}

func TestPreflightTS_HTTPUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	result, err := adapter.preflightTS(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected preflight error, got nil")
	}
	if result.Reason != ports.PreflightReasonUnauthorized {
		t.Fatalf("expected unauthorized, got %q", result.Reason)
	}
	if result.Detail != "http_status_401" {
		t.Fatalf("expected http_status_401 detail, got %q", result.Detail)
	}
}

func TestSelectStreamURL_FallbackOffFails(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		calls++
		return ports.NewPreflightResult("sync_miss", 0, 0, 0, 17999), errors.New("no ts")
	}

	_, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-1",
		"1:0:19:2B66:3F3:1:C00000:0:0:0:",
		"http://127.0.0.1:17999/1:0:19:2B66:3F3:1:C00000:0:0:0:",
		preflight,
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrNoValidTS) {
		t.Fatalf("expected ErrNoValidTS, got %v", err)
	}
	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected PreflightError, got %T", err)
	}
	if got := pErr.StructuredResult().Reason; got != ports.PreflightReasonInvalidTS {
		t.Fatalf("expected invalid_ts structured reason, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 preflight call, got %d", calls)
	}
}

func TestSelectStreamURL_NoFallbackWhenNotRelay(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		calls++
		return ports.NewPreflightResult("sync_miss", 0, 0, 0, 8001), errors.New("no ts")
	}

	_, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-2",
		"1:0:19:2B66:3F3:1:C00000:0:0:0:",
		"http://127.0.0.1:8001/1:0:19:2B66:3F3:1:C00000:0:0:0:",
		preflight,
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ports.ErrNoValidTS) {
		t.Fatalf("expected ErrNoValidTS, got %v", err)
	}
	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected PreflightError, got %T", err)
	}
	if got := pErr.StructuredResult().Reason; got != ports.PreflightReasonInvalidTS {
		t.Fatalf("expected invalid_ts structured reason, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 preflight call, got %d", calls)
	}
}

func TestSelectStreamURL_FallbackTo8001(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	expectedFallback := "http://127.0.0.1:8001/" + serviceRef

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		calls++
		if strings.Contains(rawURL, ":17999") {
			return ports.NewPreflightResult("sync_miss", 0, 0, 0, 17999), errors.New("no ts")
		}
		return ports.NewSuccessfulPreflightResult(188*3, 0, 8001), nil
	}

	got, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-3",
		serviceRef,
		resolved,
		preflight,
	)
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if got != expectedFallback {
		t.Fatalf("expected fallback url %q, got %q", expectedFallback, got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 preflight calls, got %d", calls)
	}
}

func TestSelectStreamURL_FallbackFailedAllStructuredResult(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		return ports.NewPreflightResult("sync_miss", 0, 0, 0, 17999), errors.New("no ts")
	}

	_, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-4",
		serviceRef,
		resolved,
		preflight,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected PreflightError, got %T", err)
	}
	result := pErr.StructuredResult()
	if result.Reason != ports.PreflightReasonFallbackFailed {
		t.Fatalf("expected fallback_failed, got %q", result.Reason)
	}
	if result.Detail != "fallback_failed_all" {
		t.Fatalf("expected fallback_failed_all detail, got %q", result.Detail)
	}
}

func TestSelectStreamURL_DoesNotAcceptWebIFPlaylistAsTSFallback(t *testing.T) {
	e2 := enigma2.NewClientWithOptions("http://127.0.0.1", enigma2.Options{Timeout: time.Second})
	adapter := NewLocalAdapter("", "", "", e2, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		switch {
		case strings.Contains(rawURL, ":17999"):
			return ports.NewPreflightResult("sync_miss", http.StatusOK, 564, 0, 17999), errors.New("no ts")
		case strings.Contains(rawURL, ":8001"):
			return ports.NewPreflightResult("sync_miss", http.StatusOK, 564, 0, 8001), errors.New("no ts")
		case strings.Contains(rawURL, "/web/stream.m3u"):
			return ports.NewPreflightResult("sync_miss", http.StatusOK, 564, 0, 80), errors.New("playlist, not ts")
		default:
			t.Fatalf("unexpected preflight url %q", rawURL)
			return ports.PreflightResult{}, nil
		}
	}

	_, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-webif-playlist",
		serviceRef,
		resolved,
		preflight,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected PreflightError, got %T", err)
	}
	result := pErr.StructuredResult()
	if result.Reason != ports.PreflightReasonFallbackFailed {
		t.Fatalf("expected fallback_failed, got %q", result.Reason)
	}
	if result.Detail != "fallback_failed_all" {
		t.Fatalf("expected fallback_failed_all detail, got %q", result.Detail)
	}
}

func TestPreflight_HttpClientWiring(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")

	if adapter.httpClient == nil {
		t.Fatal("httpClient should not be nil")
	}
	if adapter.httpClient.Transport == nil {
		t.Fatal("httpClient.Transport should not be nil")
	}

	buf := make([]byte, 188*3)
	buf[0] = 0x47
	buf[188] = 0x47
	buf[376] = 0x47

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	result, err := adapter.preflightTS(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected preflight success, got error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected preflight ok, got false")
	}
}

func TestBuildArgs_WarmsStreamBeforeSkippingFPSProbe(t *testing.T) {
	t.Setenv("XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT", "true")
	t.Setenv("XG2G_SKIP_FPS_PROBE_WARMUP", "50ms")

	buf := make([]byte, 188*3)
	buf[0] = 0x47
	buf[188] = 0x47
	buf[376] = 0x47

	warmupHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		warmupHits++
		_, _ = w.Write(buf)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	streamURL := srv.URL + "/1:0:19:132F:3EF:1:C00000:0:0:0"

	adapter := NewLocalAdapter("", "", t.TempDir(), nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	probeCalls := 0
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		probeCalls++
		return 0, "", errors.New("probe should not be called")
	}

	spec := ports.StreamSpec{
		SessionID: "warmup-skip-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "h264",
			VideoCRF:       20,
			Preset:         "veryfast",
		},
		Source: ports.StreamSource{
			ID:   streamURL,
			Type: ports.SourceURL,
		},
	}

	adapter.setLastKnownFPS(fpsCacheKey(spec.Source, streamURL), 50)
	args, err := adapter.buildArgs(context.Background(), spec, streamURL)
	if err != nil {
		t.Fatalf("expected buildArgs success, got error: %v", err)
	}
	if probeCalls != 0 {
		t.Fatalf("expected probe to be skipped after warmup, got %d calls", probeCalls)
	}
	if warmupHits != 1 {
		t.Fatalf("expected exactly one warmup request, got %d", warmupHits)
	}
	if x264Params, ok := valueAfter(args, "-x264-params"); !ok || !strings.Contains(x264Params, "keyint=300:min-keyint=300:scenecut=0") {
		t.Fatalf("expected cached 50fps GOP params, got %q", x264Params)
	}
}

func TestBuildArgs_WarmupFailureFallsBackToFPSProbe(t *testing.T) {
	t.Setenv("XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT", "true")
	t.Setenv("XG2G_SKIP_FPS_PROBE_WARMUP", "50ms")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	streamURL := srv.URL + "/1:0:19:132F:3EF:1:C00000:0:0:0"

	adapter := NewLocalAdapter("", "", t.TempDir(), nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	probeCalls := 0
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		probeCalls++
		return 50, "r_frame_rate", nil
	}

	spec := ports.StreamSpec{
		SessionID: "warmup-fallback-1",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			TranscodeVideo: true,
			VideoCodec:     "h264",
			VideoCRF:       20,
			Preset:         "veryfast",
		},
		Source: ports.StreamSource{
			ID:   streamURL,
			Type: ports.SourceURL,
		},
	}

	adapter.setLastKnownFPS(fpsCacheKey(spec.Source, streamURL), 50)
	args, err := adapter.buildArgs(context.Background(), spec, streamURL)
	if err != nil {
		t.Fatalf("expected buildArgs success, got error: %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("expected warmup failure to fall back to fps probe, got %d calls", probeCalls)
	}
	if x264Params, ok := valueAfter(args, "-x264-params"); !ok || !strings.Contains(x264Params, "keyint=300:min-keyint=300:scenecut=0") {
		t.Fatalf("expected probed 50fps GOP params, got %q", x264Params)
	}
}
