// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// A real server for the iOS contract test to talk to.
//
// This is not a test of the backend — every assertion here already exists in
// device_auth_e2e_test.go. It exists so the *iOS* client can be exercised
// against the production router and real SQLite instead of against Swift
// doubles, which is the only way to catch the class of defect that doubles
// reproduce faithfully: a client and a server that agree with each other's
// mocks and disagree on the wire.
//
// It runs only when XG2G_IOS_CONTRACT_PORT is set, so `go test ./...` is
// unaffected. ios/scripts/run-contract-tests.sh starts it, runs the iOS suite
// against it, and shuts it down.
//
// # What is stubbed, and what that costs
//
// Exactly one thing: a request without a DPoP credential is treated as an
// admin. That stands in for the human who approves a pairing in the web UI —
// a step the iOS client cannot and must not perform for itself. Every request
// the client makes with its own credential runs the real authentication path,
// which is the part under test. Nothing else is faked: same router, same
// middleware chain, same identity service, same SQLite schema.
func TestIOSContractServer(t *testing.T) {
	rawPort := os.Getenv("XG2G_IOS_CONTRACT_PORT")
	if rawPort == "" {
		t.Skip("XG2G_IOS_CONTRACT_PORT not set; this is the iOS contract fixture, not a backend test")
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("XG2G_IOS_CONTRACT_PORT must be a valid port, got %q", rawPort)
	}

	e := newDeviceAuthE2E(t)

	listener, err := net.Listen("tcp", "127.0.0.1:"+rawPort)
	if err != nil {
		t.Fatalf("listen on 127.0.0.1:%d: %v", port, err)
	}

	// The client sends X-Forwarded-Proto over plain HTTP on loopback: the
	// simulator reaches the host on 127.0.0.1, and terminating TLS here would
	// test the trust store rather than the auth contract.
	server := &http.Server{Handler: e.handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	lifetime := 10 * time.Minute
	if raw := os.Getenv("XG2G_IOS_CONTRACT_TTL_SECONDS"); raw != "" {
		if seconds, convErr := strconv.Atoi(raw); convErr == nil && seconds > 0 {
			lifetime = time.Duration(seconds) * time.Second
		}
	}

	t.Logf("iOS contract server listening on http://127.0.0.1:%d for %s", port, lifetime)

	// Bounded on purpose: an abandoned run must not leave a server holding a
	// port and a temp database until the machine is rebooted.
	<-time.After(lifetime)
	t.Log("iOS contract server lifetime elapsed; shutting down")
}
