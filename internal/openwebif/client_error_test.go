// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package openwebif

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(base string) *Client {
	c := New(base)
	c.http = &http.Client{Timeout: 500 * time.Millisecond}
	return c
}

func TestClientBouquets5xx(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	if _, err := c.Bouquets(context.Background()); err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestClientBouquetsInvalidJSON(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	if _, err := c.Bouquets(context.Background()); err == nil {
		t.Fatal("expected json error")
	}
}

func TestClientGetServicesTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	svcs, err := c.Services(context.Background(), "1:0:0")
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if len(svcs) != 0 {
		t.Fatalf("expected zero services on timeout, got %d", len(svcs))
	}
}

func TestClientGetServicesBouquetNotFoundSchemaOK(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"services": []map[string]string{}})
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	svcs, err := c.Services(context.Background(), "1:0:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 0 {
		t.Fatalf("expected 0 services, got %d", len(svcs))
	}
}
