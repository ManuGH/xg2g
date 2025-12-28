package recordings

import (
	"os"
	"time"
)

// IsStable checks if a file's size is stable over the given window duration.
// This prevents streaming files that are currently being written to.
//
// Returns false if:
//   - File does not exist
//   - File size changes during the window
//   - Any stat operation fails
func IsStable(path string, window time.Duration) bool {
	// First stat
	stat1, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Wait for stability window
	time.Sleep(window)

	// Second stat
	stat2, err := os.Stat(path)
	if err != nil {
		return false
	}

	// File is stable if size hasn't changed
	return stat1.Size() == stat2.Size()
}
