package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

// VerifyIntegrity checks the SQLite database for structural corruption.
// Mode can be "quick" (PRAGMA quick_check) or "full" (PRAGMA integrity_check).
// It returns a slice of error messages if corruption is found, or nil if healthy.
func VerifyIntegrity(path string, mode string) ([]string, error) {
	// 1. Open in read-only mode with busy_timeout
	// format: file:path?mode=ro&_busy_timeout=2000
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=2000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database for verification: %w", err)
	}
	defer db.Close()

	// 2. Determine Pragma
	pragma := "PRAGMA quick_check;"
	if mode == "full" {
		pragma = "PRAGMA integrity_check;"
	}

	// 3. Execute and Scan results
	rows, err := db.Query(pragma)
	if err != nil {
		return nil, fmt.Errorf("integrity pragma failed: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var res string
		if err := rows.Scan(&res); err != nil {
			return nil, fmt.Errorf("failed to scan integrity result row: %w", err)
		}
		results = append(results, res)
	}

	// 4. Contract: success is exactly a single row with "ok"
	if len(results) == 1 && strings.ToLower(results[0]) == "ok" {
		return nil, nil // Healthy
	}

	// If results is empty, something is very wrong
	if len(results) == 0 {
		return []string{"no results returned from integrity check"}, nil
	}

	// Return the diagnostic rows
	return results, nil
}
