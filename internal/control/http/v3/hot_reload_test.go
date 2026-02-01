// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package v3

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

func createTestConfig() config.AppConfig {
	return config.AppConfig{
		LogLevel:           "info",
		DataDir:            "/tmp",
		VODCacheMaxEntries: 100,
		Enigma2: config.Enigma2Settings{
			BaseURL: "http://localhost:8001",
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
		Limits: config.LimitsConfig{
			MaxSessions:   10,
			MaxTranscodes: 5,
		},
		Timeouts: config.TimeoutsConfig{
			TranscodeStart:      5 * time.Second,
			TranscodeNoProgress: 10 * time.Second,
			KillGrace:           2 * time.Second,
		},
		Breaker: config.BreakerConfig{
			Window:            1 * time.Minute,
			MinAttempts:       3,
			FailuresThreshold: 5,
		},
	}
}

func TestHotReload_LogLevel_Hardened(t *testing.T) {
	var buf safeBuffer
	log.Configure(log.Config{
		Level:  "info",
		Output: &buf,
	})

	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Fatalf("global level should be info, got %v", zerolog.GlobalLevel())
	}

	cfg := createTestConfig()
	cm := config.NewManager("/tmp/xg2g-test-config-hardened.yaml")
	srv := NewServer(cfg, cm, nil)

	// Trigger Hot-Reload
	reqObj := ConfigUpdate{LogLevel: stringPtr("debug")}
	body, _ := json.Marshal(reqObj)

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", bytes.NewBuffer(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("test-token", "admin", []string{"v3:admin"})))

	rr := httptest.NewRecorder()
	srv.PutSystemConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Verify Effect
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("global level should be debug after reload, got %v", zerolog.GlobalLevel())
	}

	// Verify Structured Audit invisibility (Operator Visibility)
	logs := log.GetRecentLogs()
	found := false
	for _, l := range logs {
		if l.Fields["event"] == "log.level_changed" {
			found = true
			if l.Fields["who"] != "admin" {
				t.Errorf("expected principal 'admin' in buffer, got %v", l.Fields["who"])
			}
			break
		}
	}
	if !found {
		t.Error("Audit event not found in structured log buffer")
	}

	// 5. Verify 400 Bad Request for invalid level
	invalidReq := ConfigUpdate{LogLevel: stringPtr("NOT_A_LEVEL")}
	body, _ = json.Marshal(invalidReq)
	req = httptest.NewRequest(http.MethodPut, "/api/v3/system/config", bytes.NewBuffer(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("test-token", "admin", []string{"v3:admin"})))
	rr = httptest.NewRecorder()
	srv.PutSystemConfig(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid level, got %d", rr.Code)
	}
}

func TestHotReload_AuditGating_Killer(t *testing.T) {
	var buf safeBuffer
	log.Configure(log.Config{
		Level:  "info",
		Output: &buf,
	})

	cfg := createTestConfig()
	srv := NewServer(cfg, config.NewManager("/tmp/xg2g-audit-gate.yaml"), nil)

	// Setting level to ERROR
	reqObj := ConfigUpdate{LogLevel: stringPtr("error")}
	body, _ := json.Marshal(reqObj)

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", bytes.NewBuffer(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("test-token", "admin", []string{"v3:admin"})))

	rr := httptest.NewRecorder()
	srv.PutSystemConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	if zerolog.GlobalLevel() != zerolog.ErrorLevel {
		t.Errorf("expected global level error, got %v", zerolog.GlobalLevel())
	}

	// VERIFY: Audit log is NOT silenced.
	output := buf.String()
	if !strings.Contains(output, "log.level_changed") {
		t.Error("KILLER FAILURE: Audit log was silenced by the ERROR level!")
	}
	if !strings.Contains(output, "audit_severity\":\"info") {
		t.Error("Audit log missing honest severity marker")
	}
}

func TestHotReload_SliceNormalization_Hardened(t *testing.T) {
	// GIVEN: Config with nil vs empty AllowedOrigins ([]string)
	old := createTestConfig()
	old.AllowedOrigins = nil

	next := old
	next.AllowedOrigins = []string{}

	// WHEN: Diffing
	diff, err := config.Diff(old, next)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	// THEN: RestartRequired should be false due to semantic nil <-> empty normalization
	if diff.RestartRequired {
		t.Error("Restart erroneously required for nil <-> empty slice transition in AllowedOrigins")
	}
}

func TestHotReload_EmptyBouquets_CSVNormalization(t *testing.T) {
	// GIVEN: Config with empty bouquet (string)
	cfg := createTestConfig()
	cfg.Bouquet = ""

	srv := NewServer(cfg, config.NewManager("/tmp/xg2g-csv-norm.yaml"), nil)

	// WHEN: Sending an empty array (translates to "" string in PutSystemConfig)
	emptyBouquets := []string{}
	reqObj := ConfigUpdate{Bouquets: &emptyBouquets}
	body, _ := json.Marshal(reqObj)

	req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", bytes.NewBuffer(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("test-token", "admin", []string{"v3:admin"})))

	rr := httptest.NewRecorder()
	srv.PutSystemConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		RestartRequired bool `json:"restartRequired"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.RestartRequired {
		t.Error("RestartRequired should be false for empty bouquet list")
	}
}

func TestHotReload_ConcurrencyStress(t *testing.T) {
	cfg := createTestConfig()
	srv := NewServer(cfg, config.NewManager("/tmp/xg2g-stress.yaml"), nil)

	const count = 50
	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(idx int) {
			defer wg.Done()
			level := "debug"
			if idx%2 == 0 {
				level = "info"
			}
			reqObj := ConfigUpdate{LogLevel: stringPtr(level)}
			body, _ := json.Marshal(reqObj)
			req := httptest.NewRequest(http.MethodPut, "/api/v3/system/config", bytes.NewBuffer(body))
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("test-token", "admin", []string{"v3:admin"})))
			rr := httptest.NewRecorder()
			srv.PutSystemConfig(rr, req)
		}(i)
	}

	wg.Wait()
}

func TestHotReload_LogBuffer_OOM_Prevention(t *testing.T) {
	// GIVEN: Logger configured with the global structured buffer.
	log.Configure(log.Config{Level: "info", Output: io.Discard})
	log.ClearRecentLogs()

	// WHEN: Feeding a giant 2MB chunk without any newlines.
	giantChunk := make([]byte, 2*1024*1024)
	for i := range giantChunk {
		giantChunk[i] = 'A'
	}

	// We use L() to verify that the internal writer handles it.
	log.L().Info().Msg(string(giantChunk[:100])) // Just a normal log first

	// Now send the giant chunk directly to the underlying writer if possible,
	// or just log a giant message.
	// Since structuredBufferWriter is internal, we trigger it via log.L().
	// Zerolog might split giant messages, but our writer must bound partial hits.
	log.L().Log().Msg(string(giantChunk))

	// THEN: The buffer shouldn't have exploded.
	// Note: structuredBufferWriter.Write will reset its 'partial' buffer when it exceeds 1MiB.
}

func stringPtr(s string) *string { return &s }
func testBoolPtr(b bool) *bool   { return &b }
