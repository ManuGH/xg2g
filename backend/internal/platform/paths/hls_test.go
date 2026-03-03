package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveHLSRoot_Matrix(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name string

		// Setup
		envHLS      string
		envLegacy   string
		setupTarget func(dir string) // Create target dir/file
		setupLegacy func(dir string) // Create legacy dir/symlink

		// Expectations
		expectRootSuffix string
		expectMigrated   bool
		expectSkipped    bool
		expectEffective  string // If set, overrides suffix check
		expectError      bool
	}{
		{
			name:            "Explicit_New_Env",
			envHLS:          "/custom/new",
			expectEffective: "/custom/new",
		},
		{
			name:            "Explicit_Legacy_Env",
			envLegacy:       "/custom/old",
			expectEffective: "/custom/old",
		},
		{
			name:            "Both_Env_New_Wins",
			envHLS:          "/custom/new",
			envLegacy:       "/custom/old",
			expectEffective: "/custom/new",
		},
		{
			name:             "Default_NoDirs",
			expectRootSuffix: "hls",
		},
		{
			name:             "Default_TargetExists",
			setupTarget:      func(d string) { _ = os.MkdirAll(d, 0750) },
			expectRootSuffix: "hls",
		},
		{
			name: "Default_LegacyExists_NoTarget_Migrates",
			setupLegacy: func(d string) {
				_ = os.MkdirAll(d, 0750)
				_ = os.WriteFile(filepath.Join(d, "data.ts"), []byte("data"), 0600)
			},
			expectRootSuffix: "hls",
			expectMigrated:   true,
		},
		{
			name: "Default_LegacySymlink_SkipsMigration",
			setupLegacy: func(d string) {
				realDir := filepath.Join(filepath.Dir(d), "real_legacy")
				_ = os.MkdirAll(realDir, 0750)
				_ = os.Symlink(realDir, d)
			},
			expectRootSuffix: "v3-hls",
			expectSkipped:    true,
		},
		{
			name:        "Default_BothExist_LegacyContent_TargetWins",
			setupTarget: func(d string) { _ = os.MkdirAll(d, 0750) },
			setupLegacy: func(d string) {
				_ = os.MkdirAll(d, 0750)
				_ = os.WriteFile(filepath.Join(d, "data.ts"), []byte("data"), 0600)
			},
			expectRootSuffix: "hls",
		},
		{
			name:        "Error_TargetIsFile",
			setupTarget: func(d string) { _ = os.WriteFile(d, []byte("file"), 0600) },
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Env Clean
			t.Setenv(EnvHLSRoot, tt.envHLS)
			t.Setenv(EnvLegacyHLSRoot, tt.envLegacy)

			// Isolated Data Dir
			dataDir := filepath.Join(tmpDir, tt.name)
			_ = os.MkdirAll(dataDir, 0750)

			targetPath := filepath.Join(dataDir, TargetDirName)
			legacyPath := filepath.Join(dataDir, LegacyDirName)

			if tt.setupTarget != nil {
				tt.setupTarget(targetPath)
			}
			if tt.setupLegacy != nil {
				tt.setupLegacy(legacyPath)
			}

			// Exec
			res, err := ResolveHLSRoot(dataDir, os.Getenv(EnvHLSRoot), os.Getenv(EnvLegacyHLSRoot))

			// Verify Error
			if tt.expectError {
				assert.Error(t, err)
				return
			} else {
				assert.NoError(t, err)
			}

			// Verify Root
			if tt.expectEffective != "" {
				assert.Equal(t, tt.expectEffective, res.EffectiveRoot)
			} else {
				assert.True(t, strings.HasSuffix(res.EffectiveRoot, tt.expectRootSuffix), "Expected suffix %s, got %s", tt.expectRootSuffix, res.EffectiveRoot)
			}

			// Verify Flags
			assert.Equal(t, tt.expectMigrated, res.Migrated, "Migrated flag mismatch")
			assert.Equal(t, tt.expectSkipped, res.MigrationSkipped, "MigrationSkipped flag mismatch")

			// Verify Marker if migrated
			if res.Migrated {
				marker := filepath.Join(res.EffectiveRoot, MarkerFile)
				assert.FileExists(t, marker, "Marker file should exist after migration")
			}
		})
	}
}
