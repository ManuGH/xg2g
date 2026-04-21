package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type backgroundScanSessionLister interface {
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
}

type backgroundScanReceiverProbe interface {
	About(ctx context.Context) (*openwebif.AboutInfo, error)
	GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error)
}

func newBackgroundScanPlaybackDetector(store backgroundScanSessionLister, receiver backgroundScanReceiverProbe) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		active, err := backgroundScanHasActiveSession(ctx, store)
		if err != nil || active || receiver == nil {
			return active, err
		}
		return backgroundScanReceiverStreaming(ctx, receiver)
	}
}

func backgroundScanHasActiveSession(ctx context.Context, store backgroundScanSessionLister) (bool, error) {
	if store == nil {
		return false, nil
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	now := time.Now()
	for _, s := range sessions {
		switch model.DeriveLifecycleState(s, now) {
		case model.LifecycleStarting, model.LifecycleBuffering, model.LifecycleActive, model.LifecycleStalled:
			return true, nil
		}
	}
	return false, nil
}

func backgroundScanReceiverStreaming(ctx context.Context, receiver backgroundScanReceiverProbe) (bool, error) {
	probeCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		probeCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
	}
	defer cancel()

	var (
		about    *openwebif.AboutInfo
		aboutErr error

		status    *openwebif.StatusInfo
		statusErr error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		about, aboutErr = receiver.About(probeCtx)
	}()

	go func() {
		defer wg.Done()
		status, statusErr = receiver.GetStatusInfo(probeCtx)
	}()

	wg.Wait()

	if statusKnown, statusActive := backgroundScanParseOWIBool(statusValue(status)); statusKnown && statusActive {
		return true, nil
	}

	if about != nil {
		aboutKnown, aboutActive := backgroundScanAboutStreamsState(about.Info.Streams)
		statusKnown, statusActive := backgroundScanParseOWIBool(statusValue(status))
		streamingSignalKnown := aboutKnown || statusKnown
		streamingActive := aboutActive || statusActive
		if aboutActive || backgroundScanTunersStreaming(about.Info.Tuners, streamingSignalKnown, streamingActive) {
			return true, nil
		}
	}

	if aboutErr != nil && statusErr != nil {
		return false, errors.Join(statusErr, aboutErr)
	}

	return false, nil
}

func backgroundScanTunersStreaming(tuners []openwebif.AboutTuner, streamingSignalKnown, streamingActive bool) bool {
	for _, tuner := range tuners {
		if strings.TrimSpace(tuner.Stream) == "" {
			continue
		}
		if !streamingSignalKnown || streamingActive {
			return true
		}
	}
	return false
}

func backgroundScanAboutStreamsState(v any) (known bool, active bool) {
	switch streams := v.(type) {
	case nil:
		return false, false
	case []any:
		return true, len(streams) > 0
	case map[string]any:
		return true, len(streams) > 0
	case string:
		return true, strings.TrimSpace(streams) != ""
	case bool:
		return true, streams
	default:
		return true, true
	}
}

func backgroundScanParseOWIBool(v string) (known bool, value bool) {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return true, false
	default:
		return false, false
	}
}

func statusValue(info *openwebif.StatusInfo) string {
	if info == nil {
		return ""
	}
	return info.IsStreaming
}
