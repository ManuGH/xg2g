package recordings

import (
	"context"

	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
)

// RecordingsService defines the minimal playback resolution contract needed by the v3 recordings service.
type RecordingsService interface {
	ResolvePlayback(ctx context.Context, id string, profile string) (domainrecordings.PlaybackResolution, error)
}

// Deps defines external dependencies for the v3 recordings service.
type Deps interface {
	RecordingsService() RecordingsService
}
