// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathSanitizer_BoundsCheck(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sanitizer_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Valid relative paths (including ..aufnahme.ts)
	validPaths := []string{
		"..aufnahme.ts",
		"Movies/TestMovie.ts",
		"Series/Season 01/Episode1.mp4",
		"TV-Recordings/Sport/Match.ts",
	}
	for _, p := range validPaths {
		clean, err := SanitizeAndValidateRelativePath(tmpDir, p)
		if err != nil {
			t.Errorf("Expected valid path for '%s', got error: %v", p, err)
		}
		if clean == "" {
			t.Errorf("Clean path for '%s' was empty", p)
		}
	}

	// Invalid relative paths
	invalidPaths := []string{
		"../outside.ts",
		"Movies/../../etc/passwd",
		"/absolute/path.ts",
		"C:\\Windows\\system32",
		"illegal:character.ts",
		"null\x00byte.ts", // Real NUL byte!
	}
	for _, p := range invalidPaths {
		_, err := SanitizeAndValidateRelativePath(tmpDir, p)
		if err == nil {
			t.Errorf("Expected error for illegal path '%s', got nil", p)
		}
	}
}

func TestNamingPresets_CollisionFormatting(t *testing.T) {
	meta := TemplateMetadata{
		Title:        "Das Traumschiff",
		ChannelName:  "ZDF HD",
		StartTime:    time.Date(2026, 8, 1, 20, 15, 0, 0, time.UTC),
		Year:         2026,
		Season:       2,
		Episode:      5,
		EpisodeTitle: "Mauritius",
		AssetID:      "asset_9988776655",
	}

	movieFile := FormatMediaFilename(NamingPresetMovies, "", meta, ContainerMP4)
	if movieFile != "Das Traumschiff (2026)/Das Traumschiff (2026).mp4" {
		t.Errorf("Unexpected movie preset output: '%s'", movieFile)
	}

	seriesFile := FormatMediaFilename(NamingPresetSeries, "", meta, ContainerTS)
	if seriesFile != "Das Traumschiff/Season 02/Das Traumschiff - S02E05 - Mauritius.ts" {
		t.Errorf("Unexpected series preset output: '%s'", seriesFile)
	}

	customFile := FormatMediaFilename(NamingPresetCustom, "{channel}/{year}/{title} - {date}", meta, ContainerMP4)
	if customFile != "ZDF HD/2026/Das Traumschiff - 2026-08-01.mp4" {
		t.Errorf("Unexpected custom preset output: '%s'", customFile)
	}

	collisionPath := AppendCollisionSuffix("Movies/Das Traumschiff.ts", meta.AssetID)
	if collisionPath != "Movies/Das Traumschiff - asset_99.ts" {
		t.Errorf("Unexpected collision suffix output: '%s'", collisionPath)
	}
}

func TestDiskAssetRepository_OptimisticConcurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "asset_repo_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repoFile := filepath.Join(tmpDir, "assets.json")
	repo, err := NewDiskAssetRepository(repoFile)
	if err != nil {
		t.Fatalf("NewDiskAssetRepository failed: %v", err)
	}

	asset, err := NewRecordingAsset("asset_101", "job_101", "Tatort", "1:0:19:283D:3FB:1:C00000:0:0:0:", "local-nvme", "Recordings/Tatort.ts", ContainerTS)
	if err != nil {
		t.Fatalf("NewRecordingAsset failed: %v", err)
	}

	// New asset save requires expectedVersion 0 or 1
	if err := repo.Save(ctx, asset, 1); err != nil {
		t.Fatalf("repo.Save initial failed: %v", err)
	}
	if asset.Version != 2 {
		t.Fatalf("Expected saved asset version to be incremented to 2, got %d", asset.Version)
	}

	// Fetch asset
	fetched, err := repo.Get(ctx, asset.ID)
	if err != nil {
		t.Fatalf("repo.Get failed: %v", err)
	}
	if fetched.State != AssetInProgress {
		t.Errorf("Expected AssetInProgress state, got %s", fetched.State)
	}

	// Transition state on clone
	updated, err := fetched.TransitionState(AssetAvailable)
	if err != nil {
		t.Fatalf("TransitionState failed: %v", err)
	}

	// Save with exact expected version = 2
	if err := repo.Save(ctx, updated, 2); err != nil {
		t.Fatalf("repo.Save updated asset with version 2 failed: %v", err)
	}
	if updated.Version != 3 {
		t.Fatalf("Expected updated asset version to be incremented to 3, got %d", updated.Version)
	}

	// Concurrent save with outdated version = 2 (expected 3!) MUST FAIL
	outdated, _ := fetched.TransitionState(AssetOffline)
	if err := repo.Save(ctx, outdated, 2); err == nil {
		t.Errorf("Expected optimistic concurrency error for outdated version 2, got nil")
	}
}

func TestDiskProfileRepository_CRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile_repo_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	repoFile := filepath.Join(tmpDir, "profiles.json")
	repo, err := NewDiskProfileRepository(repoFile)
	if err != nil {
		t.Fatalf("NewDiskProfileRepository failed: %v", err)
	}

	prof, err := NewRecordingProfile("prof_plex_movies", "Plex Movies", "nas-plex", "Movies", ContainerMP4, NamingPresetMovies)
	if err != nil {
		t.Fatalf("NewRecordingProfile failed: %v", err)
	}

	if err := repo.Save(ctx, prof); err != nil {
		t.Fatalf("repo.Save profile failed: %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("repo.List expected 1 profile, got %d (err: %v)", len(list), err)
	}

	if list[0].Name != "Plex Movies" {
		t.Errorf("Expected profile name 'Plex Movies', got '%s'", list[0].Name)
	}
}
