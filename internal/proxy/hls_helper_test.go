// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testReadyChecker struct {
	waitReadyFunc func(ctx context.Context, ref string) error
	calls         int
}

func (m *testReadyChecker) WaitReady(ctx context.Context, ref string) error {
	m.calls++
	if m.waitReadyFunc != nil {
		return m.waitReadyFunc(ctx, ref)
	}
	return nil
}

func (t *testReadyChecker) CheckInvariant(ctx context.Context, serviceRef string) error {
	return nil
}

func TestZapAndResolveStream_WaitsForStreamReady(t *testing.T) {
	// Setup WebAPI server to return stream info
	webAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXTVLCOPT:program=108\nhttp://example.com/stream\n")
	}))
	t.Cleanup(webAPISrv.Close)

	ctx := context.Background()
	checker := &testReadyChecker{}

	url, pid, err := ZapAndResolveStream(ctx, webAPISrv.URL+"/web/stream.m3u?ref=1:0:1", "1:0:1", checker)
	if err != nil {
		t.Fatalf("ZapAndResolveStream returned error: %v", err)
	}
	if url != "http://example.com/stream" {
		t.Fatalf("unexpected stream URL: got %q want %q", url, "http://example.com/stream")
	}
	if pid != 108 {
		t.Fatalf("unexpected program id: got %d want %d", pid, 108)
	}
	if checker.calls == 0 {
		t.Fatal("expected ReadyChecker.WaitReady to be called")
	}
}

func TestZapAndResolveStream_FailsWhenStreamNeverReady(t *testing.T) {
	webAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "#EXTM3U\nhttp://example.com/stream\n")
	}))
	t.Cleanup(webAPISrv.Close)

	ctx := context.Background()
	checker := &testReadyChecker{
		waitReadyFunc: func(ctx context.Context, ref string) error {
			return fmt.Errorf("timeout")
		},
	}

	_, _, err := ZapAndResolveStream(ctx, webAPISrv.URL+"/web/stream.m3u?ref=1:0:1", "1:0:1", checker)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "readiness check failed: timeout" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
