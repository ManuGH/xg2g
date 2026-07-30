package main

import (
	"context"
	"encoding/json"
	mathrand "math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSessionID = "123e4567-e89b-12d3-a456-426614174000"

func TestValidateConfigRejectsProductionAndRemoteTargets(t *testing.T) {
	t.Parallel()

	production := config{Profile: profileSmoke, BaseURL: "http://127.0.0.1:8088", ArtifactDir: "out"}
	if err := validateConfig(&production); err == nil ||
		!strings.Contains(err.Error(), "production port 8088") {
		t.Fatalf("expected explicit production target rejection, got %v", err)
	}

	remote := config{Profile: profileSmoke, BaseURL: "https://example.com:8089", ArtifactDir: "out"}
	if err := validateConfig(&remote); err == nil {
		t.Fatal("expected remote target to be rejected")
	}
}

func TestValidateConfigMutatingProfileFailsClosed(t *testing.T) {
	t.Parallel()

	base := config{
		Profile:      profileSoak,
		BaseURL:      "http://127.0.0.1:8089",
		ArtifactDir:  "out",
		MaxInflight:  1,
		CyclesPerSec: 1,
		ReadyTimeout: time.Second,
	}
	if err := validateConfig(&base); err == nil || !strings.Contains(err.Error(), "--confirm-staging") {
		t.Fatalf("expected staging confirmation error, got %v", err)
	}

	base.ConfirmStaging = true
	if err := validateConfig(&base); err == nil || !strings.Contains(err.Error(), "--token-file") {
		t.Fatalf("expected token file error, got %v", err)
	}

	base.TokenFile = "token"
	if err := validateConfig(&base); err == nil || !strings.Contains(err.Error(), "--service-ref") {
		t.Fatalf("expected service ref error, got %v", err)
	}

	base.ServiceRefs = stringList{"1:0:1:445D:453:1:C00000:0:0:0:"}
	if err := validateConfig(&base); err != nil {
		t.Fatalf("expected valid staging config, got %v", err)
	}
	if base.Duration != time.Hour {
		t.Fatalf("duration=%s, want 1h", base.Duration)
	}
}

func TestReadTokenFileRequiresPrivatePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := readTokenFile(path)
	if err != nil {
		t.Fatalf("read private token: %v", err)
	}
	if token != "secret-token" {
		t.Fatalf("token=%q", token)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(path); err == nil {
		t.Fatal("expected permissive token file to be rejected")
	}
}

func TestLifecycleCycleUsesCurrentV3ContractAndCleansUp(t *testing.T) {
	t.Parallel()

	server, requests := newLifecycleTestServer(t)
	defer server.Close()
	client := newSessionClient(server.URL, "api-token", server.Client())

	result := runLifecycleCycle(
		context.Background(),
		client,
		"1:0:1:445D:453:1:C00000:0:0:0:",
		"test-cycle",
		config{ReadyTimeout: time.Second},
	)
	if result.err != nil {
		t.Fatalf("cycle error: %v", result.err)
	}
	if result.cleanupErr != nil {
		t.Fatalf("cleanup error: %v", result.cleanupErr)
	}

	requests.mu.Lock()
	defer requests.mu.Unlock()
	if requests.streamInfo != 1 || requests.starts != 1 || requests.sessions != 1 || requests.stops != 1 {
		t.Fatalf(
			"unexpected request counts: streamInfo=%d starts=%d sessions=%d stops=%d",
			requests.streamInfo,
			requests.starts,
			requests.sessions,
			requests.stops,
		)
	}
}

func TestHeartbeatUsesCanonicalEndpoint(t *testing.T) {
	t.Parallel()

	server, requests := newLifecycleTestServer(t)
	defer server.Close()
	client := newSessionClient(server.URL, "api-token", server.Client())

	if err := client.heartbeat(context.Background(), testSessionID); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	requests.mu.Lock()
	defer requests.mu.Unlock()
	if requests.heartbeats != 1 {
		t.Fatalf("heartbeats=%d, want 1", requests.heartbeats)
	}
}

func TestSmokeFailsClosedOnReadinessAndPrometheus(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != `up{job="xg2g"}` {
			t.Errorf("query=%q", got)
		}
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{
					map[string]any{"value": []any{float64(time.Now().Unix()), "1"}},
				},
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	result := runSmoke(
		context.Background(),
		newSessionClient(server.URL, "", server.Client()),
		newPromClient(server.URL, `{job="xg2g"}`, server.Client()),
	)
	if !result.Pass {
		t.Fatalf("smoke failed: %#v", result.Failures)
	}
}

func TestLifecycleSoakUsesDurationAndConcurrency(t *testing.T) {
	t.Parallel()

	server, _ := newLifecycleTestServer(t)
	defer server.Close()
	client := newSessionClient(server.URL, "api-token", server.Client())
	cfg := config{
		Duration:     60 * time.Millisecond,
		HoldDuration: 0,
		ReadyTimeout: time.Second,
		Seed:         7,
		CyclesPerSec: 50,
		MaxInflight:  2,
		ServiceRefs:  stringList{"1:0:1:445D:453:1:C00000:0:0:0:"},
	}

	result := runLifecycleSoak(
		context.Background(),
		cfg,
		client,
		nil,
		mathrand.New(mathrand.NewSource(7)),
	)
	if !result.Pass {
		t.Fatalf("soak failed: %#v", result.Failures)
	}
	if result.Observations["cycles_attempted"] < 2 {
		t.Fatalf("cycles_attempted=%d, want at least 2", result.Observations["cycles_attempted"])
	}
	if result.Observations["cycles_attempted"] != result.Observations["cycles_succeeded"] {
		t.Fatalf("observations=%#v", result.Observations)
	}
}

func TestLifecycleSoakTreatsCancellationAsFailure(t *testing.T) {
	t.Parallel()

	server, _ := newLifecycleTestServer(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)

	result := runLifecycleSoak(
		ctx,
		config{
			Duration:     time.Second,
			HoldDuration: 0,
			ReadyTimeout: time.Second,
			Seed:         7,
			CyclesPerSec: 10,
			MaxInflight:  1,
			ServiceRefs:  stringList{"1:0:1:445D:453:1:C00000:0:0:0:"},
		},
		newSessionClient(server.URL, "api-token", server.Client()),
		nil,
		mathrand.New(mathrand.NewSource(7)),
	)
	if result.Pass {
		t.Fatalf("cancelled soak unexpectedly passed: %#v", result)
	}
	if len(result.Failures) == 0 || result.Failures[0].RuleID != "RUN_CANCELLED" {
		t.Fatalf("failures=%#v, want RUN_CANCELLED", result.Failures)
	}
}

func TestWriteReportIsPrivateAndAtomicallyReplacesDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	writtenPath, err := writeReport(dir, report{RunID: "test-run"})
	if err != nil {
		t.Fatalf("write report: %v", err)
	}
	if writtenPath != path {
		t.Fatalf("path=%q, want %q", writtenPath, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("report mode=%04o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"run_id": "test-run"`) {
		t.Fatalf("report content=%q", data)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".report-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary reports remain: %v", matches)
	}
}

type lifecycleRequestCounts struct {
	mu          sync.Mutex
	streamInfo  int
	starts      int
	sessions    int
	heartbeats  int
	stops       int
	idempotency map[string]bool
}

func newLifecycleTestServer(t *testing.T) (*httptest.Server, *lifecycleRequestCounts) {
	t.Helper()
	counts := &lifecycleRequestCounts{idempotency: make(map[string]bool)}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v3/live/stream-info", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		var request playbackInfoRequest
		decodeJSON(t, r, &request)
		if request.ServiceRef == "" || request.Capabilities.CapabilitiesVersion != 3 {
			t.Errorf("invalid playback info request: %#v", request)
		}
		counts.mu.Lock()
		counts.streamInfo++
		counts.mu.Unlock()
		writeJSON(t, w, playbackInfoResponse{PlaybackDecisionToken: "decision-token"})
	})
	mux.HandleFunc("/api/v3/intents", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		var request intentRequest
		decodeJSON(t, r, &request)
		counts.mu.Lock()
		defer counts.mu.Unlock()
		if request.IdempotencyKey == "" || counts.idempotency[request.IdempotencyKey] {
			t.Errorf("invalid or duplicate idempotency key %q", request.IdempotencyKey)
		}
		counts.idempotency[request.IdempotencyKey] = true
		switch request.Type {
		case "stream.start":
			counts.starts++
			if request.ServiceRef == "" || request.PlaybackDecisionToken != "decision-token" || request.Client == nil {
				t.Errorf("invalid start request: %#v", request)
			}
		case "stream.stop":
			counts.stops++
			if request.SessionID != testSessionID {
				t.Errorf("stop session ID=%q", request.SessionID)
			}
		default:
			t.Errorf("unexpected intent type %q", request.Type)
		}
		w.WriteHeader(http.StatusAccepted)
		writeJSON(t, w, intentAcceptedResponse{
			SessionID: testSessionID,
			RequestID: "request-id",
			Status:    "accepted",
		})
	})
	mux.HandleFunc("/api/v3/sessions/"+testSessionID, func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		counts.mu.Lock()
		counts.sessions++
		counts.mu.Unlock()
		writeJSON(t, w, sessionResponse{
			SessionID:                testSessionID,
			RequestID:                "request-id",
			State:                    "READY",
			HeartbeatIntervalSeconds: 1,
			PlaybackURL:              "/api/v3/sessions/" + testSessionID + "/hls/index.m3u8",
		})
	})
	mux.HandleFunc("/api/v3/sessions/"+testSessionID+"/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		counts.mu.Lock()
		counts.heartbeats++
		counts.mu.Unlock()
		writeJSON(t, w, sessionHeartbeatResponse{SessionID: testSessionID, Acknowledged: true})
	})
	return httptest.NewServer(mux), counts
}

func requireBearer(t *testing.T, request *http.Request) {
	t.Helper()
	if got := request.Header.Get("Authorization"); got != "Bearer api-token" {
		t.Errorf("Authorization=%q", got)
	}
}

func decodeJSON(t *testing.T, request *http.Request, target any) {
	t.Helper()
	defer request.Body.Close()
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		t.Errorf("decode request: %v", err)
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
