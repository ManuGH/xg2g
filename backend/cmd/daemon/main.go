// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ManuGH/xg2g/internal/app/bootstrap"
	"github.com/ManuGH/xg2g/internal/config"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/telemetry"
	appversion "github.com/ManuGH/xg2g/internal/version"
)

// maskURL removes user info from a URL string for safe logging.
//
//nolint:unused // retained for CLI helper tests.
func maskURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid_url]"
	}
	parsedURL.User = nil
	return parsedURL.String()
}

func printMainUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xg2g - Next Gen to Go")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g [--config path] [--version]")
	_, _ = fmt.Fprintln(w, "  xg2g daemon run [--config path]")
	_, _ = fmt.Fprintln(w, "  xg2g version")
	_, _ = fmt.Fprintln(w, "  xg2g config <command> [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements <command> [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g preflight [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g status [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g report [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g help [command]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  daemon       Run the long-lived xg2g service")
	_, _ = fmt.Fprintln(w, "  version      Print version and build metadata")
	_, _ = fmt.Fprintln(w, "  config       Validate, dump, and migrate config files")
	_, _ = fmt.Fprintln(w, "  entitlements Inspect and manage commercial unlock overrides")
	_, _ = fmt.Fprintln(w, "  storage      Manage and verify local storage (SQLite)")
	_, _ = fmt.Fprintln(w, "  preflight    Run lifecycle preflight checks against effective config")
	_, _ = fmt.Fprintln(w, "  healthcheck  Probe API readiness/liveness endpoints")
	_, _ = fmt.Fprintln(w, "  diagnostic   Trigger diagnostic actions against the API")
	_, _ = fmt.Fprintln(w, "  status       Show verified system status")
	_, _ = fmt.Fprintln(w, "  report       Generate a redacted evidence bundle")
	_, _ = fmt.Fprintln(w, "  help         Show help for a command")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --config string  path to config file (YAML)")
	_, _ = fmt.Fprintln(w, "  --version        print version and exit")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Notes:")
	_, _ = fmt.Fprintln(w, "  Running without a subcommand starts the daemon.")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  xg2g --config /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g daemon run --config /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g version")
	_, _ = fmt.Fprintln(w, "  xg2g config validate -f /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements list --token $XG2G_API_TOKEN --principal-id viewer")
	_, _ = fmt.Fprintln(w, "  xg2g entitlements grant --token $XG2G_API_TOKEN --principal-id viewer --scope xg2g:unlock")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify --path /var/lib/xg2g/sessions.sqlite")
	_, _ = fmt.Fprintln(w, "  xg2g storage decision-report --data-dir /var/lib/xg2g --bouquet Premium --format table")
	_, _ = fmt.Fprintln(w, "  xg2g storage decision-sweep --config /etc/xg2g/config.yaml --data-dir /var/lib/xg2g --bouquet Premium --skip-scan")
	_, _ = fmt.Fprintln(w, "  xg2g preflight --config /etc/xg2g/config.yaml --operation=startup --json")
	_, _ = fmt.Fprintln(w, "  xg2g preflight --config /etc/xg2g/config.yaml --runtime-snapshot --json")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck --mode=ready --port=8088")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic --action=refresh --token $XG2G_API_TOKEN")
	_, _ = fmt.Fprintln(w, "  xg2g status --token $XG2G_API_TOKEN")
	_, _ = fmt.Fprintln(w, "  xg2g report --token $XG2G_API_TOKEN --out xg2g-report.json")
}

func printVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%s (commit: %s, built: %s)\n", appversion.Version, appversion.Commit, appversion.Date)
}

func handleImmediateMainCommand(stdout, stderr io.Writer, args []string) (bool, int) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return false, 0
	}

	switch args[0] {
	case "version":
		if len(args) != 1 {
			_, _ = fmt.Fprintln(stderr, "The version command accepts no arguments.")
			printMainUsage(stderr)
			return true, 2
		}
		printVersion(stdout)
		return true, 0
	case "daemon", "help", "config", "entitlements", "storage", "preflight",
		"healthcheck", "diagnostic", "status", "report":
		return false, 0
	default:
		_, _ = fmt.Fprintln(stderr, "Unknown command.")
		printMainUsage(stderr)
		return true, 2
	}
}

func normalizeDaemonArgs(stderr io.Writer, args []string) ([]string, int) {
	if len(args) == 0 || args[0] != "daemon" {
		return args, 0
	}
	if len(args) < 2 || args[1] != "run" {
		_, _ = fmt.Fprintln(stderr, "Usage: xg2g daemon run [--config path]")
		printMainUsage(stderr)
		return nil, 2
	}
	return args[2:], 0
}

func rejectDaemonPositionals(stderr io.Writer, args []string) int {
	if len(args) == 0 {
		return 0
	}
	_, _ = fmt.Fprintln(stderr, "Unexpected positional arguments; daemon mode accepts flags only.")
	printMainUsage(stderr)
	return 2
}

func runHelp(args []string) int {
	return runHelpTo(os.Stdout, os.Stderr, args)
}

func runHelpTo(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		printMainUsage(stdout)
		return 0
	}

	switch args[0] {
	case "config":
		printConfigUsage(stdout)
		return 0
	case "entitlements":
		printEntitlementsUsage(stdout)
		return 0
	case "storage":
		printStorageUsage(stdout)
		return 0
	case "preflight":
		printPreflightUsage(stdout)
		return 0
	case "healthcheck":
		printHealthcheckUsage(stdout)
		return 0
	case "diagnostic":
		printDiagnosticUsage(stdout)
		return 0
	case "status":
		statusCmd.SetOut(stdout)
		statusCmd.SetErr(stderr)
		defer statusCmd.SetOut(nil)
		defer statusCmd.SetErr(nil)
		if err := statusCmd.Help(); err != nil {
			_, _ = fmt.Fprintf(stderr, "Unable to render help for status: %v\n", err)
			return 1
		}
		return 0
	case "report":
		reportCmd.SetOut(stdout)
		reportCmd.SetErr(stderr)
		defer reportCmd.SetOut(nil)
		defer reportCmd.SetErr(nil)
		if err := reportCmd.Help(); err != nil {
			_, _ = fmt.Fprintf(stderr, "Unable to render help for report: %v\n", err)
			return 1
		}
		return 0
	case "daemon":
		printMainUsage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "Unknown help topic: %s\n\n", args[0])
		printMainUsage(stderr)
		return 2
	}
}

type cliExitError struct {
	Code int
}

func (e cliExitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.Code)
}

func exitCodeForErr(err error) int {
	var cliErr cliExitError
	if errors.As(err, &cliErr) && cliErr.Code > 0 {
		return cliErr.Code
	}
	return 1
}

func main() {
	cliArgs := os.Args[1:]
	if handled, code := handleImmediateMainCommand(os.Stdout, os.Stderr, cliArgs); handled {
		os.Exit(code)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			os.Exit(runHelp(os.Args[2:]))
		case "config":
			os.Exit(runConfigCLI(os.Args[2:]))
		case "entitlements":
			os.Exit(runEntitlementsCLI(os.Args[2:]))
		case "storage":
			os.Exit(runStorageCLI(os.Args[2:]))
		case "preflight":
			os.Exit(runPreflightCLI(os.Args[2:]))
		case "healthcheck":
			os.Exit(runHealthcheckCLI(os.Args[2:]))
		case "diagnostic":
			os.Exit(runDiagnosticCLI(os.Args[2:]))
		case "status":
			if err := statusCmd.Execute(); err != nil {
				os.Exit(exitCodeForErr(err))
			}
			os.Exit(0)
		case "report":
			if err := reportCmd.Execute(); err != nil {
				os.Exit(exitCodeForErr(err))
			}
			os.Exit(0)
		}
	}

	var code int
	cliArgs, code = normalizeDaemonArgs(os.Stderr, cliArgs)
	if code != 0 {
		os.Exit(code)
	}

	flag.Usage = func() {
		printMainUsage(flag.CommandLine.Output())
	}
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	if err := flag.CommandLine.Parse(cliArgs); err != nil {
		os.Exit(2)
	}

	if code := rejectDaemonPositionals(os.Stderr, flag.Args()); code != 0 {
		os.Exit(code)
	}

	if *showVersion {
		printVersion(os.Stdout)
		os.Exit(0)
	}

	xglog.Configure(xglog.Config{Level: "info", Service: "xg2g", Version: appversion.Version})
	logger := xglog.WithComponent("daemon")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// OpenTelemetry tracing is opt-in: it activates only when XG2G_OTEL_ENDPOINT is
	// set. Unset (the default) installs a no-op provider, so the spans already wired
	// across the codebase cost nothing. Failure to init never blocks startup.
	otelEndpoint := strings.TrimSpace(config.ParseString("XG2G_OTEL_ENDPOINT", ""))
	otelProtocol := config.ParseString("XG2G_OTEL_PROTOCOL", "grpc")
	tracerProvider, otelErr := telemetry.NewProvider(ctx, telemetry.Config{
		Enabled:        otelEndpoint != "",
		ServiceName:    "xg2g",
		ServiceVersion: appversion.Version,
		Environment:    config.ParseString("XG2G_OTEL_ENVIRONMENT", "production"),
		ExporterType:   otelProtocol,
		Endpoint:       otelEndpoint,
		SamplingRate:   config.ParseFloat("XG2G_OTEL_SAMPLING", 1.0),
	})
	if otelErr != nil {
		logger.Error().Err(otelErr).Msg("failed to initialize OpenTelemetry tracing; continuing without traces")
	} else {
		defer func() {
			if shErr := tracerProvider.Shutdown(context.Background()); shErr != nil {
				logger.Warn().Err(shErr).Msg("OpenTelemetry tracer shutdown error")
			}
		}()
		if otelEndpoint != "" {
			logger.Info().Str("endpoint", otelEndpoint).Str("protocol", otelProtocol).Msg("OpenTelemetry tracing enabled")
		}
	}

	container, err := bootstrap.WireServices(
		ctx,
		appversion.Version,
		appversion.Commit,
		appversion.Date,
		strings.TrimSpace(*configPath),
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to wire daemon services")
	}

	if err := container.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to start bootstrap workers")
	}

	if err := container.Run(ctx, stop); err != nil {
		logger.Fatal().Err(err).Str("event", "manager.failed").Msg("daemon app failed")
	}

	logger.Info().Msg("server exiting")
}
