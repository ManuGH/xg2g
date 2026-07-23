package sessions

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// SessionStore defines the minimal session persistence contract needed by the v3 sessions service.
type SessionStore interface {
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
	UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error)
}

// Deps defines external dependencies for the v3 sessions service.
type Deps interface {
	SessionStore() SessionStore
	Config() config.AppConfig
	Bus() bus.Bus
	CapabilityRegistry() capreg.Store
	ProfileResolver() profiles.Resolver
	RuntimeContext() context.Context
}
