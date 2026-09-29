// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartTranscoder_Lifecycle(t *testing.T) {
	// Simple HTTP server emitting dummy bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid ts, but enough for ffmpeg input probe"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader, err := StartTranscoder(ctx, server.URL, "hevc", "TestAgent")
	if err != nil {
		t.Fatalf("unexpected StartTranscoder error: %v", err)
	}

	// Read or immediately close to ensure clean process termination
	time.Sleep(50 * time.Millisecond)
	if err := reader.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}
