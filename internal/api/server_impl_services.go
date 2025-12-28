// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import "net/http"

// PostServicesNowNext implements POST /services/now-next.
func (s *Server) PostServicesNowNext(w http.ResponseWriter, r *http.Request) {
	s.handleNowNextEPG(w, r)
}
