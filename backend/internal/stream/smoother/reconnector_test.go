// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestResilientUpstreamReader_Normal(t *testing.T) {
	expectedData := []byte("hello stream world 1234567890")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(expectedData)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := ResilientConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	reader := NewResilientUpstreamReader(
		ctx,
		server.Client(),
		server.URL,
		nil,
		nil, // initialBody nil -> auto-dials
		cfg,
	)
	defer reader.Close()

	buf := make([]byte, len(expectedData))
	n, err := io.ReadFull(reader, buf)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n != len(expectedData) {
		t.Fatalf("read %d bytes, expected %d", n, len(expectedData))
	}
	if !bytes.Equal(buf, expectedData) {
		t.Fatalf("content mismatch")
	}
	if reader.ReconnectCount != 0 {
		t.Fatalf("expected 0 reconnects, got %d", reader.ReconnectCount)
	}
}

func TestResilientUpstreamReader_TransparentReconnect(t *testing.T) {
	var requestCount int32
	chunk1 := []byte("PART_1_BEFORE_DISCONNECT_")
	chunk2 := []byte("PART_2_AFTER_RECONNECT")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum := atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		if reqNum == 1 {
			// First connection: write chunk 1, then simulate disconnect (abrupt close / EOF)
			_, _ = w.Write(chunk1)
			return
		}

		// Second connection: write chunk 2
		_, _ = w.Write(chunk2)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := ResilientConfig{
		MaxRetries:     3,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	reader := NewResilientUpstreamReader(
		ctx,
		server.Client(),
		server.URL,
		nil,
		nil,
		cfg,
	)
	defer reader.Close()

	// Consumer reads continuously
	expectedFull := append(chunk1, chunk2...)
	received := make([]byte, len(expectedFull))

	n, err := io.ReadFull(reader, received)
	if err != nil {
		t.Fatalf("unexpected read error during transparent reconnect: %v", err)
	}
	if n != len(expectedFull) {
		t.Fatalf("expected %d bytes, got %d", len(expectedFull), n)
	}
	if !bytes.Equal(received, expectedFull) {
		t.Fatalf("content mismatch: got %q, expected %q", string(received), string(expectedFull))
	}

	if reader.ReconnectCount != 1 {
		t.Fatalf("expected exactly 1 transparent reconnect, got %d", reader.ReconnectCount)
	}
}

func TestResilientUpstreamReader_TransientHttp503Recovery(t *testing.T) {
	var requestCount int32
	validData := []byte("RECOVERED_STREAM_PAYLOAD")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum := atomic.AddInt32(&requestCount, 1)
		if reqNum == 1 {
			// First attempt returns 503
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Subsequent attempt succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validData)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := ResilientConfig{
		MaxRetries:     3,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
	}

	reader := NewResilientUpstreamReader(
		ctx,
		server.Client(),
		server.URL,
		nil,
		nil,
		cfg,
	)
	defer reader.Close()

	received := make([]byte, len(validData))
	n, err := io.ReadFull(reader, received)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n != len(validData) || !bytes.Equal(received, validData) {
		t.Fatalf("expected %q, got %q", string(validData), string(received))
	}
}

func TestResilientUpstreamReader_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := ResilientConfig{
		MaxRetries:     2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
	}

	reader := NewResilientUpstreamReader(
		ctx,
		server.Client(),
		server.URL,
		nil,
		nil,
		cfg,
	)
	defer reader.Close()

	buf := make([]byte, 100)
	_, err := reader.Read(buf)
	if err == nil {
		t.Fatalf("expected error on permanent failure, got nil")
	}
}

func TestResilientUpstreamReader_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server stays quiet
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	cfg := ResilientConfig{
		MaxRetries:     5,
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
	}

	reader := NewResilientUpstreamReader(
		ctx,
		server.Client(),
		server.URL,
		nil,
		nil,
		cfg,
	)
	defer reader.Close()

	// Cancel context after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	buf := make([]byte, 100)
	_, err := reader.Read(buf)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
