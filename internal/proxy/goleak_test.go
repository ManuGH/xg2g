// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/goleak"
)

func TestProxy_StartShutdown_NoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	srv, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     backend.URL,
		Logger:        zerolog.New(io.Discard),
		AuthAnonymous: true,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	time.Sleep(50 * time.Millisecond)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start() didn't return after Shutdown()")
	}
}
