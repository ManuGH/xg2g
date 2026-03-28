package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/verification"
)

// ConfigProvider provides the current configuration.
type ConfigProvider interface {
	Current() *config.Snapshot
}

// ConfigChecker verifies configuration integrity.
type ConfigChecker struct {
	configPath string
	provider   ConfigProvider
}

func NewConfigChecker(path string, provider ConfigProvider) *ConfigChecker {
	return &ConfigChecker{
		configPath: path,
		provider:   provider,
	}
}

func (c *ConfigChecker) Check(ctx context.Context) ([]verification.Mismatch, error) {
	// 1. Get the current effective config first so we can normalize the
	// expected side with the same loader/version path used at runtime.
	snap := c.provider.Current()
	if snap == nil {
		return nil, fmt.Errorf("config provider returned nil snapshot")
	}

	// 2. Ensure the declared config file still exists on disk.
	if _, err := os.Stat(c.configPath); err != nil {
		if os.IsNotExist(err) {
			return []verification.Mismatch{{
				Kind:     verification.KindConfig,
				Key:      "config.file",
				Expected: "exists",
				Actual:   "missing",
			}}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// 3. Rebuild the expected config using the same defaults + file + ENV
	// precedence path as the running process. This avoids false-positive drift
	// when operators intentionally configure the service via environment.
	loader := config.NewLoader(c.configPath, snap.App.Version)
	expectedApp, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load expected config: %w", err)
	}
	expectedCfg := config.ToFileConfig(&expectedApp)

	// 4. Convert the in-memory runtime config to the same canonical file shape.
	actualCfg := config.ToFileConfig(&snap.App) // snap.App is struct, need pointer

	// 5. Compare canonical hashes.
	expectedHash := hashConfig(expectedCfg)
	actualHash := hashConfig(actualCfg)

	if expectedHash != actualHash {
		return []verification.Mismatch{{
			Kind:     verification.KindConfig,
			Key:      "config.fingerprint",
			Expected: "sha256:" + expectedHash,
			Actual:   "sha256:" + actualHash,
		}}, nil
	}

	return nil, nil
}

func hashConfig(cfg config.FileConfig) string {
	// Canonicalize via JSON
	data, _ := json.Marshal(cfg)
	// To be strictly canonical we might want a special marshaler that sorts keys,
	// but encoding/json does sort map keys by default.
	// Struct fields are ordered.

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CommandRunner executes commands.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// RealRunner executes commands using os/exec.
type RealRunner struct{}

func NewRealRunner() *RealRunner {
	return &RealRunner{}
}

func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// RuntimeChecker verifies runtime environment environment.
// Note: This checker is not concurrency-safe due to internal caching.
// It is intended to be called sequentially by the verification worker.
type RuntimeChecker struct {
	runner         CommandRunner
	expectedGo     string
	expectedFFmpeg string
	// Cache for expensive checks?
	// User said: "cached ffmpeg -version parse einmalig (oder sehr selten)"
	ffmpegVersionCached string
}

func NewRuntimeChecker(runner CommandRunner, expectGo, expectFFmpeg string) *RuntimeChecker {
	return &RuntimeChecker{
		runner:         runner,
		expectedGo:     expectGo,
		expectedFFmpeg: expectFFmpeg,
	}
}

func (c *RuntimeChecker) Check(ctx context.Context) ([]verification.Mismatch, error) {
	var mismatches []verification.Mismatch

	// 1. Go Version
	// runtime.Version() returns e.g. "go1.21.0"
	// We check if it matches expected or is compatible.
	// For strict equality:
	actualGo := runtime.Version()
	if c.expectedGo != "" && actualGo != c.expectedGo {
		mismatches = append(mismatches, verification.Mismatch{
			Kind:     verification.KindRuntime,
			Key:      "runtime.go.version",
			Expected: c.expectedGo,
			Actual:   actualGo,
		})
	}

	// 2. FFmpeg Version
	// Strategy: Exec only if not cached.
	// But what if it changes? For now, runtime binaries are static in containers.
	if c.ffmpegVersionCached == "" {
		out, err := c.runner.Run(ctx, "ffmpeg", "-version")
		if err != nil {
			return nil, fmt.Errorf("ffmpeg check: %w", err)
		}
		// Parse first line: "ffmpeg version 8.1-..."
		line := strings.Split(string(out), "\n")[0]
		parts := strings.Fields(line)
		if len(parts) >= 3 && parts[0] == "ffmpeg" && parts[1] == "version" {
			c.ffmpegVersionCached = parts[2]
		} else {
			c.ffmpegVersionCached = "unknown"
		}
	}

	if c.expectedFFmpeg != "" && !strings.HasPrefix(c.ffmpegVersionCached, c.expectedFFmpeg) {
		mismatches = append(mismatches, verification.Mismatch{
			Kind:     verification.KindBinary,
			Key:      "runtime.ffmpeg.version",
			Expected: c.expectedFFmpeg,
			Actual:   c.ffmpegVersionCached,
		})
	}

	return mismatches, nil
}
