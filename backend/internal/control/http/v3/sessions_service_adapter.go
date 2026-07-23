package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type serverSessionDeps struct {
	s *Server
}

var _ v3sessions.Deps = (*serverSessionDeps)(nil)

func (d *serverSessionDeps) SessionStore() v3sessions.SessionStore {
	return d.s.sessionsModuleDeps().store
}

func (d *serverSessionDeps) Config() config.AppConfig {
	return d.s.sessionsModuleDeps().cfg
}

func (d *serverSessionDeps) Bus() bus.Bus {
	return d.s.sessionsModuleDeps().bus
}

func (d *serverSessionDeps) CapabilityRegistry() capreg.Store {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.capabilityRegistry
}

func (d *serverSessionDeps) ProfileResolver() profiles.Resolver {
	return d.s.profileResolver
}

func (d *serverSessionDeps) RuntimeContext() context.Context {
	return d.s.runtimeContextOrBackground()
}

func (s *Server) sessionsProcessor() *v3sessions.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionsV3Service == nil {
		s.sessionsV3Service = v3sessions.NewService(&serverSessionDeps{s: s})
	}
	return s.sessionsV3Service
}
