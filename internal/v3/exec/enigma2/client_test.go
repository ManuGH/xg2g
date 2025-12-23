// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Zap(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/zap", r.URL.Path)
		assert.Equal(t, "1:0:1:123:0:0:0:0:0:0:", r.URL.Query().Get("sRef"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"result": true, "message": "zap done"}`)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, 1*time.Second)
	err := c.Zap(context.Background(), "1:0:1:123:0:0:0:0:0:0:")
	require.NoError(t, err)
}

func TestClient_GetCurrent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/getcurrent", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{
			"result": true,
			"info": {
				"ref": "1:0:1:123:0:0:0:0:0:0:",
				"name": "Test Channel",
				"provider": "Test Provider"
			}
		}`)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, 1*time.Second)
	info, err := c.GetCurrent(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1:0:1:123:0:0:0:0:0:0:", info.Info.ServiceReference)
	assert.Equal(t, "Test Channel", info.Info.Name)
}

func TestClient_GetSignal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/signal", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"result": true, "snr": 85, "agc": 90, "ber": 0, "lock": true}`)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, 1*time.Second)
	sig, err := c.GetSignal(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 85, sig.Snr)
	assert.True(t, sig.Locked)
}
