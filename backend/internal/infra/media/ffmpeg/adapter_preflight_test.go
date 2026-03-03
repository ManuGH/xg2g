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

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

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
	if !result.ok {
		t.Fatalf("expected preflight ok, got false (reason=%s)", result.reason)
	}
	if result.bytes < 188*3 {
		t.Fatalf("expected at least %d bytes, got %d", 188*3, result.bytes)
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
	if result.reason != "sync_miss" {
		t.Fatalf("expected sync_miss, got %q", result.reason)
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
	if result.reason != "short_read" {
		t.Fatalf("expected short_read, got %q", result.reason)
	}
}

func TestSelectStreamURL_FallbackOffFails(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (preflightResult, error) {
		calls++
		return preflightResult{ok: false, bytes: 0, reason: "sync_miss"}, errors.New("no ts")
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
	if calls != 1 {
		t.Fatalf("expected 1 preflight call, got %d", calls)
	}
}

func TestSelectStreamURL_NoFallbackWhenNotRelay(t *testing.T) {
	adapter := NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")

	calls := 0
	preflight := func(ctx context.Context, rawURL string) (preflightResult, error) {
		calls++
		return preflightResult{ok: false, bytes: 0, reason: "sync_miss"}, errors.New("no ts")
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
	preflight := func(ctx context.Context, rawURL string) (preflightResult, error) {
		calls++
		if strings.Contains(rawURL, ":17999") {
			return preflightResult{ok: false, bytes: 0, reason: "sync_miss"}, errors.New("no ts")
		}
		return preflightResult{ok: true, bytes: 188 * 3}, nil
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
	if !result.ok {
		t.Fatalf("expected preflight ok, got false")
	}
}
