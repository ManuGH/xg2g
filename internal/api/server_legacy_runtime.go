// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"github.com/ManuGH/xg2g/internal/config"
	legacyhttp "github.com/ManuGH/xg2g/internal/control/http/legacy"
	"github.com/ManuGH/xg2g/internal/hdhr"
)

type serverLegacyRuntime struct {
	s *Server
}

func (s *Server) legacyRuntime() legacyhttp.Runtime {
	return serverLegacyRuntime{s: s}
}

func (r serverLegacyRuntime) CurrentConfig() config.AppConfig {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.cfg
}

func (r serverLegacyRuntime) PlaylistFilename() string {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.snap.Runtime.PlaylistFilename
}

func (r serverLegacyRuntime) ResolveDataFilePath(rel string) (string, error) {
	return r.s.dataFilePath(rel)
}

func (r serverLegacyRuntime) HDHomeRunServer() *hdhr.Server {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.hdhr
}

func (r serverLegacyRuntime) PiconSemaphore() chan struct{} {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.piconSemaphore
}
