// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newHandlerUnderTest(t *testing.T, recv *fakeReceiver, cfg PreparationConfig) *PrepareHandler {
	t.Helper()
	pm, _ := newPrepManager(t, recv, cfg)
	return NewPrepareHandler(pm, "127.0.0.1", 8001)
}

func do(t *testing.T, h *PrepareHandler, method, path, clientID string) (int, prepareResponse) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if clientID != "" {
		req.Header.Set(clientIDHeader, clientID)
	}
	req.Header.Set(zapIDHeader, "zap-http")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body prepareResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func startPreparation(t *testing.T, h *PrepareHandler, ref, clientID string) prepareResponse {
	t.Helper()
	path := "/api/v3/stream/prepare?sref=" + url.QueryEscape(ref)
	code, body := do(t, h, http.MethodPost, path, clientID)
	if code != http.StatusAccepted {
		t.Fatalf("start: want 202, got %d (%+v)", code, body)
	}
	if body.PreparationID == "" {
		t.Fatal("start must return a preparation id")
	}
	return body
}

func pollUntilSettled(t *testing.T, h *PrepareHandler, id, clientID string, within time.Duration) prepareResponse {
	t.Helper()
	deadline := time.After(within)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		code, body := do(t, h, http.MethodGet, "/api/v3/stream/prepare/"+id, clientID)
		if code != http.StatusOK {
			t.Fatalf("status: want 200, got %d (%+v)", code, body)
		}
		switch PreparationState(body.State) {
		case PreparationReady, PreparationFailed, PreparationCancelled, PreparationCommitted:
			return body
		}
		select {
		case <-deadline:
			t.Fatalf("preparation never settled, last state %q", body.State)
		case <-tick.C:
		}
	}
}

// The whole cycle over HTTP: start, watch it become ready, take it.
//
// 202 on start is the point: accepted and running. A status code has never said
// anything about a broadcast, and this API does not pretend otherwise — readiness
// arrives later, as its own observable state.
func TestPrepareHTTP_StartPollCommit(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	started := startPreparation(t, h, refB, "sterling-1")
	if started.State != string(PreparationPending) {
		t.Fatalf("a fresh preparation is pending, got %q", started.State)
	}

	ready := pollUntilSettled(t, h, started.PreparationID, "sterling-1", 5*time.Second)
	if ready.State != string(PreparationReady) {
		t.Fatalf("want ready, got %q (%s)", ready.State, ready.Detail)
	}
	if ready.Generation == 0 {
		t.Fatal("a ready preparation must report the generation to commit")
	}

	commitPath := "/api/v3/stream/prepare/" + started.PreparationID + "/commit?generation=" +
		itoa(ready.Generation)
	code, committed := do(t, h, http.MethodPost, commitPath, "sterling-1")
	if code != http.StatusOK {
		t.Fatalf("commit: want 200, got %d (%+v)", code, committed)
	}
	if committed.State != string(PreparationCommitted) {
		t.Fatalf("want committed, got %q", committed.State)
	}

	// Idempotent: a client retrying after a lost response must not be told its
	// channel change failed.
	code, again := do(t, h, http.MethodPost, commitPath, "sterling-1")
	if code != http.StatusOK || again.State != string(PreparationCommitted) {
		t.Fatalf("commit must be idempotent, got %d / %q", code, again.State)
	}
}

// A commit quoting a generation the stream has left is a conflict, not a bad
// request: the client asked correctly, the world moved underneath it.
func TestPrepareHTTP_StaleGenerationIsAConflict(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	started := startPreparation(t, h, refB, "sterling-1")
	ready := pollUntilSettled(t, h, started.PreparationID, "sterling-1", 5*time.Second)

	path := "/api/v3/stream/prepare/" + started.PreparationID + "/commit?generation=" +
		itoa(ready.Generation+99)
	code, body := do(t, h, http.MethodPost, path, "sterling-1")
	if code != http.StatusConflict {
		t.Fatalf("want 409 for a stale generation, got %d (%+v)", code, body)
	}
	if body.Detail == "" {
		t.Fatal("a conflict must say what happened")
	}
}

// Committing before readiness is refused, and the body says which criteria are
// outstanding — the failure is diagnosable from the response alone.
func TestPrepareHTTP_CommitBeforeReadinessIsRefusedWithReasons(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, nil) // on air, delivering nothing
	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 300 * time.Millisecond
	h := newHandlerUnderTest(t, recv, cfg)

	started := startPreparation(t, h, refB, "sterling-1")

	code, body := do(t, h, http.MethodPost,
		"/api/v3/stream/prepare/"+started.PreparationID+"/commit?generation=1", "sterling-1")
	if code != http.StatusPreconditionFailed {
		t.Fatalf("want 412 before readiness, got %d (%+v)", code, body)
	}

	failed := pollUntilSettled(t, h, started.PreparationID, "sterling-1", 3*time.Second)
	if failed.State != string(PreparationFailed) {
		t.Fatalf("want failed, got %q", failed.State)
	}
	if failed.Outcome == "" {
		t.Fatal("a failure must name its outcome")
	}
	if len(failed.Pending) == 0 {
		t.Fatal("a failure must name the outstanding criteria")
	}
}

// A preparation belongs to the client that started it. Another client may not read
// it, take it, or throw it away, even knowing its identifier.
func TestPrepareHTTP_OwnershipIsEnforced(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	started := startPreparation(t, h, refB, "sterling-1")
	ready := pollUntilSettled(t, h, started.PreparationID, "sterling-1", 5*time.Second)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v3/stream/prepare/" + started.PreparationID},
		{http.MethodDelete, "/api/v3/stream/prepare/" + started.PreparationID},
		{http.MethodPost, "/api/v3/stream/prepare/" + started.PreparationID + "/commit?generation=" + itoa(ready.Generation)},
	} {
		code, _ := do(t, h, tc.method, tc.path, "someone-else")
		if code != http.StatusForbidden {
			t.Errorf("%s %s: want 403 for a foreign client, got %d", tc.method, tc.path, code)
		}
	}

	// The owner is unaffected by the attempts.
	still := pollUntilSettled(t, h, started.PreparationID, "sterling-1", time.Second)
	if still.State != string(PreparationReady) {
		t.Fatalf("the owner's preparation must be untouched, got %q", still.State)
	}
}

// Cancelling is idempotent: a client cleaning up must never have to care whether it
// won the race against an expiry or a supersede.
func TestPrepareHTTP_CancelIsIdempotent(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	started := startPreparation(t, h, refB, "sterling-1")

	for i := 0; i < 2; i++ {
		code, body := do(t, h, http.MethodDelete, "/api/v3/stream/prepare/"+started.PreparationID, "sterling-1")
		if code != http.StatusOK {
			t.Fatalf("cancel %d: want 200, got %d (%+v)", i, code, body)
		}
		if body.State != string(PreparationCancelled) {
			t.Fatalf("cancel %d: want cancelled, got %q", i, body.State)
		}
	}
}

// The API refuses to guess who is asking. Without an identity there is no ownership
// and no "one preparation per client".
func TestPrepareHTTP_RequiresAClientIdentity(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	code, _ := do(t, h, http.MethodPost, "/api/v3/stream/prepare?sref="+url.QueryEscape(refB), "")
	if code != http.StatusBadRequest {
		t.Fatalf("want 400 without a client id, got %d", code)
	}
}

func TestPrepareHTTP_RejectsMissingAndMalformedInput(t *testing.T) {
	recv := newFakeReceiver(t)
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	if code, _ := do(t, h, http.MethodPost, "/api/v3/stream/prepare", "sterling-1"); code != http.StatusBadRequest {
		t.Errorf("missing sref: want 400, got %d", code)
	}
	if code, _ := do(t, h, http.MethodGet, "/api/v3/stream/prepare/nope", "sterling-1"); code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", code)
	}
	if code, _ := do(t, h, http.MethodGet, "/api/v3/stream/prepare", "sterling-1"); code != http.StatusMethodNotAllowed {
		t.Errorf("GET on the collection: want 405, got %d", code)
	}
}

// A newer preparation from the same client supersedes the previous one over HTTP
// exactly as it does underneath: one client, one preparation, one tuner.
func TestPrepareHTTP_SecondPreparationSupersedesTheFirst(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, nil) // never ready, so it is still in flight
	recv.serve(refC, presentableBroadcast(t))
	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 5 * time.Second
	h := newHandlerUnderTest(t, recv, cfg)

	first := startPreparation(t, h, refB, "sterling-1")
	waitFor(t, 2*time.Second, func() bool { return recv.dialCount() >= 1 })
	second := startPreparation(t, h, refC, "sterling-1")

	if first.PreparationID == second.PreparationID {
		t.Fatal("a new preparation must have its own identifier")
	}

	firstNow := pollUntilSettled(t, h, first.PreparationID, "sterling-1", 3*time.Second)
	if firstNow.State != string(PreparationCancelled) {
		t.Fatalf("the superseded preparation must be cancelled, got %q", firstNow.State)
	}
	secondNow := pollUntilSettled(t, h, second.PreparationID, "sterling-1", 5*time.Second)
	if secondNow.State != string(PreparationReady) {
		t.Fatalf("the newer preparation must proceed, got %q (%s)", secondNow.State, secondNow.Detail)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// An identifier that was never issued answers the same way whichever verb asks.
// The three endpoints used to disagree, so whether a preparation existed could be
// inferred from the choice of method rather than from the answer.
func TestPrepareHTTP_UnknownPreparationIsNotFoundForEveryVerb(t *testing.T) {
	recv := newFakeReceiver(t)
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	const unknown = "prep-never-issued"
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v3/stream/prepare/" + unknown},
		{http.MethodDelete, "/api/v3/stream/prepare/" + unknown},
		{http.MethodPost, "/api/v3/stream/prepare/" + unknown + "/commit?generation=1"},
	} {
		code, _ := do(t, h, tc.method, tc.path, "sterling-1")
		if code != http.StatusNotFound {
			t.Errorf("%s %s: want 404 for an identifier that was never issued, got %d", tc.method, tc.path, code)
		}
	}
}

// A reference reaches this endpoint as a query parameter, which the standard library
// has already percent-decoded. Decoding it again corrupts anything that legitimately
// contains an escaped percent sign.
func TestPrepareHTTP_ServiceRefIsDecodedExactlyOnce(t *testing.T) {
	recv := newFakeReceiver(t)
	h := newHandlerUnderTest(t, recv, DefaultPreparationConfig())

	// The reference contains the literal text "%2520". Escaped onto the wire and
	// decoded once it comes back unchanged; decoded twice it collapses to "%20" and
	// names a different service.
	raw := "1:0:19:1:AAAA:0:0:0:0:%2520:"
	code, body := do(t, h, http.MethodPost,
		"/api/v3/stream/prepare?sref="+url.QueryEscape(raw), "sterling-1")
	if code != http.StatusAccepted {
		t.Fatalf("start: want 202, got %d (%+v)", code, body)
	}
	if body.ServiceRef != raw {
		t.Fatalf("service reference was not decoded exactly once:\n got %q\nwant %q", body.ServiceRef, raw)
	}
	if strings.Contains(body.ServiceRef, "%20:") {
		t.Fatalf("service reference was decoded twice: %q", body.ServiceRef)
	}
}
