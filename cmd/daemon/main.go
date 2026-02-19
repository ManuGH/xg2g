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
	xglog "github.com/ManuGH/xg2g/internal/log"
)

var (
	version   = "3.1.5"
	commit    = "dev"
	buildDate = "unknown"
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
	_, _ = fmt.Fprintln(w, "  xg2g config <command> [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic [flags]")
	_, _ = fmt.Fprintln(w, "  xg2g help [command]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  config       Validate, dump, and migrate config files")
	_, _ = fmt.Fprintln(w, "  storage      Manage and verify local storage (SQLite)")
	_, _ = fmt.Fprintln(w, "  healthcheck  Probe API readiness/liveness endpoints")
	_, _ = fmt.Fprintln(w, "  diagnostic   Trigger diagnostic actions against the API")
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
	_, _ = fmt.Fprintln(w, "  xg2g config validate -f /etc/xg2g/config.yaml")
	_, _ = fmt.Fprintln(w, "  xg2g storage verify --path /var/lib/xg2g/sessions.sqlite")
	_, _ = fmt.Fprintln(w, "  xg2g healthcheck --mode=ready --port=8088")
	_, _ = fmt.Fprintln(w, "  xg2g diagnostic --action=refresh --token $XG2G_API_TOKEN")
}

func runHelp(args []string) int {
	if len(args) == 0 {
		printMainUsage(os.Stdout)
		return 0
	}

	switch args[0] {
	case "config":
		printConfigUsage(os.Stdout)
		return 0
	case "storage":
		printStorageUsage(os.Stdout)
		return 0
	case "healthcheck":
		printHealthcheckUsage(os.Stdout)
		return 0
	case "diagnostic":
		printDiagnosticUsage(os.Stdout)
		return 0
	case "daemon":
		printMainUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown help topic: %s\n\n", args[0])
		printMainUsage(os.Stderr)
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help":
			os.Exit(runHelp(os.Args[2:]))
		case "config":
			os.Exit(runConfigCLI(os.Args[2:]))
		case "storage":
			os.Exit(runStorageCLI(os.Args[2:]))
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

	flag.Usage = func() {
		printMainUsage(flag.CommandLine.Output())
	}
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "", "path to config file (YAML)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s (commit: %s, built: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	xglog.Configure(xglog.Config{Level: "info", Service: "xg2g", Version: version})
	logger := xglog.WithComponent("daemon")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	container, err := bootstrap.WireServices(ctx, version, commit, buildDate, strings.TrimSpace(*configPath))
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
