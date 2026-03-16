package recordings

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

// RecordingsService defines the minimal playback resolution contract needed by the v3 recordings service.
type RecordingsService interface {
	ResolvePlayback(ctx context.Context, id string, profile string) (domainrecordings.PlaybackResolution, error)
	GetMediaTruth(ctx context.Context, id string) (playback.MediaTruth, error)
}

// Deps defines external dependencies for the v3 recordings service.
type Deps interface {
	RecordingsService() RecordingsService
	Config() config.AppConfig
	HostPressure(ctx context.Context) playbackprofile.HostPressureAssessment
}
