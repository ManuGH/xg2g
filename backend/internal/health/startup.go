// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package health

import (
	"context"
	"os"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
)

// PerformStartupChecks validates the environment and dependencies before starting the server.
func PerformStartupChecks(ctx context.Context, cfg config.AppConfig) error {
	logger := log.WithComponent("startup-check")
	logger.Info().Msg("Running lifecycle startup preflight...")

	report := EvaluateLifecyclePreflight(ctx, cfg, LifecyclePreflightOptions{Operation: LifecycleOperationStartup})
	if report.Fatal {
		return report.StartupError()
	}
	if report.Blocking {
		logger.Warn().Str("findings", report.Summary(LifecyclePreflightSeverityBlock)).Msg("startup preflight reported readiness-blocking findings")
	}
	if report.Status == LifecyclePreflightSeverityWarn {
		logger.Warn().Str("findings", report.Summary(LifecyclePreflightSeverityWarn)).Msg("startup preflight reported warnings")
	}

	logger.Info().Msg("✅ All startup checks passed")
	return nil
}

func checkFileReadable(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path comes from operator config; verifying readability is expected
	if err != nil {
		return err
	}
	return f.Close()
}
