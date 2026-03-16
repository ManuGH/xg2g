package v3

import v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"

type serverSessionDeps struct {
	s *Server
}

var _ v3sessions.Deps = (*serverSessionDeps)(nil)

func (d *serverSessionDeps) SessionStore() v3sessions.SessionStore {
	return d.s.sessionsModuleDeps().store
}

func (s *Server) sessionsProcessor() *v3sessions.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionsV3Service == nil {
		s.sessionsV3Service = v3sessions.NewService(&serverSessionDeps{s: s})
	}
	return s.sessionsV3Service
}
