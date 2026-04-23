package v3

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameLocalRecordingArtifacts_RenamesMainAndSidecars(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.ts")
	mustWriteRecordingArtifact(t, mainPath, "video")
	mustWriteRecordingArtifact(t, mainPath+".meta", "svc\nMonk\ndesc\n")
	mustWriteRecordingArtifact(t, mainPath+".ap", "ap")
	mustWriteRecordingArtifact(t, mainPath+".cuts", "cuts")
	mustWriteRecordingArtifact(t, mainPath+".sc", "sc")
	mustWriteRecordingArtifact(t, filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.eit"), "eit")

	if err := renameLocalRecordingArtifacts(mainPath, "Psych"); err != nil {
		t.Fatalf("renameLocalRecordingArtifacts() error = %v", err)
	}

	newMainPath := filepath.Join(dir, "20251217 1219 - ORF1 HD - Psych.ts")
	for _, path := range []string{
		newMainPath,
		newMainPath + ".meta",
		newMainPath + ".ap",
		newMainPath + ".cuts",
		newMainPath + ".sc",
		filepath.Join(dir, "20251217 1219 - ORF1 HD - Psych.eit"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s: %v", path, err)
		}
	}

	metaContent, err := os.ReadFile(newMainPath + ".meta")
	if err != nil {
		t.Fatalf("ReadFile(meta) error = %v", err)
	}
	if got := string(metaContent); got != "svc\nPsych\ndesc\n" {
		t.Fatalf("unexpected meta content: %q", got)
	}

	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Fatalf("old main path still exists or unexpected error: %v", err)
	}
}

func TestRenameLocalRecordingArtifacts_ReplacesReadOnlyMetaViaTempRename(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.ts")
	mustWriteRecordingArtifact(t, mainPath, "video")
	mustWriteRecordingArtifact(t, mainPath+".meta", "svc\nMonk\ndesc\n")
	if err := os.Chmod(mainPath+".meta", 0o444); err != nil {
		t.Fatalf("Chmod(meta) error = %v", err)
	}

	if err := renameLocalRecordingArtifacts(mainPath, "Psych"); err != nil {
		t.Fatalf("renameLocalRecordingArtifacts() error = %v", err)
	}

	newMetaPath := filepath.Join(dir, "20251217 1219 - ORF1 HD - Psych.ts.meta")
	metaContent, err := os.ReadFile(newMetaPath)
	if err != nil {
		t.Fatalf("ReadFile(meta) error = %v", err)
	}
	if got := string(metaContent); got != "svc\nPsych\ndesc\n" {
		t.Fatalf("unexpected meta content: %q", got)
	}
}

func TestDeleteLocalRecordingArtifacts_RemovesMainAndSidecars(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.ts")
	mustWriteRecordingArtifact(t, mainPath, "video")
	mustWriteRecordingArtifact(t, mainPath+".meta", "svc\nMonk\n")
	mustWriteRecordingArtifact(t, mainPath+".ap", "ap")
	mustWriteRecordingArtifact(t, filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.eit"), "eit")

	if err := deleteLocalRecordingArtifacts(mainPath); err != nil {
		t.Fatalf("deleteLocalRecordingArtifacts() error = %v", err)
	}

	for _, path := range []string{
		mainPath,
		mainPath + ".meta",
		mainPath + ".ap",
		filepath.Join(dir, "20251217 1219 - ORF1 HD - Monk.eit"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", path, err)
		}
	}
}

func mustWriteRecordingArtifact(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
