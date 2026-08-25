// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// A receiver that accepts the TCP connection but never returns response headers must not pin
// the coalescing session for the transport's default header timeout. ConnectTimeout is the
// configured bound, and because the upstream body outlives the caller's context it can only be
// enforced on the transport - not by passing that context to the request.
func TestPipeline_UnresponsiveUpstream_HonoursConnectTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case accepted <- struct{}{}:
			default:
			}
			// Deliberately never write a response: hold the connection open.
			defer func() { _ = conn.Close() }()
		}
	}()

	const connectTimeout = 400 * time.Millisecond

	connectorCfg := DefaultConnectorConfig("http://"+listener.Addr().String(), 0)
	connectorCfg.StreamPort = listener.Addr().(*net.TCPAddr).Port
	connectorCfg.ConnectTimeout = connectTimeout

	connector := NewLivePipelineConnector(connectorCfg)

	start := time.Now()
	_, err = connector.Connect(context.Background(), session.NewSessionKey(
		"127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "1:0:19:1:3FB:1:C00000:0:0:0:"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected connect to fail against an upstream that never answers")
	}
	select {
	case <-accepted:
	default:
		t.Fatalf("precondition failed: upstream never accepted a connection")
	}
	// Generous ceiling: the point is that it is bounded by ConnectTimeout rather than by the
	// transport's previously hardcoded 15s header timeout.
	if elapsed > 5*time.Second {
		t.Fatalf("connect took %v; ConnectTimeout of %v was not enforced", elapsed, connectTimeout)
	}
}
