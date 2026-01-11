package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/rs/zerolog"
)

// LocalAdapter implements ports.MediaPipeline using local exec.Command.
type LocalAdapter struct {
	BinPath string
	HLSRoot string
	Logger  zerolog.Logger
	E2      *enigma2.Client // Dependency for Tuner operations
	mu      sync.Mutex
	// activeProcs maps run handles to running commands
	activeProcs map[ports.RunHandle]*exec.Cmd
}

// NewLocalAdapter creates a new adapter instance.
func NewLocalAdapter(binPath string, hlsRoot string, e2 *enigma2.Client, logger zerolog.Logger) *LocalAdapter {
	return &LocalAdapter{
		BinPath:     binPath,
		HLSRoot:     hlsRoot,
		E2:          e2,
		Logger:      logger,
		activeProcs: make(map[ports.RunHandle]*exec.Cmd),
	}
}

// Start initiates the media process.
func (a *LocalAdapter) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 0. Tune if required
	if spec.Source.Type == ports.SourceTuner && a.E2 != nil {
		if spec.Source.TunerSlot < 0 {
			return "", fmt.Errorf("invalid tuner slot: %d", spec.Source.TunerSlot)
		}
		// Create ephemeral tuner using legacy logic (reused)
		// We use a short timeout for the tune operation itself
		tuner := enigma2.NewTuner(a.E2, spec.Source.TunerSlot, 10*time.Second)

		// Use a detached context or the start context?
		// Start context is appropriate.
		if err := tuner.Tune(ctx, spec.Source.ID); err != nil {
			return "", fmt.Errorf("tuning failed: %w", err)
		}
	}

	// 1. Generate Arguments from Spec
	args, err := a.buildArgs(spec)
	if err != nil {
		return "", fmt.Errorf("failed to build args: %w", err)
	}

	// 2. Prepare Command
	cmd := exec.CommandContext(ctx, a.BinPath, args...) // #nosec G204

	// 3. Setup Logging
	cmd.Stdout = nil
	cmd.Stderr = nil

	// 4. Start
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg start failed: %w", err)
	}

	handle := ports.RunHandle(fmt.Sprintf("%s-%d", spec.SessionID, cmd.Process.Pid))
	a.activeProcs[handle] = cmd

	a.Logger.Info().
		Str("handle", string(handle)).
		Str("spec_id", spec.SessionID).
		Int("pid", cmd.Process.Pid).
		Msg("started media process")

	return handle, nil
}

// Stop terminates the process.
func (a *LocalAdapter) Stop(ctx context.Context, handle ports.RunHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cmd, exists := a.activeProcs[handle]
	if !exists {
		return nil // Idempotent
	}

	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	}

	delete(a.activeProcs, handle)
	return nil
}

// Health checks if the process is running.
func (a *LocalAdapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, exists := a.activeProcs[handle]
	if !exists {
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process not found",
			LastCheck: time.Now(),
		}
	}

	return ports.HealthStatus{
		Healthy:   true,
		Message:   "process active",
		LastCheck: time.Now(),
	}
}

func (a *LocalAdapter) buildArgs(spec ports.StreamSpec) ([]string, error) {
	var args []string

	// Input
	switch spec.Source.Type {
	case ports.SourceTuner:
		args = append(args, "-i", spec.Source.ID)
	case ports.SourceURL:
		args = append(args, "-i", spec.Source.ID)
	case ports.SourceFile:
		args = append(args, "-re", "-i", spec.Source.ID)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}

	if spec.Mode == ports.ModeLive {
		args = append(args, "-c:v", "libx264", "-f", "hls")
		outputPath := filepath.Join(a.HLSRoot, spec.SessionID, "stream.m3u8")
		_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
		args = append(args, outputPath)
	}

	return args, nil
}
