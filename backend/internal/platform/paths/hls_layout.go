package paths

import (
	"path/filepath"
	"strings"
)

const (
	LiveSessionsDirName       = "sessions"
	RecordingArtifactsDirName = "recordings"
)

// LiveSessionsRoot returns the dedicated subtree for ephemeral live session artifacts.
func LiveSessionsRoot(hlsRoot string) string {
	root := strings.TrimSpace(hlsRoot)
	if root == "" {
		return ""
	}
	return filepath.Join(root, LiveSessionsDirName)
}

// LiveSessionDir returns the canonical directory for one live session's HLS artifacts.
func LiveSessionDir(hlsRoot, sessionID string) string {
	base := LiveSessionsRoot(hlsRoot)
	sessionID = strings.TrimSpace(sessionID)
	if base == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(base, sessionID)
}

// LiveSessionArtifactRelPath returns the canonical relative path for a live session artifact.
func LiveSessionArtifactRelPath(sessionID, filename string) string {
	sessionID = strings.TrimSpace(sessionID)
	filename = strings.TrimSpace(filename)
	if sessionID == "" || filename == "" {
		return ""
	}
	return filepath.Join(LiveSessionsDirName, sessionID, filename)
}

// RecordingArtifactsRoot returns the dedicated subtree for materialized recording artifacts.
func RecordingArtifactsRoot(hlsRoot string) string {
	root := strings.TrimSpace(hlsRoot)
	if root == "" {
		return ""
	}
	return filepath.Join(root, RecordingArtifactsDirName)
}

// RecordingArtifactDir returns the canonical directory for a materialized recording artifact key.
func RecordingArtifactDir(hlsRoot, artifactKey string) string {
	base := RecordingArtifactsRoot(hlsRoot)
	artifactKey = strings.TrimSpace(artifactKey)
	if base == "" || artifactKey == "" {
		return ""
	}
	return filepath.Join(base, artifactKey)
}
