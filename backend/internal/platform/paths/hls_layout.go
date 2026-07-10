package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	LiveSessionsDirName       = "sessions"
	RecordingArtifactsDirName = "recordings"
)

// minShmFreeBytes is the minimum free space (32 MiB) required in /dev/shm
// before using it as tmpfs for HLS segments. If below this threshold, fall back to disk.
const minShmFreeBytes = 32 * 1024 * 1024

// LiveHLSRoot returns the base directory for live HLS streams.
// It prefers /dev/shm (tmpfs) on Linux if available and has sufficient free space,
// otherwise falls back to hlsRoot on disk.
func LiveHLSRoot(hlsRoot string) string {
	if runtime.GOOS != "linux" {
		return strings.TrimSpace(hlsRoot)
	}
	if shmFreeBytes() >= minShmFreeBytes {
		return "/dev/shm/xg2g"
	}
	return strings.TrimSpace(hlsRoot)
}

// LiveHLSRootForPolicy returns the base directory for live HLS streams depending on DVR policy.
// If dvrWindowSec > 300 (5 minutes), it routes directly to persistent disk (hlsRoot on NVMe/SSD)
// to accommodate large DVR buffers without exhausting RAM disk. Otherwise it returns LiveHLSRoot.
func LiveHLSRootForPolicy(hlsRoot string, dvrWindowSec int) string {
	if dvrWindowSec > 300 {
		return strings.TrimSpace(hlsRoot)
	}
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

// hasHLSArtifacts returns true if dir contains actual HLS session artifacts.
func hasHLSArtifacts(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"index.m3u8", "index.m3u8.tmp", ".first_frame", "init.mp4"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".m4s") || strings.HasSuffix(entry.Name(), ".ts") || strings.HasSuffix(entry.Name(), ".m3u8")) {
			return true
		}
	}
	return false
}

// LiveSessionDir returns the canonical directory for one live session's HLS artifacts.
// When looking up existing sessions, it checks both RAM disk (/dev/shm) and persistent disk (hlsRoot).
func LiveSessionDir(hlsRoot, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	shmDir := filepath.Join("/dev/shm/xg2g", LiveSessionsDirName, sessionID)
	diskRoot := strings.TrimSpace(hlsRoot)
	var diskDir string
	if diskRoot != "" {
		diskDir = filepath.Join(diskRoot, LiveSessionsDirName, sessionID)
	}

	// 1. If persistent disk directory has actual HLS artifacts, prefer it
	if diskDir != "" && hasHLSArtifacts(diskDir) {
		return diskDir
	}
	// 2. If RAM directory has actual HLS artifacts, prefer it
	if hasHLSArtifacts(shmDir) {
		return shmDir
	}
	// 3. Fallback: if diskDir exists (and shmDir does not), use diskDir
	if diskDir != "" {
		if st, err := os.Stat(diskDir); err == nil && st.IsDir() {
			return diskDir
		}
	}
	// 4. Fallback: if shmDir exists, use shmDir
	if st, err := os.Stat(shmDir); err == nil && st.IsDir() {
		return shmDir
	}
	base := LiveSessionsRoot(hlsRoot)
	if base == "" {
		return ""
	}
	return filepath.Join(base, sessionID)
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
