package validation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// PerformStartupChecks validates the environment and dependencies before starting the server.
func PerformStartupChecks(ctx context.Context, cfg config.AppConfig) error {
	logger := log.WithComponent("startup-check")
	logger.Info().Msg("Runnning pre-flight startup checks...")

	// 1. Data Directory Permissions
	if err := checkDataDir(logger, cfg.DataDir); err != nil {
		return fmt.Errorf("data directory check failed: %w", err)
	}

	// 2. OpenWebIF Connectivity
	if err := checkOpenWebIF(ctx, logger, cfg); err != nil {
		return fmt.Errorf("receiver check failed: %w", err)
	}

	logger.Info().Msg("✅ All startup checks passed")
	return nil
}

func checkDataDir(logger zerolog.Logger, path string) error {
	// Check if directory exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", path)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	// Check write permissions by creating a temp file
	testFile := filepath.Join(path, ".write_test")
	if err := os.WriteFile(testFile, []byte("ok"), 0600); err != nil {
		return fmt.Errorf("directory is not writable: %s (error: %v)", path, err)
	}
	_ = os.Remove(testFile)

	logger.Info().Str("path", path).Msg("✓ Data directory is writable")
	return nil
}

func checkOpenWebIF(ctx context.Context, logger zerolog.Logger, cfg config.AppConfig) error {
	if cfg.OWIBase == "" {
		return fmt.Errorf("OpenWebIF base URL is not configured")
	}

	client := openwebif.NewWithPort(cfg.OWIBase, 0, openwebif.Options{
		Timeout:         5 * time.Second,
		Username:        cfg.OWIUsername,
		Password:        cfg.OWIPassword,
		UseWebIFStreams: cfg.UseWebIFStreams,
	})

	// Check connectivity (GetVersion or similar lightweight call)
	// We'll use Bouquets as it also validates the next step
	bouquets, err := client.Bouquets(ctx)
	if err != nil {
		return fmt.Errorf("failed to reach receiver at %s: %v", cfg.OWIBase, err)
	}
	logger.Info().Str("receiver", cfg.OWIBase).Msg("✓ Receiver is reachable")

	// Best-effort: fetch tuner/FBC info for visibility
	if about, err := client.About(ctx); err == nil && about != nil {
		tuners := about.TunersCount
		if tuners == 0 && len(about.Tuners) > 0 {
			tuners = len(about.Tuners)
		}
		if tuners == 0 && len(about.FBCTuners) > 0 {
			tuners = len(about.FBCTuners)
		}
		logger.Info().
			Str("receiver", cfg.OWIBase).
			Str("model", about.Info.Model).
			Str("boxtype", about.Info.Boxtype).
			Int("tuners_reported", tuners).
			Msg("receiver about info")
	}

	// 3. Bouquet Existence
	// cfg.Bouquet can be a comma-separated list
	configuredBouquets := strings.Split(cfg.Bouquet, ",")
	for _, required := range configuredBouquets {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		found := false
		// Wait, iterating over map values? No, bouquets is map[name]ref
		// So we can just check if required is in keys.
		_, exists := bouquets[required]
		if exists {
			found = true
		}

		if !found {
			// Helper to list available
			var available []string
			for name := range bouquets {
				available = append(available, fmt.Sprintf("'%s'", name))
			}
			return fmt.Errorf("required bouquet '%s' not found on receiver. Available: %s", required, strings.Join(available, ", "))
		}
		logger.Info().Str("bouquet", required).Msg("✓ Bouquet found")
	}

	return nil
}
