package validate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritableDirectory(t *testing.T) {
	// 1. Valid Writable Directory (Existing)
	t.Run("ValidExisting", func(t *testing.T) {
		tmpDir := t.TempDir()
		v := validate.New()
		v.WritableDirectory("test", tmpDir, true)
		assert.True(t, v.IsValid())
	})

	// 2. Valid Writable Directory (New)
	t.Run("ValidNew", func(t *testing.T) {
		tmpDir := t.TempDir()
		newDir := filepath.Join(tmpDir, "new_dir")
		v := validate.New()
		v.WritableDirectory("test", newDir, false)
		assert.True(t, v.IsValid())
		assert.DirExists(t, newDir)
	})

	// 3. Read-Only Directory (Permission Denied)
	t.Run("ReadOnly", func(t *testing.T) {
		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")
		require.NoError(t, os.Mkdir(readOnlyDir, 0500)) // r-x for user (no write)

		v := validate.New()
		v.WritableDirectory("test", readOnlyDir, true)

		if os.Geteuid() == 0 {
			t.Skip("Skipping ReadOnly test running as root (always writable)")
		} else {
			assert.False(t, v.IsValid())
			if v.Err() != nil {
				assert.Contains(t, v.Err().Error(), "directory is not writable")
			}
		}
	})

	// 4. Missing Directory (mustExist=true)
	t.Run("MissingMustExist", func(t *testing.T) {
		tmpDir := t.TempDir()
		missingDir := filepath.Join(tmpDir, "missing")

		v := validate.New()
		v.WritableDirectory("test", missingDir, true)
		assert.False(t, v.IsValid())
		assert.Contains(t, v.Err().Error(), "directory does not exist")
	})

	// 5. Parent Not Writable (New)
	t.Run("ParentReadOnly", func(t *testing.T) {
		tmpDir := t.TempDir()
		readOnlyParent := filepath.Join(tmpDir, "parent_ro")
		require.NoError(t, os.Mkdir(readOnlyParent, 0500))

		nested := filepath.Join(readOnlyParent, "nested")

		v := validate.New()
		v.WritableDirectory("test", nested, false)

		if os.Geteuid() == 0 {
			t.Skip("Skipping ParentReadOnly test running as root")
		} else {
			assert.False(t, v.IsValid())
			assert.Error(t, v.Err())
		}
	})
}
