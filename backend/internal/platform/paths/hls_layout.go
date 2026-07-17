package paths

import (
	"path/filepath"
	"strings"
)

const (
	LiveSessionsDirName       = "sessions"
	RecordingArtifactsDirName = "recordings"
)

// LiveHLSRoot returns the base directory for live HLS streams.
// xg2g now utilizes an internal RAM Shadow Store to buffer the latest segments
// in-memory (bounded to ~10 segments), making OS-level tmpfs mounts (/dev/shm)
// obsolete and dangerous for long DVR windows. It directly returns hlsRoot.
func LiveHLSRoot(hlsRoot string) string {
	return strings.TrimSpace(hlsRoot)
}

// LiveHLSRootForPolicy returns the base directory for live HLS streams depending on DVR policy.
func LiveHLSRootForPolicy(hlsRoot string, dvrWindowSec int) string {
	return LiveHLSRoot(hlsRoot)
}

// LiveSessionsRoot returns the dedicated subtree for ephemeral live session artifacts.
func LiveSessionsRoot(hlsRoot string) string {
	root := LiveHLSRoot(hlsRoot)
	if root == "" {
		return ""
	}
	return filepath.Join(root, LiveSessionsDirName)
}

// LiveSessionDir returns the canonical directory for one live session's HLS artifacts.
func LiveSessionDir(hlsRoot, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	diskRoot := strings.TrimSpace(hlsRoot)
	if diskRoot != "" {
		return filepath.Join(diskRoot, LiveSessionsDirName, sessionID)
	}
	return ""
}

// LiveSessionDirForPolicy returns the output directory for a live session based on DVR policy.
func LiveSessionDirForPolicy(hlsRoot, sessionID string, dvrWindowSec int) string {
	baseRoot := LiveHLSRootForPolicy(hlsRoot, dvrWindowSec)
	sessionID = strings.TrimSpace(sessionID)
	if baseRoot == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(baseRoot, LiveSessionsDirName, sessionID)
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
