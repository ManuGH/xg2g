package hls

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

type LeaseRenewer interface {
	RenewLeaseFromConsumption(ctx context.Context, sessionID string)
}

type LivePreviewServer interface {
	ServeLivePreviewFrame(w context.Context, r context.Context, root string, segmentSeconds int, ffmpegBin, sessionID string)
}

type Deps interface {
	Config() config.AppConfig
	SessionStore() v3sessions.SessionStore
	StoreRegistry() store.StoreRegistry
	LeaseRenewer() LeaseRenewer
}
