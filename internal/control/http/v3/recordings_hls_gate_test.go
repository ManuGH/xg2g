package v3

import (
	"os"
	"strings"
	"testing"
)

func TestHLSPlaylist_NoSyncPreflightIO(t *testing.T) {
	forbidden := []string{
		"os.Stat", "os.ReadFile", "filepath.EvalSymlinks", "exec.Command",
		"ffprobe", "ProbeDuration", "ResolveLocalExisting",
		"RecordingPlaylistReady", "RecordingLivePlaylistReady", "ensureRecordingVODPlaylist",
	}

	content, err := os.ReadFile("recordings.go")
	if err != nil {
		t.Fatalf("failed to read recordings.go: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	inFunction := false
	checking := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			inFunction = false
			checking = false
			if strings.HasPrefix(trimmed, "func (s *Server) GetRecordingHLSPlaylist") ||
				strings.HasPrefix(trimmed, "func (s *Server) scheduleRecordingVODPlaylist") {
				inFunction = true
				checking = true
			}
		}

		if inFunction && checking {
			for _, f := range forbidden {
				if strings.Contains(line, f) && !strings.Contains(line, "//") {
					t.Errorf("Forbidden call %q found in HLS handler path at recordings.go:%d", f, i+1)
				}
			}
		}
	}
}
