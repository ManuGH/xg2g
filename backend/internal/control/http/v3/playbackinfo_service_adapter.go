package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	v3tokens "github.com/ManuGH/xg2g/internal/control/http/v3/tokens"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type serverPlaybackInfoDeps struct {
	s *Server
}

var _ v3playbackinfo.Deps = (*serverPlaybackInfoDeps)(nil)

func (d *serverPlaybackInfoDeps) SessionStore() v3playbackinfo.SessionStore {
	return d.s.sessionsModuleDeps().store
}

func (d *serverPlaybackInfoDeps) Config() config.AppConfig {
	return d.s.sessionsModuleDeps().cfg
}

func (d *serverPlaybackInfoDeps) CapabilityRegistry() capreg.Store {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.capabilityRegistry
}

func (d *serverPlaybackInfoDeps) ProfileResolver() profiles.Resolver {
	return d.s.profileResolver
}

func (d *serverPlaybackInfoDeps) TokensService() *v3tokens.Service {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.tokensService
}

func (d *serverPlaybackInfoDeps) JWTSecret() []byte {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return append([]byte(nil), d.s.JWTSecret...)
}

func (d *serverPlaybackInfoDeps) RuntimeContext() context.Context {
	return d.s.runtimeContextOrBackground()
}

func (s *Server) playbackInfoProcessor() *v3playbackinfo.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playbackInfoV3Service == nil {
		s.playbackInfoV3Service = v3playbackinfo.NewService(&serverPlaybackInfoDeps{s: s})
	}
	return s.playbackInfoV3Service
}
