// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

func (s *Server) routes() http.Handler {
	r := s.newRouter()
	s.registerPublicRoutes(r)

	rAuth, rRead, rWrite, rAdmin, rStatus := s.scopedRouters(r)
	s.registerOperatorRoutes(rAuth, rAdmin, rStatus)
	s.registerCanonicalV3Routes(r)
	v3.RegisterCompatibilityRoutes(rRead, rWrite, s.v3Handler)

	return r
}
