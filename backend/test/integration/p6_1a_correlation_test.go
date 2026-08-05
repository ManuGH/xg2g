// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/rs/zerolog"
)

type ObservedEvent struct {
	Event             string `json:"event"`
	SessionID         string `json:"session_id,omitempty"`
	TranscodeJobID    string `json:"transcode_job_id,omitempty"`
	ProcessGeneration uint64 `json:"process_generation,omitempty"`
	PID               int    `json:"pid,omitempty"`
	StartedAtUnixMS   int64  `json:"started_at_unix_ms,omitempty"`
	Reason            string `json:"reason,omitempty"`
	StartupPhase      string `json:"startup_phase,omitempty"`
	SegmentPath       string `json:"segment_path,omitempty"`
}

type LifecycleObservedResult struct {
	Scenario          string                            `json:"scenario"`
	Timestamps        map[string]int64                  `json:"timestamps"`
	ObservedEvents    []ObservedEvent                   `json:"observed_events"`
	FFmpegStartCount  int                               `json:"ffmpeg_start_count"`
	RecoveryAction    string                            `json:"recovery_action"`
	FinalOutcome      string                            `json:"final_outcome"`
	ProcessIdentities []ffmpeg.TranscodeProcessIdentity `json:"process_identities"`
}

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

func TestP6_1a_RealFFmpegJobGenerationAcrossManualRestart(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping real FFmpeg process execution test")
	}

	tmpDir := t.TempDir()
	var logBuf safeBuffer
	logger := zerolog.New(&logBuf).With().Timestamp().Logger()

	adapter := ffmpeg.NewLocalAdapter(
		ffmpegPath,
		"ffprobe",
		tmpDir,
		nil,
		logger,
		"5000000",
		"5M",
		5*time.Second,
		5*time.Second,
		false,
		5*time.Second,
		2,
		5*time.Second,
		10*time.Second,
		"",
	)

	jobID := "job-p61a-baseline-001"
	sessionID1 := "sess-p61a-001"
	sessionID2 := "sess-p61a-002"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Generate a real test input file (5 second TS stream)
	inputTSPath := filepath.Join(tmpDir, "input.ts")
	genCmd := exec.Command(ffmpegPath, "-f", "lavfi", "-i", "testsrc=duration=5:size=640x360:rate=25", "-f", "mpegts", "-y", inputTSPath)
	if genOut, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate test input TS file: %v (out: %s)", err, string(genOut))
	}

	spec1 := ports.StreamSpec{
		JobID:     jobID,
		SessionID: sessionID1,
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source: ports.StreamSource{
			Type: ports.SourceFile,
			ID:   inputTSPath,
		},
		Profile: ports.ProfileSpec{
			Name:           "mobile_360p",
			TranscodeVideo: true,
			Container:      "mpegts",
			DVRWindowSec:   12,
		},
	}

	// 1. Start real FFmpeg process Generation 1
	started1 := time.Now()
	handle1, err := adapter.Start(ctx, spec1)
	if err != nil {
		t.Fatalf("failed to start real FFmpeg process: %v", err)
	}

	time.Sleep(3000 * time.Millisecond)
	stalled1 := time.Now()
	_ = adapter.Stop(ctx, handle1)

	// 2. Start real FFmpeg process Generation 2 for SAME JobID under new SessionID
	spec2 := spec1
	spec2.SessionID = sessionID2

	recovered1 := time.Now()
	handle2, err := adapter.Start(ctx, spec2)
	if err != nil {
		t.Fatalf("failed to start second real FFmpeg process: %v", err)
	}
	time.Sleep(3000 * time.Millisecond)
	_ = adapter.Stop(ctx, handle2)

	// 3. Deterministic Waiter: Poll log buffer until 2 started and 2 stopped events are captured
	var observedEvents []ObservedEvent
	var processIdentities []ffmpeg.TranscodeProcessIdentity
	startCount := 0
	stopCount := 0
	readyCount := 0

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		observedEvents = nil
		processIdentities = nil
		startCount = 0
		stopCount = 0
		readyCount = 0

		lines := strings.Split(logBuf.String(), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var evt ObservedEvent
			if json.Unmarshal([]byte(line), &evt) == nil && evt.Event != "" {
				if strings.HasPrefix(evt.Event, "transcoder.") {
					observedEvents = append(observedEvents, evt)
					switch evt.Event {
					case "transcoder.started":
						startCount++
						stTime := time.UnixMilli(evt.StartedAtUnixMS)
						if evt.StartedAtUnixMS == 0 {
							stTime = started1
						}
						processIdentities = append(processIdentities, ffmpeg.NewProcessIdentity(evt.TranscodeJobID, evt.ProcessGeneration, evt.PID, stTime))
					case "transcoder.stopped":
						stopCount++
					case "transcoder.ready":
						readyCount++
					}
				}
			}
		}

		if startCount >= 2 && stopCount >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if startCount != 2 {
		t.Fatalf("expected exactly 2 real transcoder.started events, observed %d (logs:\n%s)", startCount, logBuf.String())
	}
	if stopCount != 2 {
		t.Fatalf("expected exactly 2 real transcoder.stopped events, observed %d (logs:\n%s)", stopCount, logBuf.String())
	}
	if readyCount < 1 {
		t.Fatalf("expected at least 1 real transcoder.ready event, observed %d (logs:\n%s)", readyCount, logBuf.String())
	}

	// 4. Strict Identity Assertions
	if len(processIdentities) != 2 {
		t.Fatalf("expected exactly 2 process identities, got %d", len(processIdentities))
	}
	pid1 := processIdentities[0].PID
	pid2 := processIdentities[1].PID
	if pid1 <= 0 || pid2 <= 0 {
		t.Fatalf("expected positive PIDs, got pid1=%d, pid2=%d", pid1, pid2)
	}
	if pid1 == pid2 {
		t.Fatalf("expected distinct PIDs for separate runs, got identical PID %d", pid1)
	}
	if processIdentities[0].JobID != jobID || processIdentities[1].JobID != jobID {
		t.Fatalf("expected JobID %s for both identities, got %s and %s", jobID, processIdentities[0].JobID, processIdentities[1].JobID)
	}
	if processIdentities[0].Generation != 1 || processIdentities[1].Generation != 2 {
		t.Fatalf("expected generations 1 and 2, got %d and %d", processIdentities[0].Generation, processIdentities[1].Generation)
	}
	if processIdentities[0].StartedAt.IsZero() || processIdentities[1].StartedAt.IsZero() {
		t.Fatalf("expected valid non-zero StartedAt timestamps in identities")
	}

	// 5. Sequence Invariant & No Ready After Stopped Assertion
	genStopped := make(map[uint64]bool)
	for _, evt := range observedEvents {
		if evt.Event == "transcoder.stopped" {
			genStopped[evt.ProcessGeneration] = true
		}
		if evt.Event == "transcoder.ready" {
			if genStopped[evt.ProcessGeneration] {
				t.Fatalf("invalid event sequence: transcoder.ready logged AFTER transcoder.stopped for generation %d", evt.ProcessGeneration)
			}
		}
	}

	// 6. Map Cleanup Assertion
	if _, ok1 := adapter.GetProcessIdentity(handle1); ok1 {
		t.Fatalf("expected handle1 to be removed from processIdentities map after stop")
	}
	if _, ok2 := adapter.GetProcessIdentity(handle2); ok2 {
		t.Fatalf("expected handle2 to be removed from processIdentities map after stop")
	}

	// 7. Write Portable Baseline Result
	result := LifecycleObservedResult{
		Scenario: "real_ffmpeg_single_rendition_manual_restart_observed",
		Timestamps: map[string]int64{
			"started":   started1.UnixMilli(),
			"stalled":   stalled1.UnixMilli(),
			"recovered": recovered1.UnixMilli(),
		},
		ObservedEvents:    observedEvents,
		FFmpegStartCount:  startCount,
		RecoveryAction:    "manual_test_restart",
		FinalOutcome:      "second_process_started_by_test",
		ProcessIdentities: processIdentities,
	}

	outputDir := tmpDir
	if custom := os.Getenv("BASELINE_RESULTS_DIR"); custom != "" {
		outputDir = custom
		_ = os.MkdirAll(outputDir, 0755)
	}
	artifactPath := filepath.Join(outputDir, "baseline_results.json")

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal baseline result: %v", err)
	}

	if err := os.WriteFile(artifactPath, data, 0644); err != nil {
		t.Fatalf("failed to write baseline_results.json: %v", err)
	}

	t.Logf("portable observed baseline_results.json written to %s (starts=%d, stops=%d, ready=%d)\nLOGS:\n%s", artifactPath, startCount, stopCount, readyCount, logBuf.String())
}
