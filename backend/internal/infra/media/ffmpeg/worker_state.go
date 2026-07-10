package ffmpeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type WorkerState struct {
	SessionID  string `json:"sessionId"`
	PID        int    `json:"pid"`
	ServiceRef string `json:"serviceRef"`
	ProfileID  string `json:"profileId"`
	CreatedAt  int64  `json:"createdAtUnix"`
}

// sessionStateBaseDir computes the portable base directory for session worker states.
// It prefers /dev/shm (tmpfs) on Linux but falls back to os.TempDir() when unavailable.
var sessionStateBaseDir = func() string {
	base := "/dev/shm"
	if _, err := os.Stat(base); err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "xg2g", "workers")
}()

// SessionStateBaseDir returns the base directory for worker state files.
func SessionStateBaseDir() string {
	return sessionStateBaseDir
}

func WriteWorkerState(sessionID, serviceRef, profileID string, pid int) error {
	dir := filepath.Join(sessionStateBaseDir, sessionID)
	_ = os.MkdirAll(dir, 0750)
	state := WorkerState{
		SessionID:  sessionID,
		PID:        pid,
		ServiceRef: serviceRef,
		ProfileID:  profileID,
		CreatedAt:  time.Now().Unix(),
	}
	b, _ := json.Marshal(state)
	return os.WriteFile(filepath.Join(dir, "state.json"), b, 0600)
}

func ReadWorkerStates() ([]WorkerState, error) {
	entries, err := os.ReadDir(sessionStateBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var states []WorkerState
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// e.Name() is from os.ReadDir, and we only read from subdirectories
		// that contain a fixed "state.json" filename. Path traversal via directory names
		// is meaningless since the caller controls session IDs (UUIDs), not arbitrary paths.
		b, err := os.ReadFile(filepath.Join(sessionStateBaseDir, e.Name(), "state.json")) // #nosec G304
		if err == nil {
			var st WorkerState
			if err := json.Unmarshal(b, &st); err == nil {
				states = append(states, st)
			}
		}
	}
	return states, nil
}
