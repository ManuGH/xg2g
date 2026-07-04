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

func WriteWorkerState(sessionID, serviceRef, profileID string, pid int) error {
	dir := filepath.Join("/dev/shm/xg2g/sessions", sessionID)
	_ = os.MkdirAll(dir, 0755)
	state := WorkerState{
		SessionID:  sessionID,
		PID:        pid,
		ServiceRef: serviceRef,
		ProfileID:  profileID,
		CreatedAt:  time.Now().Unix(),
	}
	b, _ := json.Marshal(state)
	return os.WriteFile(filepath.Join(dir, "state.json"), b, 0644)
}

func ReadWorkerStates() ([]WorkerState, error) {
	entries, err := os.ReadDir("/dev/shm/xg2g/sessions")
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
		b, err := os.ReadFile(filepath.Join("/dev/shm/xg2g/sessions", e.Name(), "state.json"))
		if err == nil {
			var st WorkerState
			if err := json.Unmarshal(b, &st); err == nil {
				states = append(states, st)
			}
		}
	}
	return states, nil
}
