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

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func presentDecisionMode(value string) string {
	switch strings.TrimSpace(value) {
	case "direct_stream":
		return "remux"
	default:
		return strings.TrimSpace(value)
	}
}

func presentClientCapsSource(value string) string {
	switch strings.TrimSpace(value) {
	case "runtime_plus_family":
		return "runtime+family"
	case "family_fallback":
		return "family"
	default:
		return strings.TrimSpace(value)
	}
}

func resolveStorageDBPath(dataDir string, dbName string) string {
	dataDir = strings.TrimSpace(dataDir)
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return ""
	}

	candidates := make([]string, 0, 3)
	seen := make(map[string]bool, 3)
	addCandidate := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		path := filepath.Join(dir, dbName)
		if seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	addCandidate(config.ParseString("XG2G_STORE_PATH", ""))
	addCandidate(filepath.Join(dataDir, "store"))
	addCandidate(dataDir)

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return dbName
}

func runStorageCLI(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printStorageUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "verify":
		return runStorageVerify(args[1:])
	case "decision-report":
		return runStorageDecisionReport(args[1:])
	case "decision-sweep":
		return runStorageDecisionSweep(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", args[0])
		printStorageUsage(os.Stderr)
		return 2
	}
}

func printStorageUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify [--path PATH | --all] [--mode quick|full]")
	_, _ = fmt.Fprintln(w, "  xg2g storage decision-report [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g storage decision-sweep [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --path string  Path to a specific SQLite database file")
	_, _ = fmt.Fprintln(w, "  --all          Verify all known databases in $XG2G_DATA_DIR")
	_, _ = fmt.Fprintln(w, "  --mode string  Verification mode: quick (default) or full")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Subcommands:")
	_, _ = fmt.Fprintln(w, "  verify           Check database integrity")
	_, _ = fmt.Fprintln(w, "  decision-report  Read-only report over local playlist + scan + audit storage")
	_, _ = fmt.Fprintln(w, "  decision-sweep   Evaluate selected live senders and persist sweep-origin decisions")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "decision-report flags:")
	_, _ = fmt.Fprintln(w, "  --data-dir string      Path to xg2g data dir (default: XG2G_DATA_DIR / XG2G_DATA)")
	_, _ = fmt.Fprintln(w, "  --playlist string      Relative playlist filename inside data dir")
	_, _ = fmt.Fprintln(w, "  --bouquet string       Bouquet/group filter (for example Premium)")
	_, _ = fmt.Fprintln(w, "  --client-family string Filter current decisions by client family")
	_, _ = fmt.Fprintln(w, "  --intent string        Filter current decisions by requested intent")
	_, _ = fmt.Fprintln(w, "  --origin string        Filter current decisions by decision origin (runtime or sweep)")
	_, _ = fmt.Fprintln(w, "  --format string        Output format: table (default) or json")
	_, _ = fmt.Fprintln(w, "  --out string           Write report to file instead of stdout")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "decision-sweep flags:")
	_, _ = fmt.Fprintln(w, "  --config string        Path to config.yaml (defaults to data-dir/config.yaml when present)")
	_, _ = fmt.Fprintln(w, "  --data-dir string      Path to xg2g data dir (default: XG2G_DATA_DIR / XG2G_DATA)")
	_, _ = fmt.Fprintln(w, "  --playlist string      Relative playlist filename inside data dir")
	_, _ = fmt.Fprintln(w, "  --bouquet string       Bouquet/group filter (required unless --channel or --service-ref is set)")
	_, _ = fmt.Fprintln(w, "  --channel string       Comma-separated exact channel names to sweep")
	_, _ = fmt.Fprintln(w, "  --service-ref string   Comma-separated service refs to sweep")
	_, _ = fmt.Fprintln(w, "  --client-family string Comma-separated SSOT client fixture families (default: ios_safari_native,chromium_hlsjs)")
	_, _ = fmt.Fprintln(w, "  --limit int            Maximum matched senders to sweep (0 = all)")
	_, _ = fmt.Fprintln(w, "  --skip-scan            Decide only from existing capabilities.sqlite truth; no new probes")
	_, _ = fmt.Fprintln(w, "  --state-path string    Persist and diff against sweep snapshot JSON")
	_, _ = fmt.Fprintln(w, "  --no-state             Disable persisted sweep snapshot diffing")
	_, _ = fmt.Fprintln(w, "  --timeout duration     Overall sweep timeout (default: 10m)")
	_, _ = fmt.Fprintln(w, "  --probe-delay duration Delay between probes (default: 0s)")
	_, _ = fmt.Fprintln(w, "  --format string        Output format: table (default) or json")
	_, _ = fmt.Fprintln(w, "  --out string           Write report to file instead of stdout")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Notes:")
	_, _ = fmt.Fprintln(w, "  decision-report is read-only.")
	_, _ = fmt.Fprintln(w, "  decision-sweep writes decision_audit.sqlite using origin 'sweep'. Without --skip-scan it also updates capabilities.sqlite.")
	_, _ = fmt.Fprintln(w, "  decision-sweep also keeps a scope-aware last_sweep.json baseline unless --no-state is set.")
	_, _ = fmt.Fprintln(w, "  decision-sweep exits 1 when mode/truth/coverage drift is detected.")
	_, _ = fmt.Fprintln(w, "  Report/sweep output shows direct_stream as 'remux' and runtime_plus_family as 'runtime+family'.")
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
		dataDir := config.ResolveDataDirFromEnv()
		if dataDir == "" {
			fmt.Fprintln(os.Stderr, "Error: --all requires XG2G_DATA_DIR (or XG2G_DATA) to be set.")
			return 2
		}

		dbs := []string{"sessions.sqlite", "resume.sqlite", "capabilities.sqlite", "decision_audit.sqlite"}
		exitCode := 0
		checkedCount := 0
		for _, dbName := range dbs {
			dbPath := resolveStorageDBPath(dataDir, dbName)
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
