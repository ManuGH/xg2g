// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func runStorageCLI(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printStorageUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "verify":
		return runStorageVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", args[0])
		printStorageUsage(os.Stderr)
		return 2
	}
}

func printStorageUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify [--path PATH | --all] [--mode quick|full]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --path string  Path to a specific SQLite database file")
	_, _ = fmt.Fprintln(w, "  --all          Verify all known databases in $XG2G_DATA_DIR")
	_, _ = fmt.Fprintln(w, "  --mode string  Verification mode: quick (default) or full")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Subcommands:")
	_, _ = fmt.Fprintln(w, "  verify    Check database integrity")
}

func runStorageVerify(args []string) int {
	fs := flag.NewFlagSet("xg2g storage verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var path string
	var mode string
	var all bool

	fs.StringVar(&path, "path", "", "Path to the SQLite database file")
	fs.StringVar(&mode, "mode", "quick", "Verification mode: quick or full")
	fs.BoolVar(&all, "all", false, "Verify all known databases in $XG2G_DATA_DIR")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !all && path == "" {
		fmt.Fprintln(os.Stderr, "Error: --path or --all is required")
		return 2
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "quick" && mode != "full" {
		fmt.Fprintf(os.Stderr, "Error: invalid mode %q. Use 'quick' or 'full'.\n", mode)
		return 2
	}

	if all {
		dataDir := os.Getenv("XG2G_DATA_DIR")
		if dataDir == "" {
			dataDir = os.Getenv("XG2G_DATA") // Backward compatibility fallback
		}
		if dataDir == "" {
			fmt.Fprintln(os.Stderr, "Error: --all requires XG2G_DATA_DIR (or XG2G_DATA) to be set.")
			return 2
		}

		dbs := []string{"sessions.sqlite", "resume.sqlite", "capabilities.sqlite"}
		exitCode := 0
		checkedCount := 0
		for _, dbName := range dbs {
			dbPath := filepath.Join(dataDir, dbName)
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				continue
			}
			checkedCount++
			if code := doVerify(dbPath, mode); code != 0 {
				exitCode = code
			}
		}

		if checkedCount == 0 {
			fmt.Fprintf(os.Stderr, "Error: No databases found in %s\n", dataDir)
			fmt.Fprintf(os.Stderr, "Expected at least one of: %s\n", strings.Join(dbs, ", "))
			return 2
		}
		return exitCode
	}

	return doVerify(path, mode)
}

func doVerify(path string, mode string) int {
	fmt.Fprintf(os.Stderr, "🔍 Verifying integrity of %s (mode: %s)...\n", path, mode)

	issues, err := sqlite.VerifyIntegrity(path, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Verification interrupted by system error: %v\n", err)
		return 1
	}

	if issues != nil {
		fmt.Fprintln(os.Stderr, "🚨 CORRUPTION DETECTED!")
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "  - %s\n", issue)
		}
		return 1
	}

	fmt.Println("✅ Integrity Verified: ok")
	return 0
}
