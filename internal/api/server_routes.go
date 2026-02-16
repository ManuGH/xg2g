// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
)

func (s *Server) routes() http.Handler {
	r := s.newRouter()
	s.registerPublicRoutes(r)

	rAuth, rRead, rWrite, rAdmin, rStatus := s.scopedRouters(r)
	s.registerOperatorRoutes(rAuth, rAdmin, rStatus)
	s.registerCanonicalV3Routes(r)
	s.registerManualV3Routes(rRead, rWrite)
	s.registerClientPlaybackRoutes(rRead)

	s.registerLegacyRoutes(r, s.newLANGuard())

	return r
}
