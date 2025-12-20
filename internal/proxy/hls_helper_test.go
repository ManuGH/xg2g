// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestZapAndResolveStream_WaitsForStreamReady(t *testing.T) {
	oldZapDelay := zapDelay
	oldProbeTimeout := streamProbeTimeout
	oldProbeAttempt := streamProbeAttemptDur
	oldProbeRetry := streamProbeRetryDelay
	zapDelay = 0
	streamProbeTimeout = 500 * time.Millisecond
	streamProbeAttemptDur = 100 * time.Millisecond
	streamProbeRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() {
		zapDelay = oldZapDelay
		streamProbeTimeout = oldProbeTimeout
		streamProbeAttemptDur = oldProbeAttempt
		streamProbeRetryDelay = oldProbeRetry
	})

	var streamAttempts atomic.Int32
	streamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := streamAttempts.Add(1)
		if attempt < 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x47}) // MPEG-TS sync byte (enough to prove the port serves data)
	}))
	t.Cleanup(streamSrv.Close)

	webAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXTVLCOPT:program=108\n%s\n", streamSrv.URL)
	}))
	t.Cleanup(webAPISrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url, pid, err := ZapAndResolveStream(ctx, webAPISrv.URL+"/web/stream.m3u?ref=1:0:1")
	if err != nil {
		t.Fatalf("ZapAndResolveStream returned error: %v", err)
	}
	if url != streamSrv.URL {
		t.Fatalf("unexpected stream URL: got %q want %q", url, streamSrv.URL)
	}
	if pid != 108 {
		t.Fatalf("unexpected program id: got %d want %d", pid, 108)
	}
	if got := streamAttempts.Load(); got < 3 {
		t.Fatalf("expected stream readiness probes, got attempts=%d", got)
	}
}

func TestZapAndResolveStream_FailsWhenStreamNeverReady(t *testing.T) {
	oldZapDelay := zapDelay
	oldProbeTimeout := streamProbeTimeout
	oldProbeAttempt := streamProbeAttemptDur
	oldProbeRetry := streamProbeRetryDelay
	zapDelay = 0
	streamProbeTimeout = 150 * time.Millisecond
	streamProbeAttemptDur = 50 * time.Millisecond
	streamProbeRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() {
		zapDelay = oldZapDelay
		streamProbeTimeout = oldProbeTimeout
		streamProbeAttemptDur = oldProbeAttempt
		streamProbeRetryDelay = oldProbeRetry
	})

	streamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(streamSrv.Close)

	webAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "#EXTM3U\n%s\n", streamSrv.URL)
	}))
	t.Cleanup(webAPISrv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := ZapAndResolveStream(ctx, webAPISrv.URL+"/web/stream.m3u?ref=1:0:1")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

