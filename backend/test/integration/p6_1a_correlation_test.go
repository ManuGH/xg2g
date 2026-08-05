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

func TestP6_1a_RealFFmpegJobGenerationAcrossManualRestart(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping real FFmpeg process execution test")
	}

	tmpDir := t.TempDir()
	var logBuf bytes.Buffer
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

	time.Sleep(600 * time.Millisecond)
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
	time.Sleep(600 * time.Millisecond)
	_ = adapter.Stop(ctx, handle2)

	// Parse captured zerolog JSON lines to extract REAL observed events
	lines := strings.Split(logBuf.String(), "\n")
	var observedEvents []ObservedEvent
	var processIdentities []ffmpeg.TranscodeProcessIdentity
	startCount := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt ObservedEvent
		if json.Unmarshal([]byte(line), &evt) == nil && evt.Event != "" {
			if strings.HasPrefix(evt.Event, "transcoder.") {
				observedEvents = append(observedEvents, evt)
				if evt.Event == "transcoder.started" {
					startCount++
					processIdentities = append(processIdentities, ffmpeg.NewProcessIdentity(evt.TranscodeJobID, evt.ProcessGeneration, evt.PID, started1))
				}
			}
		}
	}

	if startCount != 2 {
		t.Fatalf("expected 2 real transcoder.started events, observed %d", startCount)
	}

	// Assert generations 1 and 2 were assigned
	if len(processIdentities) >= 2 {
		if processIdentities[0].Generation != 1 {
			t.Errorf("expected first generation 1, got %d", processIdentities[0].Generation)
		}
		if processIdentities[1].Generation != 2 {
			t.Errorf("expected second generation 2, got %d", processIdentities[1].Generation)
		}
		if processIdentities[0].JobID != jobID || processIdentities[1].JobID != jobID {
			t.Errorf("expected JobID %s for both generations", jobID)
		}
	}

	// Build baseline result structure from REAL captured observation
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

	t.Logf("portable observed baseline_results.json written to %s (startCount=%d)", artifactPath, startCount)
}
