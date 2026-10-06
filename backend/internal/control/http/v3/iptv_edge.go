// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/scrubber"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// resolveClientServiceRef resolves an inbound client-supplied service reference
// using the Server's IPTVResolver.
//
// Semantics:
//   - KindPassThrough / KindLegacyRaw: returns the raw ref and ok=true (legacy raw also increments legacy metric).
//   - KindOpaque: returns the resolved internal raw ref and ok=true.
//   - edge.ErrNotFound: writes HTTP 404 ProblemDetails (CodeNotFound) without echoing input, returns ok=false.
//   - edge.ErrInvalidID: writes HTTP 400 ProblemDetails (CodeInvalidInput) without echoing input, returns ok=false.
//   - Nil resolver: behaves as disabled (opaque -> 404, pass-through/legacy -> ok).
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

// SetIPTVResolver sets or updates the injected IPTV edge resolver in a concurrency-safe manner.
func (s *Server) SetIPTVResolver(r *edge.Resolver) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.iptvResolver = r
	s.mu.Unlock()
	if r != nil {
		log.SetGlobalScrubber(scrubber.New(r).Scrub)
	} else {
		log.SetGlobalScrubber(nil)
	}
}

// isIPTVRef reports whether the normalized ref has an Enigma2 IPTV service type.
func isIPTVRef(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	parts := strings.Split(trimmed, ":")
	return len(parts) > 0 && sourceref.IsIPTVServiceType(parts[0])
}

// maskServiceRef masks an outbound service reference to opaque iptv_<id> if it is IPTV.
// Non-IPTV references (such as DVB 1:...) and references that are already opaque (iptv_...)
// are preserved untouched.
// If the resolver is disabled or missing and the reference is IPTV, it fails closed and returns "".
func (s *Server) maskServiceRef(rawRef string) string {
	if s == nil {
		if isIPTVRef(rawRef) {
			return ""
		}
		return rawRef
	}
	resolver := s.IPTVResolver()
	if resolver == nil {
		if isIPTVRef(rawRef) {
			return ""
		}
		return rawRef
	}
	return resolver.MaskServiceRef(rawRef)
}

// maskServiceRefPtr masks an outbound *string service reference.
// If ref is nil, it returns nil.
// If *ref is an IPTV reference that fails closed, it returns nil.
func (s *Server) maskServiceRefPtr(ref *string) *string {
	if ref == nil {
		return nil
	}
	masked := s.maskServiceRef(*ref)
	if masked == "" && *ref != "" && isIPTVRef(*ref) {
		return nil
	}
	return &masked
}

// maskLogoURL rewrites an outbound logo or picon URL to /logos/iptv_<id>.png if the service is IPTV.
// Non-IPTV logo URLs are preserved untouched.
func (s *Server) maskLogoURL(logoURL, rawRef string) string {
	if s == nil {
		if isIPTVRef(rawRef) {
			return ""
		}
		return logoURL
	}
	resolver := s.IPTVResolver()
	if resolver == nil {
		if isIPTVRef(rawRef) {
			return ""
		}
		return logoURL
	}
	return resolver.MaskLogoURL(logoURL, rawRef)
}

// maskHouseholdProfile masks the service references inside a household Profile.
func (s *Server) maskHouseholdProfile(p household.Profile) household.Profile {
	p.AllowedServiceRefs = s.maskServiceRefs(p.AllowedServiceRefs)
	p.FavoriteServiceRefs = s.maskServiceRefs(p.FavoriteServiceRefs)
	return p
}

// maskHouseholdProfiles masks the service references inside a slice of household Profiles.
func (s *Server) maskHouseholdProfiles(profs []household.Profile) []household.Profile {
	if profs == nil {
		return nil
	}
	out := make([]household.Profile, len(profs))
	for i, p := range profs {
		out[i] = s.maskHouseholdProfile(p)
	}
	return out
}

// maskAccessPolicy masks an identity AccessPolicy (no service references contained).
func (s *Server) maskAccessPolicy(pol *identity.AccessPolicy) *identity.AccessPolicy {
	return pol
}

// maskProfilePolicy masks the blocked channels in an identity ProfilePolicy.
func (s *Server) maskProfilePolicy(pol *identity.ProfilePolicy) *identity.ProfilePolicy {
	if pol == nil {
		return nil
	}
	polCopy := *pol
	polCopy.BlockedChannels = s.maskServiceRefs(pol.BlockedChannels)
	return &polCopy
}

// maskServiceRefs masks a slice of service references. Any IPTV references that fail closed are omitted.
func (s *Server) maskServiceRefs(refs []string) []string {
	if refs == nil {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		masked := s.maskServiceRef(r)
		if masked != "" {
			out = append(out, masked)
		}
	}
	return out
}
