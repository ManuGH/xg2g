// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"errors"
	"os"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/hls"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
)

type playbackRuntimeState struct {
	segmentTruth          *hls.SegmentTruth
	attemptedSegmentTruth bool
	resumeState           *resume.State
}

func resolvePlaybackRuntimeState(ctx context.Context, deps recordingsModuleDeps, principalID, recordingID string, mode decision.Mode) playbackRuntimeState {
	segmentTruth, attemptedSegmentTruth := resolvePlaybackSegmentTruth(ctx, deps.artifacts, recordingID, mode)
	return playbackRuntimeState{
		segmentTruth:          segmentTruth,
		attemptedSegmentTruth: attemptedSegmentTruth,
		resumeState:           loadPlaybackResumeState(ctx, deps.resumeStore, principalID, recordingID),
	}
}

func loadPlaybackResumeState(ctx context.Context, store resume.Store, principalID, recordingID string) *resume.State {
	if store == nil || principalID == "" {
		return nil
	}

	stored, err := store.Get(ctx, principalID, recordingID)
	if err != nil {
		return nil
	}

	return stored
}

func resolvePlaybackSegmentTruth(ctx context.Context, resolver artifacts.Resolver, recordingID string, mode decision.Mode) (*hls.SegmentTruth, bool) {
	if mode != decision.ModeDirectStream && mode != decision.ModeTranscode {
		return nil, false
	}
	if resolver == nil {
		return nil, false
	}

	artifact, artifactErr := resolver.ResolvePlaylist(ctx, recordingID, "", "", nil)
	if artifactErr != nil {
		return nil, false
	}

	content, err := readPlaybackArtifactContent(artifact)
	if err != nil {
		return nil, true
	}

	truth, err := hls.ExtractSegmentTruth(content)
	if err != nil {
		return nil, true
	}

	return truth, true
}

func readPlaybackArtifactContent(a artifacts.ArtifactOK) (string, error) {
	if a.Data != nil {
		return string(a.Data), nil
	}
	if a.AbsPath != "" {
		b, err := os.ReadFile(a.AbsPath)
		return string(b), err
	}
	return "", errors.New("empty artifact")
}
