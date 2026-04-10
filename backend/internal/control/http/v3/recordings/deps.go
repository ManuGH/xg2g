package recordings

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// RecordingsService defines the minimal playback resolution contract needed by the v3 recordings service.
type RecordingsService interface {
	ResolvePlayback(ctx context.Context, id string, profile string) (domainrecordings.PlaybackResolution, error)
	GetMediaTruth(ctx context.Context, id string) (playback.MediaTruth, error)
}

type ChannelTruthSource interface {
	GetCapability(serviceRef string) (scan.Capability, bool)
}

type channelTruthProbeSource interface {
	ProbeCapability(ctx context.Context, serviceRef string) (scan.Capability, bool, error)
}

type DecisionAuditSink interface {
	Record(ctx context.Context, event decision.Event) error
}

// Deps defines external dependencies for the v3 recordings service.
type Deps interface {
	RecordingsService() RecordingsService
	ChannelTruthSource() ChannelTruthSource
	DecisionAuditSink() DecisionAuditSink
	CapabilityRegistry() capreg.Store
	Config() config.AppConfig
	HostPressure(ctx context.Context) playbackprofile.HostPressureAssessment
	HostRuntime(ctx context.Context) playbackprofile.HostRuntimeSnapshot
	ReceiverContext(ctx context.Context) *capreg.ReceiverContext
}
