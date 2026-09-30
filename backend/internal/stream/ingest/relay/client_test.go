// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/admission"
)

type mockLease struct {
	accountID string
	sourceID  string
	clientID  string
	isShared  bool
	released  atomic.Bool
}

var _ admission.SlotLease = (*mockLease)(nil)

func (m *mockLease) AccountID() string { return m.accountID }
func (m *mockLease) SourceID() string  { return m.sourceID }
func (m *mockLease) ClientID() string  { return m.clientID }
func (m *mockLease) IsShared() bool    { return m.isShared }
func (m *mockLease) Release() error {
	m.released.Store(true)
	return nil
}

func TestRelayClient_FetchSuccess(t *testing.T) {
	expectedToken := "secret-relay-token-xyz"
	expectedID := "opaque-channel-123"
	expectedTarget := "http://provider.upstream/stream.ts"
	payload := "test-mpegts-stream-bytes"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Relay-Token") != expectedToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Target-URI") != expectedTarget {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("id") != expectedID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		RelayBaseURL:   server.URL,
		AuthToken:      expectedToken,
		SelfAddresses:  []string{"10.10.55.14:8089", "xg2g.home.matrixcentral.de"},
		ConnectTimeout: time.Second,
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	lease := &mockLease{
		accountID: "acc-1",
		sourceID:  expectedID,
		clientID:  "client-1",
	}

	reader, err := client.Fetch(context.Background(), lease, expectedID, expectedTarget)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != payload {
		t.Fatalf("expected payload %q, got %q", payload, string(data))
	}

	// Verify closing reader releases the slot lease
	if err := reader.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !lease.released.Load() {
		t.Fatalf("expected lease to be released upon reader closure")
	}
}

func TestRelayClient_MissingLeaseRejection(t *testing.T) {
	client, err := NewClient(ClientConfig{
		RelayBaseURL: "http://10.10.55.64:8085",
		AuthToken:    "token",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// 1. Nil lease
	_, err = client.Fetch(context.Background(), nil, "src-1", "http://provider.test/stream")
	if !errors.Is(err, ErrMissingSlotLease) {
		t.Fatalf("expected ErrMissingSlotLease for nil lease, got: %v", err)
	}

	// 2. Mismatched source ID
	wrongLease := &mockLease{sourceID: "src-different"}
	_, err = client.Fetch(context.Background(), wrongLease, "src-1", "http://provider.test/stream")
	if !errors.Is(err, ErrMissingSlotLease) {
		t.Fatalf("expected ErrMissingSlotLease for mismatched lease, got: %v", err)
	}
}

func TestRelayClient_LoopbackRecursionRejection(t *testing.T) {
	client, err := NewClient(ClientConfig{
		RelayBaseURL:  "http://10.10.55.64:8085",
		AuthToken:     "token",
		SelfAddresses: []string{"10.10.55.14:8089", "xg2g.home.matrixcentral.de", "localhost"},
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	lease := &mockLease{sourceID: "src-loop"}

	// Target points back to xg2g's IP / port
	loopTargets := []string{
		"http://10.10.55.14:8089/api/v3/stream/smooth/relay/abc",
		"http://10.10.55.14/api/v3/stream",
		"https://xg2g.home.matrixcentral.de/stream",
		"http://localhost:8089/stream",
	}

	for _, target := range loopTargets {
		_, err := client.Fetch(context.Background(), lease, "src-loop", target)
		if !errors.Is(err, ErrRecursiveRelayTarget) {
			t.Errorf("target %s: expected ErrRecursiveRelayTarget, got %v", target, err)
		}
	}
}

func TestRelayClient_UnauthorizedAndUnavailable(t *testing.T) {
	// Unauthorized server
	server401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server401.Close()

	client401, _ := NewClient(ClientConfig{RelayBaseURL: server401.URL, AuthToken: "wrong"})
	lease := &mockLease{sourceID: "src-1"}

	_, err := client401.Fetch(context.Background(), lease, "src-1", "http://provider.test/s")
	if !errors.Is(err, ErrRelayUnauthorized) {
		t.Fatalf("expected ErrRelayUnauthorized, got %v", err)
	}

	// Unavailable server (503)
	server503 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server503.Close()

	client503, _ := NewClient(ClientConfig{RelayBaseURL: server503.URL, AuthToken: "token"})
	_, err = client503.Fetch(context.Background(), lease, "src-1", "http://provider.test/s")
	if !errors.Is(err, ErrRelayUnavailable) {
		t.Fatalf("expected ErrRelayUnavailable, got %v", err)
	}
}
