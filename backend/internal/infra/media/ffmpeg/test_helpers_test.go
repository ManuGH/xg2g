package ffmpeg

import (
	"context"
	"io"
	"os/exec"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"go.opentelemetry.io/otel/trace"
)

// noopStartupSpan returns a non-recording span for tests that drive the monitor
// directly without an active tracer provider.
func noopStartupSpan() trace.Span {
	return trace.SpanFromContext(context.Background())
}

func (a *LocalAdapter) buildArgs(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]string, error) {
	plan, err := a.buildArgsWithPlan(ctx, spec, inputURL)
	if err != nil {
		return nil, err
	}
	return plan.args, nil
}

func (a *LocalAdapter) monitorProcess(parentCtx context.Context, handle ports.RunHandle, cmd *exec.Cmd, stderr io.ReadCloser, sessionID string, usesVAAPI bool) {
	backend := profiles.GPUBackendNone
	if usesVAAPI {
		backend = profiles.GPUBackendVAAPI
	}
	a.monitorProcessWithStartTimeout(parentCtx, handle, cmd, stderr, sessionID, 0, backend, "", a.StartTimeout, noopStartupSpan(), time.Now(), nil)
}

func (a *LocalAdapter) startTimeoutForSpec(spec ports.StreamSpec) time.Duration {
	return a.startTimeoutForProfile(spec.Source.Type, spec.Profile)
}
