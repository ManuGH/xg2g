// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
)

func (s *Server) routes() http.Handler {
	return s.buildRouter()
}
