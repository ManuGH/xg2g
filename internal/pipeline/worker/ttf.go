// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"os"
	"strings"
	"time"
)

const (
	ttfsPollInterval = 200 * time.Millisecond
	ttfsMaxWait      = 30 * time.Second
)

func observeFirstSegment(ctx context.Context, sessionDir string, start time.Time, profile string) {
	ticker := time.NewTicker(ttfsPollInterval)
	defer ticker.Stop()

	timeout := time.NewTimer(ttfsMaxWait)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			if hasSegment(sessionDir) {
				observeTTFS(profile, start)
				return
			}
		}
	}
}

func hasSegment(sessionDir string) bool {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "seg_") {
			continue
		}
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".m4s") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Size() > 0 {
			return true
		}
	}
	return false
}
