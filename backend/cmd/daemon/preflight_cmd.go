package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/health"
	appversion "github.com/ManuGH/xg2g/internal/version"
)

func runPreflightCLI(args []string) int {
	return runPreflightCLIWithIO(args, os.Stdout, os.Stderr, detectRepoRoot)
}

func runPreflightCLIWithIO(args []string, stdout, stderr io.Writer, detectRepoRootFn func() string) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printPreflightUsage(fs.Output())
	}

	var configPath string
	var operation string
	var outputJSON bool
	var runtimeSnapshot bool
	var installRoot string
	var repoRoot string
	var targetVersion string
	var restoreRoot string

	fs.StringVar(&configPath, "config", "", "path to YAML configuration file")
	fs.StringVar(&configPath, "f", "", "path to YAML configuration file (shorthand)")
	fs.StringVar(&operation, "operation", string(health.LifecycleOperationStartup), "lifecycle operation (startup|install|upgrade|restore|rollback)")
	fs.BoolVar(&outputJSON, "json", false, "output raw JSON")
	fs.BoolVar(&runtimeSnapshot, "runtime-snapshot", false, "include live runtime snapshot and drift classification")
	fs.StringVar(&installRoot, "install-root", "/", "root prefix for installed host artifacts")
	fs.StringVar(&repoRoot, "repo-root", "", "optional repo root for deploy bundle comparisons")
	fs.StringVar(&targetVersion, "target-version", "", "optional semantic target release for upgrade checks")
	fs.StringVar(&restoreRoot, "restore-root", "", "path to the restore artifact directory for restore preflight")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	op := health.LifecycleOperation(strings.ToLower(strings.TrimSpace(operation)))
	switch op {
	case health.LifecycleOperationStartup,
		health.LifecycleOperationInstall,
		health.LifecycleOperationUpgrade,
		health.LifecycleOperationRestore,
		health.LifecycleOperationRollback:
	default:
		_, _ = fmt.Fprintf(stderr, "invalid --operation %q\n", operation)
		return 2
	}

	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = resolveDefaultConfigPath()
	}

	loader := config.NewLoader(configPath, appversion.Version)
	cfg, err := loader.Load()
	if err != nil {
		if configPath == "" {
			_, _ = fmt.Fprintf(stderr, "failed to load configuration from defaults/env: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(stderr, "failed to load configuration from %s: %v\n", configPath, err)
		}
		return 2
	}

	var fileCfg *config.FileConfig
	if strings.TrimSpace(configPath) != "" {
		raw, rawErr := config.LoadFileConfig(configPath)
		if rawErr != nil {
			_, _ = fmt.Fprintf(stderr, "failed to load raw file config from %s: %v\n", configPath, rawErr)
			return 2
		}
		fileCfg = raw
	}

	var runtime *health.LifecycleRuntimeSnapshot
	if runtimeSnapshot {
		if strings.TrimSpace(repoRoot) == "" && detectRepoRootFn != nil {
			repoRoot = detectRepoRootFn()
		}
		snapshot := health.CollectLifecycleRuntimeSnapshot(context.Background(), cfg, health.LifecycleRuntimeSnapshotOptions{
			InstallRoot: installRoot,
			RepoRoot:    repoRoot,
		})
		runtime = &snapshot
	}

	report := health.EvaluateLifecyclePreflight(context.Background(), cfg, health.LifecyclePreflightOptions{
		Operation:       op,
		RuntimeSnapshot: runtime,
		FileConfig:      fileCfg,
		ConfigPath:      configPath,
		TargetRelease:   targetVersion,
		RestoreRoot:     restoreRoot,
	})
	if outputJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "failed to encode preflight report: %v\n", err)
			return 2
		}
	} else {
		printLifecyclePreflightReport(stdout, report, configPath)
	}

	switch report.Status {
	case health.LifecyclePreflightSeverityFatal:
		return 2
	case health.LifecyclePreflightSeverityBlock:
		return 1
	default:
		return 0
	}
}

func printPreflightUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g preflight [--config /etc/xg2g/config.yaml] [--operation=startup] [--json]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --config string     path to YAML configuration file")
	_, _ = fmt.Fprintln(w, "  -f string           path to YAML configuration file (shorthand)")
	_, _ = fmt.Fprintln(w, "  --operation string  lifecycle operation (startup|install|upgrade|restore|rollback)")
	_, _ = fmt.Fprintln(w, "  --json              output raw JSON")
	_, _ = fmt.Fprintln(w, "  --runtime-snapshot  include live runtime snapshot and drift classification")
	_, _ = fmt.Fprintln(w, "  --install-root      root prefix for installed host artifacts (default: /)")
	_, _ = fmt.Fprintln(w, "  --repo-root         optional repo root for deploy bundle comparisons")
	_, _ = fmt.Fprintln(w, "  --target-version    optional semantic target release for upgrade checks")
	_, _ = fmt.Fprintln(w, "  --restore-root      path to the restore artifact directory for restore preflight")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Exit Codes:")
	_, _ = fmt.Fprintln(w, "  0  ok or warn")
	_, _ = fmt.Fprintln(w, "  1  block")
	_, _ = fmt.Fprintln(w, "  2  fatal or config load failure")
}

func printLifecyclePreflightReport(w io.Writer, report health.LifecyclePreflightReport, configPath string) {
	source := "defaults/env"
	if strings.TrimSpace(configPath) != "" {
		source = configPath
	}

	_, _ = fmt.Fprintf(w, "Lifecycle Preflight\n")
	_, _ = fmt.Fprintf(w, "  operation: %s\n", report.Operation)
	_, _ = fmt.Fprintf(w, "  source: %s\n", source)
	_, _ = fmt.Fprintf(w, "  status: %s\n", report.Status)
	_, _ = fmt.Fprintf(w, "  fatal: %t\n", report.Fatal)
	_, _ = fmt.Fprintf(w, "  blocking: %t\n", report.Blocking)

	if len(report.Findings) == 0 {
		_, _ = fmt.Fprintln(w, "  findings: none")
	} else {
		_, _ = fmt.Fprintln(w, "  findings:")
		for _, finding := range report.Findings {
			_, _ = fmt.Fprintf(w, "    - [%s] %s\n", finding.Severity, lifecycleFindingLabel(finding))
		}
	}

	if report.Runtime != nil {
		_, _ = fmt.Fprintf(w, "  runtime drift: %s\n", report.Runtime.Drift.Class)
		_, _ = fmt.Fprintf(w, "  runtime unit: %s\n", report.Runtime.Unit.Installed.Path)
		_, _ = fmt.Fprintf(w, "  runtime compose image: %s\n", report.Runtime.Compose.Image)
		if len(report.Runtime.Drift.Findings) > 0 {
			_, _ = fmt.Fprintln(w, "  runtime findings:")
			for _, finding := range report.Runtime.Drift.Findings {
				_, _ = fmt.Fprintf(w, "    - [%s] %s\n", finding.Class, runtimeFindingLabel(finding))
			}
		}
	}
	if report.Upgrade != nil {
		_, _ = fmt.Fprintf(w, "  upgrade current release: %s\n", report.Upgrade.CurrentRelease)
		_, _ = fmt.Fprintf(w, "  upgrade target release: %s\n", report.Upgrade.TargetRelease)
		if len(report.Upgrade.ConfigMigrationChanges) > 0 {
			_, _ = fmt.Fprintln(w, "  upgrade config migration:")
			for _, change := range report.Upgrade.ConfigMigrationChanges {
				_, _ = fmt.Fprintf(w, "    - %s\n", change)
			}
		}
		if len(report.Upgrade.DeprecatedSurfaces) > 0 {
			_, _ = fmt.Fprintln(w, "  upgrade deprecated surfaces:")
			for _, surface := range report.Upgrade.DeprecatedSurfaces {
				_, _ = fmt.Fprintf(w, "    - %s\n", surface)
			}
		}
	}
	if report.Restore != nil {
		_, _ = fmt.Fprintf(w, "  restore root: %s\n", report.Restore.RestoreRoot)
		if len(report.Restore.MissingRequired) > 0 {
			_, _ = fmt.Fprintln(w, "  restore missing required:")
			for _, artifact := range report.Restore.MissingRequired {
				_, _ = fmt.Fprintf(w, "    - %s\n", artifact)
			}
		}
		if len(report.Restore.MissingOptional) > 0 {
			_, _ = fmt.Fprintln(w, "  restore missing optional:")
			for _, artifact := range report.Restore.MissingOptional {
				_, _ = fmt.Fprintf(w, "    - %s\n", artifact)
			}
		}
	}
}

func lifecycleFindingLabel(finding health.LifecyclePreflightFinding) string {
	parts := make([]string, 0, 3)
	if contract := strings.TrimSpace(strings.ReplaceAll(finding.Contract, "_", " ")); contract != "" {
		parts = append(parts, contract)
	}
	if field := strings.TrimSpace(finding.Field); field != "" {
		parts = append(parts, field)
	}
	summary := strings.TrimSpace(finding.Summary)
	detail := strings.TrimSpace(finding.Detail)
	switch {
	case summary != "" && detail != "" && summary != detail:
		parts = append(parts, summary+" ("+detail+")")
	case summary != "":
		parts = append(parts, summary)
	case detail != "":
		parts = append(parts, detail)
	}
	return strings.Join(parts, ": ")
}

func runtimeFindingLabel(finding health.LifecycleRuntimeDriftFinding) string {
	parts := make([]string, 0, 2)
	if field := strings.TrimSpace(finding.Field); field != "" {
		parts = append(parts, field)
	}
	summary := strings.TrimSpace(finding.Summary)
	detail := strings.TrimSpace(finding.Detail)
	switch {
	case summary != "" && detail != "" && summary != detail:
		parts = append(parts, summary+" ("+detail+")")
	case summary != "":
		parts = append(parts, summary)
	case detail != "":
		parts = append(parts, detail)
	}
	return strings.Join(parts, ": ")
}

func detectRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		if fileExists(filepath.Join(dir, "deploy", "docker-compose.yml")) && fileExists(filepath.Join(dir, "deploy", "xg2g.service")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
