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

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/rs/zerolog"
)

func TestPreflightTranscodeProfiles_PublishesMeasuredProfileBenchmarks(t *testing.T) {
	adapter := NewLocalAdapter("ffmpeg", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128")
	adapter.vaapiEncoders = map[string]bool{"h264_vaapi": true}
	adapter.nvencEncoders = map[string]bool{"h264_nvenc": true}
	adapter.profileProbeFn = func(_ context.Context, req profileProbeRequest) (time.Duration, error) {
		switch req.Backend + ":" + req.ProfileID {
		case "cpu:" + playbackprofile.BenchmarkProfileAudioAACStereo:
			return 35 * time.Millisecond, nil
		case "cpu:" + playbackprofile.BenchmarkProfileVideoH2641080P:
			return 220 * time.Millisecond, nil
		case "cpu:" + playbackprofile.BenchmarkProfileVideoH2641080I:
			return 360 * time.Millisecond, nil
		case "cpu:" + playbackprofile.BenchmarkProfileVideoH2641080I50:
			return 520 * time.Millisecond, nil
		case "vaapi:" + playbackprofile.BenchmarkProfileVideoH2641080P:
			return 80 * time.Millisecond, nil
		case "vaapi:" + playbackprofile.BenchmarkProfileVideoH2641080I:
			return 170 * time.Millisecond, nil
		case "vaapi:" + playbackprofile.BenchmarkProfileVideoH2641080I50:
			return 260 * time.Millisecond, nil
		case "vaapi:" + playbackprofile.BenchmarkProfileVideoH2642160P:
			return 410 * time.Millisecond, nil
		case "vaapi:" + playbackprofile.BenchmarkProfileVideoH2642160P50:
			return 760 * time.Millisecond, nil
		case "nvenc:" + playbackprofile.BenchmarkProfileVideoH2641080P:
			return 95 * time.Millisecond, nil
		case "nvenc:" + playbackprofile.BenchmarkProfileVideoH2641080I:
			return 210 * time.Millisecond, nil
		case "nvenc:" + playbackprofile.BenchmarkProfileVideoH2641080I50:
			return 240 * time.Millisecond, nil
		case "nvenc:" + playbackprofile.BenchmarkProfileVideoH2642160P:
			return 330 * time.Millisecond, nil
		case "nvenc:" + playbackprofile.BenchmarkProfileVideoH2642160P50:
			return 610 * time.Millisecond, nil
		case "cpu:" + playbackprofile.BenchmarkProfileVideoH2642160P:
			return 780 * time.Millisecond, nil
		case "cpu:" + playbackprofile.BenchmarkProfileVideoH2642160P50:
			return 1400 * time.Millisecond, nil
		default:
			return 0, errors.New("unexpected profile probe")
		}
	}

	hardware.SetCPUProfileBenchmarks(nil)
	hardware.SetVAAPIProfileBenchmarks(nil)
	hardware.SetNVENCProfileBenchmarks(nil)
	t.Cleanup(func() {
		hardware.SetCPUProfileBenchmarks(nil)
		hardware.SetVAAPIProfileBenchmarks(nil)
		hardware.SetNVENCProfileBenchmarks(nil)
	})

	adapter.PreflightTranscodeProfiles()

	cpuCap, cpuBackend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileVideoH2641080P)
	if !ok {
		t.Fatal("expected published 1080p profile benchmark")
	}
	if cpuBackend != "vaapi" || cpuCap.ProbeElapsed != 80*time.Millisecond {
		t.Fatalf("expected fastest measured backend to win for 1080p, got backend=%q cap=%#v", cpuBackend, cpuCap)
	}

	interlacedCap, interlacedBackend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileVideoH2641080I)
	if !ok {
		t.Fatal("expected published 1080i profile benchmark")
	}
	if interlacedBackend != "vaapi" || interlacedCap.ProbeElapsed != 170*time.Millisecond {
		t.Fatalf("expected fastest measured backend to win for 1080i, got backend=%q cap=%#v", interlacedBackend, interlacedCap)
	}

	interlaced50Cap, interlaced50Backend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileVideoH2641080I50)
	if !ok {
		t.Fatal("expected published 1080i50 profile benchmark")
	}
	if interlaced50Backend != "nvenc" || interlaced50Cap.ProbeElapsed != 240*time.Millisecond {
		t.Fatalf("expected fastest measured backend to win for 1080i50, got backend=%q cap=%#v", interlaced50Backend, interlaced50Cap)
	}

	audioCap, audioBackend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileAudioAACStereo)
	if !ok {
		t.Fatal("expected published audio profile benchmark")
	}
	if audioBackend != "cpu" || audioCap.ProbeElapsed != 35*time.Millisecond {
		t.Fatalf("expected cpu audio profile benchmark, got backend=%q cap=%#v", audioBackend, audioCap)
	}

	uhdCap, uhdBackend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileVideoH2642160P)
	if !ok {
		t.Fatal("expected published 2160p profile benchmark")
	}
	if uhdBackend != "nvenc" || uhdCap.ProbeElapsed != 330*time.Millisecond {
		t.Fatalf("expected fastest measured backend to win for 2160p, got backend=%q cap=%#v", uhdBackend, uhdCap)
	}

	uhd50Cap, uhd50Backend, ok := hardware.HardwareProfileCapabilityFor(playbackprofile.BenchmarkProfileVideoH2642160P50)
	if !ok {
		t.Fatal("expected published 2160p50 profile benchmark")
	}
	if uhd50Backend != "nvenc" || uhd50Cap.ProbeElapsed != 610*time.Millisecond {
		t.Fatalf("expected fastest measured backend to win for 2160p50, got backend=%q cap=%#v", uhd50Backend, uhd50Cap)
	}
}

func TestPreflightPathCorrectness_PublishesMeasuredPathTruth(t *testing.T) {
	adapter := NewLocalAdapter("ffmpeg", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "/dev/dri/renderD128")
	adapter.vaapiEncoders = map[string]bool{
		"hevc_vaapi": true,
		"av1_vaapi":  true,
	}
	adapter.pathProbeFn = func(_ context.Context, req pathProbeRequest) (hardware.HardwarePathCapability, error) {
		switch req.PathID {
		case hardware.PathVAAPIFullInterlacedHEVC:
			return hardware.HardwarePathCapability{
				Verified: true,
				Status:   hardware.PathStatusVerified,
				Reason:   "synthetic yavg 118.2",
			}, nil
		case hardware.PathVAAPIEncodeOnlyInterlacedHEVC:
			return hardware.HardwarePathCapability{
				Verified: true,
				Status:   hardware.PathStatusVerified,
				Reason:   "synthetic yavg 121.7",
			}, nil
		case hardware.PathVAAPIEncodeOnlyInterlacedAV1:
			return hardware.HardwarePathCapability{
				Verified: true,
				Status:   hardware.PathStatusVerified,
				Reason:   "synthetic yavg 119.1",
			}, nil
		default:
			return hardware.HardwarePathCapability{}, errors.New("unexpected path probe")
		}
	}

	hardware.SetPathCapabilities(nil)
	t.Cleanup(func() {
		hardware.SetPathCapabilities(nil)
	})

	adapter.PreflightPathCorrectness()

	hevcCap, ok := hardware.HardwarePathCapabilityFor(hardware.PathVAAPIFullInterlacedHEVC)
	if !ok {
		t.Fatal("expected published hevc path correctness")
	}
	if !hevcCap.Verified || hevcCap.Status != hardware.PathStatusVerified {
		t.Fatalf("unexpected hevc path capability: %#v", hevcCap)
	}

	hevcEncodeOnlyCap, ok := hardware.HardwarePathCapabilityFor(hardware.PathVAAPIEncodeOnlyInterlacedHEVC)
	if !ok {
		t.Fatal("expected published hevc encode-only path correctness")
	}
	if !hevcEncodeOnlyCap.Verified || hevcEncodeOnlyCap.Status != hardware.PathStatusVerified {
		t.Fatalf("unexpected hevc encode-only path capability: %#v", hevcEncodeOnlyCap)
	}

	if av1Cap, ok := hardware.HardwarePathCapabilityFor(hardware.PathVAAPIFullInterlacedAV1); ok {
		t.Fatalf("unexpected full av1 path capability for intentionally unused path: %#v", av1Cap)
	}

	av1EncodeOnlyCap, ok := hardware.HardwarePathCapabilityFor(hardware.PathVAAPIEncodeOnlyInterlacedAV1)
	if !ok {
		t.Fatal("expected published av1 encode-only path correctness")
	}
	if !av1EncodeOnlyCap.Verified || av1EncodeOnlyCap.Status != hardware.PathStatusVerified {
		t.Fatalf("unexpected av1 encode-only path capability: %#v", av1EncodeOnlyCap)
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
	if calls != 3 {
		t.Fatalf("expected 3 preflight calls, got %d", calls)
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
	if calls != 3 {
		t.Fatalf("expected 3 preflight calls, got %d", calls)
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
			return ports.NewPreflightResult("sync_miss", http.StatusOK, 188*3, 0, 17999), errors.New("no ts")
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

func TestSelectStreamURL_RetriesTransientRelayShortReadBeforeFallback(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	origRetryDelay := preflightRetryDelay
	preflightRetryDelay = time.Millisecond
	t.Cleanup(func() {
		preflightRetryDelay = origRetryDelay
	})

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		calls++
		if calls == 1 {
			return ports.NewPreflightResult("short_read", http.StatusOK, 0, 0, 17999), errors.New("short read")
		}
		return ports.NewSuccessfulPreflightResult(188*3, 0, 17999), nil
	}

	got, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-retry-relay",
		serviceRef,
		resolved,
		preflight,
	)
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if got != resolved {
		t.Fatalf("expected resolved url %q, got %q", resolved, got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 preflight calls, got %d", calls)
	}
}

func TestSelectStreamURL_RetriesTransient8001Fallback(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	origRetryDelay := preflightRetryDelay
	preflightRetryDelay = time.Millisecond
	t.Cleanup(func() {
		preflightRetryDelay = origRetryDelay
	})

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	expectedFallback := "http://127.0.0.1:8001/" + serviceRef

	relayCalls := 0
	fallbackCalls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		switch {
		case strings.Contains(rawURL, ":17999"):
			relayCalls++
			return ports.NewPreflightResult("short_read", http.StatusOK, 0, 0, 17999), errors.New("short read")
		case strings.Contains(rawURL, ":8001"):
			fallbackCalls++
			if fallbackCalls == 1 {
				return ports.NewPreflightResult("short_read", http.StatusOK, 28, 0, 8001), errors.New("short body")
			}
			return ports.NewSuccessfulPreflightResult(188*3, 0, 8001), nil
		default:
			t.Fatalf("unexpected preflight url %q", rawURL)
			return ports.PreflightResult{}, nil
		}
	}

	got, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-retry-fallback",
		serviceRef,
		resolved,
		preflight,
	)
	if err != nil {
		t.Fatalf("expected fallback retry success, got error: %v", err)
	}
	if got != expectedFallback {
		t.Fatalf("expected fallback url %q, got %q", expectedFallback, got)
	}
	if relayCalls != preflightMaxTries {
		t.Fatalf("expected %d relay preflight calls, got %d", preflightMaxTries, relayCalls)
	}
	if fallbackCalls != 2 {
		t.Fatalf("expected 2 fallback preflight calls, got %d", fallbackCalls)
	}
}

func TestSelectStreamURL_Extends8001WarmupAfterRepeatedShortReads(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	origRetryDelay := preflightRetryDelay
	preflightRetryDelay = time.Millisecond
	t.Cleanup(func() {
		preflightRetryDelay = origRetryDelay
	})

	serviceRef := "1:0:19:EF75:3F9:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	expectedFallback := "http://127.0.0.1:8001/" + serviceRef

	relayCalls := 0
	fallbackCalls := 0
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		switch {
		case strings.Contains(rawURL, ":17999"):
			relayCalls++
			return ports.NewPreflightResult("short_read", http.StatusOK, 0, 0, 17999), errors.New("relay warming")
		case strings.Contains(rawURL, ":8001"):
			fallbackCalls++
			if fallbackCalls < 6 {
				return ports.NewPreflightResult("short_read", http.StatusOK, 28, 0, 8001), errors.New("direct stream warming")
			}
			return ports.NewSuccessfulPreflightResult(188*3, 0, 8001), nil
		default:
			t.Fatalf("unexpected preflight url %q", rawURL)
			return ports.PreflightResult{}, nil
		}
	}

	got, err := adapter.selectStreamURLWithPreflight(
		context.Background(),
		"sid-extended-8001-warmup",
		serviceRef,
		resolved,
		preflight,
	)
	if err != nil {
		t.Fatalf("expected extended 8001 warmup success, got error: %v", err)
	}
	if got != expectedFallback {
		t.Fatalf("expected fallback url %q, got %q", expectedFallback, got)
	}
	if relayCalls != preflightMaxTries {
		t.Fatalf("expected %d relay preflight calls, got %d", preflightMaxTries, relayCalls)
	}
	if fallbackCalls != 6 {
		t.Fatalf("expected 6 fallback preflight calls, got %d", fallbackCalls)
	}
}

func TestSelectStreamURL_FallbackFailedAllStructuredResult(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	serviceRef := "1:0:19:2B66:3F3:1:C00000:0:0:0:"
	resolved := "http://127.0.0.1:17999/" + serviceRef
	preflight := func(ctx context.Context, rawURL string) (ports.PreflightResult, error) {
		return ports.NewPreflightResult("sync_miss", http.StatusOK, 188*3, 0, 17999), errors.New("no ts")
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
	if x264Params, ok := valueAfter(args, "-x264-params"); !ok || !strings.Contains(x264Params, "keyint=150:min-keyint=150:scenecut=0") {
		t.Fatalf("expected cached 25fps output GOP params, got %q", x264Params)
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
	if x264Params, ok := valueAfter(args, "-x264-params"); !ok || !strings.Contains(x264Params, "keyint=150:min-keyint=150:scenecut=0") {
		t.Fatalf("expected probed 25fps output GOP params, got %q", x264Params)
	}
}
