// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"

	"github.com/ManuGH/xg2g/internal/channels"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/ManuGH/xg2g/internal/verification"
)

type v3DependencySnapshot struct {
	handler             *v3.Server
	runtimeDeps         v3.Dependencies
	recordingPathMapper *recordings.PathMapper
	channelManager      *channels.Manager
	seriesManager       *dvr.Manager
	seriesEngine        *dvr.SeriesEngine
	vodManager          *vod.Manager
	epgCache            *epg.TV
	healthManager       *health.Manager
	recordingsService   recservice.Service
	requestShutdown     func(context.Context) error
	preflightProvider   v3.PreflightProvider
}

// V3Overrides applies optional test/runtime overrides that are not part of core runtime wiring.
type V3Overrides struct {
	VerificationStore verification.Store
	VODProber         vod.Prober
	Resolver          recservice.Resolver
	RecordingsService recservice.Service
}
