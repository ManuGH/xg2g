// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// scrambleVideoPID returns a copy of a clear MPEG-TS capture with transport_scrambling_control
// set on every packet of the given PID, reproducing exactly what the receiver emits on
// port 8001 when no softcam is descrambling the service. PAT and PMT stay clear, as they do
// on the wire, so program selection still succeeds and only the payload is opaque.
func scrambleVideoPID(t *testing.T, data []byte, pid uint16) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	scrambled := 0
	for off := 0; off+ring.TSPacketSize <= len(out); off += ring.TSPacketSize {
		if out[off] != 0x47 {
			t.Fatalf("capture is not %d-byte aligned at offset %d", ring.TSPacketSize, off)
		}
		pktPID := (uint16(out[off+1]&0x1F) << 8) | uint16(out[off+2])
		if pktPID != pid {
			continue
		}
		out[off+3] = (out[off+3] &^ 0xC0) | 0x80
		scrambled++
	}
	if scrambled == 0 {
		t.Fatalf("no packets found on video PID %d; fixture cannot exercise the scrambled path", pid)
	}
	return out
}

// videoPIDOf discovers the video PID with the production PSI parser rather than hardcoding it,
// so the fixture stays correct if the capture is ever replaced.
func videoPIDOf(t *testing.T, data []byte) uint16 {
	t.Helper()
	probe := ring.NewMasterRing(len(data) + ring.TSPacketSize)
	defer probe.Close()
	if _, err := probe.Push(context.Background(), data); err != nil {
		t.Fatalf("probe push failed: %v", err)
	}
	pid, _ := probe.VideoDetails()
	if pid == 0 {
		t.Fatalf("failed to discover video PID from capture")
	}
	return pid
}

// A scrambled upstream can never yield a keyframe, so the live route must reject it promptly
// with an error naming the cause, instead of stalling for the full attach budget and then
// reporting a generic attach failure that sends the operator hunting through xg2g.
func TestPipeline_ScrambledUpstream_FailsFastWithDiagnosis(t *testing.T) {
	root := findProjectRoot(t)
	clear, err := os.ReadFile(filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts"))
	if err != nil {
		t.Fatalf("read capture failed: %v", err)
	}

	payload := scrambleVideoPID(t, clear, videoPIDOf(t, clear))

	// Same pacing as the clear-stream end-to-end test, so the only variable is the
	// transport_scrambling_control field.
	connectorCfg := DefaultConnectorConfig("http://127.0.0.1", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 50.0
	connectorCfg.NormConfig.PacerIntervalMs = 5.0
	connectorCfg.NormConfig.InitialBitrateKbps = 20000.0
	connectorCfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	connector := NewLivePipelineConnector(connectorCfg)
	manager := session.NewManager(session.DefaultManagerConfig(), connector)
	defer func() { _ = manager.Close() }()

	server := httptest.NewServer(NewHandlerWithReceiver(manager, "127.0.0.1", 8001))
	defer server.Close()

	start := time.Now()
	// Field 4 of the service reference is the DVB program number in hex, which the handler
	// uses to select the PMT. The capture carries program 1, so the reference must say 1.
	resp, err := http.Get(server.URL + "/api/v3/stream/live/1:0:19:1:3FB:1:C00000:0:0:0:")
	if err != nil {
		t.Fatalf("live request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected HTTP 502 for a scrambled upstream, got %d (body: %q)", resp.StatusCode, string(body))
	}
	if !strings.Contains(strings.ToLower(string(body)), "scrambled") {
		t.Fatalf("response must name the cause so the receiver is the obvious place to look, got %q", string(body))
	}
	if elapsed >= primedAttachTimeout {
		t.Fatalf("scrambled upstream is terminal and must fail fast, but took %v (attach budget %v)", elapsed, primedAttachTimeout)
	}
}
