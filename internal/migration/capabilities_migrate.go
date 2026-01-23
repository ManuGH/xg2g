package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// MigrateCapabilities moves hardware capability metadata from JSON to SQLite.
func MigrateCapabilities(ctx context.Context, jsonPath string, sqliteStore *scan.SqliteStore, dryRun bool) (int, string, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", nil
		}
		return 0, "", fmt.Errorf("read capabilities json: %w", err)
	}

	var loaded map[string]scan.Capability
	if err := json.Unmarshal(data, &loaded); err != nil {
		return 0, "", fmt.Errorf("unmarshal capabilities json: %w", err)
	}

	count := 0
	var checksumData [][]byte
	for _, cp := range loaded {
		if !dryRun {
			sqliteStore.Update(cp)
		}

		// Deterministic checksum using canonical JSON
		ser, _ := json.Marshal(cp)
		checksumData = append(checksumData, ser)

		count++
	}

	return count, CalculateChecksum(checksumData), nil
}
