// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	v3tokens "github.com/ManuGH/xg2g/internal/control/http/v3/tokens"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// SessionStore defines session persistence access needed by playbackinfo.
type SessionStore interface {
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
}

// Deps defines infrastructure dependencies required by Service.
type Deps interface {
	Config() config.AppConfig
	SessionStore() SessionStore
	CapabilityRegistry() capreg.Store
	ProfileResolver() profiles.Resolver
	TokensService() *v3tokens.Service
	JWTSecret() []byte
	RuntimeContext() context.Context
}
