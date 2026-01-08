package control

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestControlImportPurity ensures that the internal/control layer does NOT have
// any transitive dependencies on forbidden packages like internal/api or internal/api/v3.
func TestControlImportPurity(t *testing.T) {
	// We use 'go list -deps' to inspect the full transitive dependency graph.
	cmd := exec.Command("go", "list", "-deps", "github.com/ManuGH/xg2g/internal/control/...")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "Failed to run go list -deps: %s", stderr.String())

	deps := strings.Split(stdout.String(), "\n")

	// Forbidden package prefix (covers internal/api and internal/api/v3)
	forbiddenPrefix := "github.com/ManuGH/xg2g/internal/api"

	for _, d := range deps {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}

		// If any dependency in the graph starts with our forbidden prefix, it's an architectural leak.
		assert.False(t, strings.HasPrefix(d, forbiddenPrefix),
			"Architecural Violation: Control layer has a transitive dependency on '%s'", d)
	}
}
