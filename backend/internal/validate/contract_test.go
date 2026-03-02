package validate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContractDriftGate prevents regression of known drifted field names/tags.
// See ADR-003 for context.
func TestContractDriftGate(t *testing.T) {
	projectRoot := findProjectRoot(t)

	// List of forbidden patterns that indicate contract drift
	forbiddenPatterns := []string{
		`json:"stream_url"`,
		`json:"playback_type"`,
		`json:"mime_type"`,
		`json:"recording_id"`,
		`json:"duration_source"`,
	}

	// Directories to scan (control layer where DTOs are mapped)
	scanDirs := []string{
		"internal/control/http/v3",
		"internal/api", // Legacy layer included for coverage
	}

	violations := []string{}

	for _, dir := range scanDirs {
		fullDir := filepath.Join(projectRoot, dir)
		err := filepath.Walk(fullDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			// Exclude generated files (they might contain the patterns safely)
			if strings.HasSuffix(path, "server_gen.go") {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer func() { _ = file.Close() }()

			scanner := bufio.NewScanner(file)
			lineNum := 1
			for scanner.Scan() {
				line := scanner.Text()
				for _, pattern := range forbiddenPatterns {
					if strings.Contains(line, pattern) {
						relPath, _ := filepath.Rel(projectRoot, path)
						violations = append(violations, fmt.Sprintf("%s:%d: found forbidden pattern %q", relPath, lineNum, pattern))
					}
				}
				lineNum++
			}
			return scanner.Err()
		})

		if err != nil && !os.IsNotExist(err) {
			t.Errorf("Failed to scan %s: %v", dir, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("Contract drift violations detected (see ADR-003):\n\n%s\n\nPlease use canonical generated DTOs instead of handwritten ones.", strings.Join(violations, "\n"))
	}
}
