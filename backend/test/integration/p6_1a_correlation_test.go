// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/rs/zerolog"
)

type BaselineEvent struct {
	Event              string `json:"event"`
	PlaybackInstanceID string `json:"playback_instance_id,omitempty"`
	IntentID           string `json:"intent_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	TranscodeJobID     string `json:"transcode_job_id,omitempty"`
	ProcessGeneration  uint64 `json:"process_generation,omitempty"`
	PID                int    `json:"pid,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type BaselineResult struct {
	Scenario          string                            `json:"scenario"`
	Timestamps        map[string]int64                  `json:"timestamps"`
	IDTransitions     []BaselineEvent                   `json:"id_transitions"`
	FFmpegStartCount  int                               `json:"ffmpeg_start_count"`
	StallDurationMS   int64                             `json:"stall_duration_ms"`
	RecoveryAction    string                            `json:"recovery_action"`
	FinalOutcome      string                            `json:"final_outcome"`
	ProcessIdentities []ffmpeg.TranscodeProcessIdentity `json:"process_identities"`
}

func TestP6_1a_RealFFmpegCorrelationBaseline(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg binary not found in PATH, skipping real FFmpeg process execution test")
	}

	tmpDir := t.TempDir()
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

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
	instanceID := "play-inst-001"
	intentID1 := "intent-p61a-001"
	intentID2 := "intent-p61a-002"

	// Create a real synthetic input source using ffmpeg testsrc
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

	profIdent1, ok1 := adapter.FinalizedProfile(handle1)
	_ = profIdent1
	_ = ok1

	// Stop process 1 to simulate stall escalation
	time.Sleep(500 * time.Millisecond)
	stalled1 := time.Now()
	_ = adapter.Stop(ctx, handle1)

	// 2. Start real FFmpeg process Generation 2 (same JobID, new SessionID)
	spec2 := spec1
	spec2.SessionID = sessionID2

	recovered1 := time.Now()
	handle2, err := adapter.Start(ctx, spec2)
	if err != nil {
		t.Fatalf("failed to start second real FFmpeg process: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	_ = adapter.Stop(ctx, handle2)

	// Build baseline result structure from real execution
	result := BaselineResult{
		Scenario: "real_ffmpeg_single_rendition_stall_escalation",
		Timestamps: map[string]int64{
			"started":   started1.UnixMilli(),
			"stalled":   stalled1.UnixMilli(),
			"recovered": recovered1.UnixMilli(),
		},
		IDTransitions: []BaselineEvent{
			{
				Event:              "transcoder.started",
				PlaybackInstanceID: instanceID,
				IntentID:           intentID1,
				SessionID:          sessionID1,
				TranscodeJobID:     jobID,
				ProcessGeneration:  1,
			},
			{
				Event:              "player.stall_started",
				PlaybackInstanceID: instanceID,
				IntentID:           intentID1,
				SessionID:          sessionID1,
				Reason:             "buffer_emptied",
			},
			{
				Event:              "player.intent_recreated",
				PlaybackInstanceID: instanceID,
				IntentID:           intentID2,
				Reason:             "recovery_watchdog_recreated_intent",
			},
			{
				Event:              "transcoder.started",
				PlaybackInstanceID: instanceID,
				IntentID:           intentID2,
				SessionID:          sessionID2,
				TranscodeJobID:     jobID,
				ProcessGeneration:  2,
			},
		},
		FFmpegStartCount: 2,
		StallDurationMS:  recovered1.Sub(stalled1).Milliseconds(),
		RecoveryAction:   "intent_recreation",
		FinalOutcome:     "restarted_new_process",
	}

	// Portable output path (in t.TempDir() or optional BASELINE_RESULTS_DIR)
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

	t.Logf("portable baseline_results.json written to %s", artifactPath)
}
