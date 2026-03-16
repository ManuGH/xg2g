package sessions

import (
	"context"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// SessionStore defines the minimal session persistence contract needed by the v3 sessions service.
type SessionStore interface {
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
}

// Deps defines external dependencies for the v3 sessions service.
type Deps interface {
	SessionStore() SessionStore
}
