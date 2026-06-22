// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

// relayServerServing binds a server to 127.0.0.1:17999 so preflightTS classifies
// it as a stream-relay source (isStreamRelayURL gates on port 17999), serving body.
func relayServerServing(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	_ = srv.Listener.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:17999")
	if err != nil {
		t.Skipf("port 17999 unavailable for relay preflight test: %v", err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func newRelayPreflightAdapter() *LocalAdapter {
	return NewLocalAdapter("", "", "", nil, zerolog.New(io.Discard), "", "", 0, 0, true, 2*time.Second, 6, 0, 0, "")
}

// A clear (FTA/passthrough) relay source is clean from its first packet, so the
// clear-lead fast path must accept it after reading only the leading probe -- not
// the full ~752KB descrambler-lock window. This is the ~4s startup win.
func TestPreflightTS_RelayClearLead_FastPath(t *testing.T) {
	srv := relayServerServing(t, tsBuf(4096, false)) // 752KB, all clear
	res, err := newRelayPreflightAdapter().preflightTS(context.Background(), srv.URL)
	if err != nil || !res.OK {
		t.Fatalf("clear relay source must preflight OK, got ok=%v err=%v reason=%s", res.OK, err, res.Reason)
	}
	if res.Bytes > preflightRelayLeadProbeBytes {
		t.Fatalf("clear-lead fast path must stop at the lead probe (<=%d), read %d", preflightRelayLeadProbeBytes, res.Bytes)
	}
	if res.Bytes >= preflightRelayScanBytes {
		t.Fatalf("fast path must NOT drain the full window (%d), read %d", preflightRelayScanBytes, res.Bytes)
	}
}

// NEGATIVE CONTROL 1: a descrambling channel is scrambled at the head until the ECM
// locks, then clears. It must NOT trip the clear-lead fast path; it must drain the
// full window and be classified OK on the cleared trailing sample (the original bug
// this window guards against).
func TestPreflightTS_RelayScrambledLeadThenClear_ReadsFullWindow(t *testing.T) {
	body := append(tsBuf(2000, true), tsBuf(3000, false)...) // lock then clear, ~940KB
	srv := relayServerServing(t, body)
	res, err := newRelayPreflightAdapter().preflightTS(context.Background(), srv.URL)
	if err != nil || !res.OK {
		t.Fatalf("descrambling source (scrambled head, clear tail) must preflight OK, got ok=%v err=%v reason=%s", res.OK, err, res.Reason)
	}
	if res.Bytes < 188*2000 {
		t.Fatalf("scrambled head must defeat the fast path and drain past the lock (>=%d), read %d", 188*2000, res.Bytes)
	}
}

// NEGATIVE CONTROL 2: a genuinely scrambled channel (scrambled throughout) must still
// be flagged -- the fast path must never pass it.
func TestPreflightTS_RelayFullyScrambled_StillFlagged(t *testing.T) {
	srv := relayServerServing(t, tsBuf(4096, true)) // scrambled throughout
	res, err := newRelayPreflightAdapter().preflightTS(context.Background(), srv.URL)
	if err == nil || res.OK {
		t.Fatalf("fully scrambled source must be rejected, got ok=%v err=%v", res.OK, err)
	}
	if res.Reason != ports.PreflightReasonScrambled {
		t.Fatalf("expected scrambled reason, got %q (detail=%s)", res.Reason, res.Detail)
	}
}
