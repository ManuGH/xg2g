package storageinventory

import (
	"path/filepath"
	"testing"
)

func TestInventoryIncludesSeparatedHLSArtifactClasses(t *testing.T) {
	hlsRoot := filepath.Join(t.TempDir(), "hls")

	artifacts := Inventory(RuntimePaths{
		DataDir:          t.TempDir(),
		StoreBackend:     "memory",
		HLSRoot:          hlsRoot,
		PlaylistFilename: "playlist.m3u8",
		XMLTVPath:        "xmltv.xml",
	})

	var liveClass ArtifactClass
	var recordingsClass ArtifactClass
	var livePath string
	var recordingsPath string

	for _, artifact := range artifacts {
		switch artifact.ID {
		case "live_sessions_root":
			liveClass = artifact.Class
			livePath = artifact.Path
		case "recording_artifacts_root":
			recordingsClass = artifact.Class
			recordingsPath = artifact.Path
		}
	}

	if liveClass != ClassTransient {
		t.Fatalf("live_sessions_root class = %q, want %q", liveClass, ClassTransient)
	}
	if recordingsClass != ClassMaterialized {
		t.Fatalf("recording_artifacts_root class = %q, want %q", recordingsClass, ClassMaterialized)
	}
	if livePath != filepath.Join(hlsRoot, "sessions") {
		t.Fatalf("live_sessions_root path = %q", livePath)
	}
	if recordingsPath != filepath.Join(hlsRoot, "recordings") {
		t.Fatalf("recording_artifacts_root path = %q", recordingsPath)
	}
}
