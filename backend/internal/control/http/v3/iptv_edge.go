// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"errors"
	"net/http"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// resolveClientServiceRef resolves an inbound client-supplied service reference
// using the Server's IPTVResolver.
//
// Semantics:
//  - KindPassThrough / KindLegacyRaw: returns the raw ref and ok=true (legacy raw also increments legacy metric).
//  - KindOpaque: returns the resolved internal raw ref and ok=true.
//  - edge.ErrNotFound: writes HTTP 404 ProblemDetails (CodeNotFound) without echoing input, returns ok=false.
//  - edge.ErrInvalidID: writes HTTP 400 ProblemDetails (CodeInvalidInput) without echoing input, returns ok=false.
//  - Nil resolver: behaves as disabled (opaque -> 404, pass-through/legacy -> ok).
func (s *Server) resolveClientServiceRef(w http.ResponseWriter, r *http.Request, endpoint, ref string) (string, bool) {
	normEndpoint := metrics.NormalizeIPTVEndpoint(endpoint)
	var resolver *edge.Resolver
	if s != nil {
		resolver = s.IPTVResolver()
	}

	raw, _, err := resolver.ResolveInbound(normEndpoint, ref)
	if err != nil {
		if errors.Is(err, edge.ErrNotFound) {
			writeRegisteredProblem(w, r, http.StatusNotFound, "", "Resource Not Found", problemcode.CodeNotFound, "iptv source not found", nil)
			return "", false
		}
		if errors.Is(err, edge.ErrInvalidID) {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "", "Invalid Request", problemcode.CodeInvalidInput, "invalid iptv source id", nil)
			return "", false
		}
		writeRegisteredProblem(w, r, http.StatusBadRequest, "", "Invalid Request", problemcode.CodeInvalidInput, "invalid service reference", nil)
		return "", false
	}
	return raw, true
}

// resolveServiceRefElement resolves a single service reference without writing an HTTP response.
// Used for element-wise array resolution in batch endpoints.
func (s *Server) resolveServiceRefElement(endpoint, ref string) (string, edge.Kind, error) {
	normEndpoint := metrics.NormalizeIPTVEndpoint(endpoint)
	var resolver *edge.Resolver
	if s != nil {
		resolver = s.IPTVResolver()
	}
	return resolver.ResolveInbound(normEndpoint, ref)
}
