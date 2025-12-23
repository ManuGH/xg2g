// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTuner_Tune_Success(t *testing.T) {
	var polls int32
	targetRef := "1:0:1:123:0"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/zap":
			fmt.Fprintln(w, `{"result": true}`)
		case "/api/getcurrent":
			// First poll returns wrong ref, second returns correct
			count := atomic.AddInt32(&polls, 1)
			ref := "1:0:0:0:0"
			if count > 1 {
				ref = targetRef
			}
			fmt.Fprintf(w, `{"result": true, "info": {"ref": "%s"}}`+"\n", ref)
		case "/api/signal":
			// Lock on 3rd poll
			count := atomic.LoadInt32(&polls)
			locked := count > 2
			fmt.Fprintf(w, `{"result": true, "lock": %v, "snr": 80}`+"\n", locked)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, 1*time.Second)
	tuner := NewTuner(client, 0)
	tuner.PollInterval = 10 * time.Millisecond // Fast poll

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := tuner.Tune(ctx, targetRef)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, atomic.LoadInt32(&polls), int32(3))
}

func TestTuner_Tune_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/zap":
			fmt.Fprintln(w, `{"result": true}`)
		case "/api/getcurrent":
			fmt.Fprintln(w, `{"result": true, "info": {"ref": "1:0:0:0:0"}}`) // Wrong Ref
		case "/api/signal":
			fmt.Fprintln(w, `{"result": true, "lock": false}`)
		}
	}))
	defer ts.Close()

	client := NewClient(ts.URL, 1*time.Second)
	tuner := NewTuner(client, 0)
	tuner.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := tuner.Tune(ctx, "1:0:1:TARGET:0")
	assert.ErrorIs(t, err, ErrReadyTimeout)
}
