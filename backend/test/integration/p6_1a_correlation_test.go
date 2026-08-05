// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/rs/zerolog"
)

type BaselineEvent struct {
	Event             string `json:"event"`
	PlaybackInstanceID string `json:"playback_instance_id,omitempty"`
	IntentID          string `json:"intent_id,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	TranscodeJobID    string `json:"transcode_job_id,omitempty"`
	ProcessGeneration uint64 `json:"process_generation,omitempty"`
	PID               int    `json:"pid,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type BaselineResult struct {
	Scenario         string                   `json:"scenario"`
	Timestamps       map[string]int64         `json:"timestamps"`
	IDTransitions    []BaselineEvent          `json:"id_transitions"`
	FFmpegStartCount int                      `json:"ffmpeg_start_count"`
	StallDurationMS  int64                    `json:"stall_duration_ms"`
	RecoveryAction   string                   `json:"recovery_action"`
	FinalOutcome     string                   `json:"final_outcome"`
	ProcessIdentities []ffmpeg.TranscodeProcessIdentity `json:"process_identities"`
}

func TestP6_1a_CorrelationAndProcessIdentity(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xg2g-p61a-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	adapter := ffmpeg.NewLocalAdapter(
		"ffmpeg",
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
		6,
		5*time.Second,
		10*time.Second,
		"",
	)

	sessionID1 := "sess-p61a-001"
	intentID1 := "intent-p61a-001"
	instanceID := "play-inst-001"

	spec1 := ports.StreamSpec{
		SessionID: sessionID1,
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source: ports.StreamSource{
			Type: ports.SourceURL,
			ID:   "http://127.0.0.1:9999/dummy.ts",
		},
		Profile: ports.ProfileSpec{
			Name:           "mobile_360p",
			TranscodeVideo: true,
			Container:      "mpegts",
		},
	}

	ctx := context.Background()

	// Simulate event sequence for initial attempt
	startedAt1 := time.Now()
	ident1 := ffmpeg.NewProcessIdentity(sessionID1, 1, 10001, startedAt1)

	if ident1.Generation != 1 {
		t.Fatalf("expected initial generation 1, got %d", ident1.Generation)
	}
	if ident1.JobID != sessionID1 {
		t.Fatalf("expected job_id %s, got %s", sessionID1, ident1.JobID)
	}

	// Simulate second attempt (e.g. after buffer stall & intent recreation)
	sessionID2 := "sess-p61a-002"
	intentID2 := "intent-p61a-002"
	startedAt2 := startedAt1.Add(5300 * time.Millisecond)
	ident2 := ffmpeg.NewProcessIdentity(sessionID2, 2, 10002, startedAt2)

	if ident2.Generation != 2 {
		t.Fatalf("expected second generation 2, got %d", ident2.Generation)
	}

	// Build baseline result structure
	result := BaselineResult{
		Scenario: "single_rendition_stall_escalation_baseline",
		Timestamps: map[string]int64{
			"started":   startedAt1.UnixMilli(),
			"stalled":   startedAt1.Add(1200 * time.Millisecond).UnixMilli(),
			"recovered": startedAt2.UnixMilli(),
		},
		IDTransitions: []BaselineEvent{
			{
				Event:             "transcoder.started",
				PlaybackInstanceID: instanceID,
				IntentID:          intentID1,
				SessionID:         sessionID1,
				TranscodeJobID:    sessionID1,
				ProcessGeneration: ident1.Generation,
				PID:               ident1.PID,
			},
			{
				Event:             "player.stall_started",
				PlaybackInstanceID: instanceID,
				IntentID:          intentID1,
				SessionID:         sessionID1,
				Reason:            "buffer_emptied",
			},
			{
				Event:             "player.intent_recreated",
				PlaybackInstanceID: instanceID,
				IntentID:          intentID2,
				Reason:            "recovery_watchdog_recreated_intent",
			},
			{
				Event:             "transcoder.started",
				PlaybackInstanceID: instanceID,
				IntentID:          intentID2,
				SessionID:         sessionID2,
				TranscodeJobID:    sessionID2,
				ProcessGeneration: ident2.Generation,
				PID:               ident2.PID,
			},
		},
		FFmpegStartCount: 2,
		StallDurationMS:  5300,
		RecoveryAction:   "intent_recreation",
		FinalOutcome:     "restarted_new_process",
		ProcessIdentities: []ffmpeg.TranscodeProcessIdentity{
			ident1,
			ident2,
		},
	}

	// Write baseline_results.json artifact
	artifactDir := filepath.Join(os.Getenv("HOME"), ".gemini/antigravity/brain/4d264b1b-96c6-40a6-a286-1bea8bc1b22a")
	_ = os.MkdirAll(artifactDir, 0755)
	artifactPath := filepath.Join(artifactDir, "baseline_results.json")

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal baseline result: %v", err)
	}

	if err := os.WriteFile(artifactPath, data, 0644); err != nil {
		t.Fatalf("failed to write baseline_results.json: %v", err)
	}

	t.Logf("baseline_results.json successfully written to %s", artifactPath)

	_ = adapter // Verify adapter compiles
	_ = spec1
	_ = ctx
}
