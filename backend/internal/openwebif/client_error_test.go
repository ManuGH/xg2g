// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package openwebif

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
	if _, err := c.Services(context.Background(), "1:0:0"); err == nil {
		t.Fatal("expected error on transport timeout")
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

func TestServices_FallbackToFlatOnParseMismatch(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/getservices":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"services":"invalid-shape"}`))
		case "/api/getallservices":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"services":[
					{
						"servicename":"Container",
						"servicereference":"1:7:1:0:0:0:0:0:0:0:",
						"subservices":[
							{"servicename":"BBC One","servicereference":"1:0:1:100:200:300:0:0:0:0:"}
						]
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := newTestClient(s.URL)
	svcs, err := c.Services(context.Background(), "1:0:0")
	if err != nil {
		t.Fatalf("expected fallback success, got error: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service from flat fallback, got %d", len(svcs))
	}
	if svcs[0][0] != "BBC One" {
		t.Fatalf("unexpected service name %q", svcs[0][0])
	}
}

func TestServices_CapabilityCachedPerHost(t *testing.T) {
	var nestedCalls atomic.Int64
	var flatCalls atomic.Int64

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/getservices":
			nestedCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"services":"invalid-shape"}`))
		case "/api/getallservices":
			flatCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"services":[
					{
						"servicename":"Container",
						"servicereference":"1:7:1:0:0:0:0:0:0:0:",
						"subservices":[
							{"servicename":"BBC One","servicereference":"1:0:1:100:200:300:0:0:0:0:"}
						]
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	c := newTestClient(s.URL)

	if _, err := c.Services(context.Background(), "1:0:0"); err != nil {
		t.Fatalf("first services call failed: %v", err)
	}
	if _, err := c.Services(context.Background(), "1:0:1"); err != nil {
		t.Fatalf("second services call failed: %v", err)
	}

	if got := nestedCalls.Load(); got != 1 {
		t.Fatalf("expected nested endpoint called once, got %d", got)
	}
	if got := flatCalls.Load(); got != 2 {
		t.Fatalf("expected flat endpoint called twice, got %d", got)
	}
}
