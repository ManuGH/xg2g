// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Handles EPG data retrieval and serving.
// Non-goals: EPG Parsing logic (see internal/epg).

type nowNextRequest struct {
	Services []string `json:"services"`
}

type epgEntry struct {
	Title string `json:"title,omitempty"`
	Start int64  `json:"start,omitempty"` // unix seconds
	End   int64  `json:"end,omitempty"`   // unix seconds
}

type nowNextItem struct {
	ServiceRef string    `json:"serviceRef"`
	Now        *epgEntry `json:"now,omitempty"`
	Next       *epgEntry `json:"next,omitempty"`
}

// handleNowNextEPG returns now/next EPG for a list of service references.
func (s *Server) handleNowNextEPG(w http.ResponseWriter, r *http.Request) {
	var req nowNextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Services) == 0 {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "epg/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "Request body must contain non-empty services list", nil)
		return
	}

	profile := household.NormalizeProfile(s.currentHouseholdProfile(r.Context()))
	if household.HasServiceRestrictionsNormalized(profile) {
		visibleRefs, err := s.householdVisibleServiceRefSet(profile, s.systemModuleDeps())
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "epg/read_failed", "Failed to Read EPG", problemcode.CodeReadFailed, "Failed to resolve visible household services", nil)
			return
		}
		filtered := make([]string, 0, len(req.Services))
		for _, serviceRef := range req.Services {
			if _, ok := visibleRefs[read.CanonicalServiceRef(serviceRef)]; ok {
				filtered = append(filtered, serviceRef)
			}
		}
		req.Services = filtered
	}

	s.mu.RLock()
	epgCache := s.epgCache
	epgSource := s.epgSource
	s.mu.RUnlock()

	programs := epgCachePrograms(epgCache)
	if len(programs) == 0 && epgSource != nil {
		var err error
		programs, err = epgSource.GetPrograms(r.Context())
		if err != nil {
			log.L().Error().Err(err).Msg("failed to load EPG programs for now/next fallback")
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "epg/read_failed", "Failed to Read EPG", problemcode.CodeReadFailed, "Failed to load now/next EPG data", nil)
			return
		}
	}

	writeNowNextResponse(w, buildNowNextItems(req.Services, programs, time.Now()))
}

func writeNowNextResponse(w http.ResponseWriter, items []nowNextItem) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
	})
}

func buildNowNextItems(serviceRefs []string, programs []epg.Programme, now time.Time) []nowNextItem {
	progMap := make(map[string][]epg.Programme)
	for _, program := range programs {
		canonicalRef := read.CanonicalServiceRef(program.Channel)
		progMap[canonicalRef] = append(progMap[canonicalRef], program)
	}

	items := make([]nowNextItem, 0, len(serviceRefs))
	for _, serviceRef := range serviceRefs {
		progs := progMap[read.CanonicalServiceRef(serviceRef)]
		if len(progs) == 0 {
			items = append(items, nowNextItem{ServiceRef: serviceRef})
			continue
		}

		var current *epgEntry
		var next *epgEntry

		for _, program := range progs {
			start, serr := time.Parse(xmltvTimeFormat, program.Start)
			stop, perr := time.Parse(xmltvTimeFormat, program.Stop)
			if serr != nil || perr != nil {
				continue
			}

			entry := &epgEntry{
				Title: program.Title.Text,
				Start: start.Unix(),
				End:   stop.Unix(),
			}

			if now.After(start) && now.Before(stop) {
				current = entry
			} else if start.After(now) {
				if next == nil || start.Before(time.Unix(next.Start, 0)) {
					next = entry
				}
			}
		}

		items = append(items, nowNextItem{
			ServiceRef: serviceRef,
			Now:        current,
			Next:       next,
		})
	}

	return items
}

func epgCachePrograms(cache *epg.TV) []epg.Programme {
	if cache == nil {
		return nil
	}
	return cache.Programs
}

const xmltvTimeFormat = "20060102150405 -0700"

// EpgItem defines the JSON response structure for an EPG program
type EpgItem struct {
	Id         *string `json:"id,omitempty"`
	ServiceRef string  `json:"serviceRef,omitempty"`
	Title      string  `json:"title,omitempty"`
	Desc       *string `json:"desc,omitempty"`
	Start      int     `json:"start,omitempty"`
	End        int     `json:"end,omitempty"`
	Duration   *int    `json:"duration,omitempty"`
}

// Helper to parse XMLTV dates "20080715003000 +0200"
//
//nolint:unused
func parseXMLTVTime(s string) (time.Time, error) {

	// Format: YYYYMMDDhhmmss ZZZZ
	const layout = "20060102150405 -0700"
	return time.Parse(layout, s)
}

// epgAdapter adapts the server infrastructure to the control-layer EpgSource interface.
type epgAdapter struct {
	s *Server
}

func (w *epgAdapter) GetPrograms(ctx context.Context) ([]epg.Programme, error) {
	s := w.s
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	if strings.TrimSpace(cfg.XMLTVPath) == "" {
		return nil, os.ErrNotExist
	}

	xmltvPath, err := s.dataFilePath(cfg.XMLTVPath)
	if err != nil {
		return nil, err
	}

	// Singleflight for Concurrency Protection
	result, err, _ := s.epgSfg.Do("epg-load", func() (interface{}, error) {
		fileInfo, err := os.Stat(xmltvPath)
		if err != nil {
			log.L().Error().Err(err).Str("path", xmltvPath).Msg("EPG file stat failed")
			return nil, err
		}

		s.mu.Lock()
		if s.epgCache != nil && !fileInfo.ModTime().After(s.epgCacheMTime) {
			defer s.mu.Unlock()
			return s.epgCache, nil
		}
		s.mu.Unlock()

		// Parse
		data, err := os.ReadFile(xmltvPath) // #nosec G304
		if err != nil {
			return nil, err
		}

		var parsedTU epg.TV
		if err := xml.Unmarshal(data, &parsedTU); err != nil {
			s.mu.RLock()
			stale := s.epgCache
			s.mu.RUnlock()
			if stale != nil {
				return stale, nil
			}
			return nil, err
		}

		// Update Cache
		s.mu.Lock()
		s.epgCache = &parsedTU
		s.epgCacheMTime = fileInfo.ModTime()
		s.epgCacheTime = time.Now()
		tvVal := s.epgCache
		s.mu.Unlock()

		return tvVal, nil
	})

	if err != nil {
		return nil, err
	}

	tv := result.(*epg.TV)
	return tv.Programs, nil
}

func (w *epgAdapter) GetBouquetServiceRefs(ctx context.Context, bouquet string) (map[string]struct{}, error) {
	s := w.s
	s.mu.RLock()
	snap := s.snap
	cfg := s.cfg
	s.mu.RUnlock()

	playlistName := snap.Runtime.PlaylistFilename
	playlistName = strings.TrimSpace(playlistName)
	if playlistName == "" {
		return make(map[string]struct{}), nil
	}
	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]struct{}), nil
		}
		return nil, err
	}

	data, err := os.ReadFile(playlistPath) // #nosec G304
	if err != nil {
		// Parity: Legacy ignores filter if playlist read fails (effectively strict filter = empty results)
		// unless search is active (handled by caller).
		// Legacy behavior: "allowedRefs" remains initialized but empty.
		// So we return empty map, NO error.
		return make(map[string]struct{}), nil
	}

	allowedRefs := make(map[string]struct{})
	channels := m3u.Parse(string(data))

	for _, ch := range channels {
		if ch.Group != bouquet {
			continue
		}
		if ch.TvgID != "" {
			allowedRefs[ch.TvgID] = struct{}{}
		}
	}

	return allowedRefs, nil
}

// GetEpg implements ServerInterface
func (s *Server) GetEpg(w http.ResponseWriter, r *http.Request, params GetEpgParams) {
	s.mu.RLock()
	src := s.epgSource
	s.mu.RUnlock()
	profile := household.NormalizeProfile(s.currentHouseholdProfile(r.Context()))

	q := read.EpgQuery{}
	if params.From != nil {
		q.From = int64(*params.From)
	}
	if params.To != nil {
		q.To = int64(*params.To)
	}
	if params.Bouquet != nil {
		q.Bouquet = *params.Bouquet
	}
	if params.Q != nil {
		q.Q = *params.Q
	}

	entries, err := read.GetEpg(r.Context(), src, q, read.RealClock{})
	if err != nil {
		log.L().Error().Err(err).Msg("failed to load EPG")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Internal Server Error", problemcode.CodeInternalError, "Failed to load EPG data", nil)
		return
	}

	var visibleRefs map[string]struct{}
	if household.HasServiceRestrictionsNormalized(profile) {
		visibleRefs, err = s.householdVisibleServiceRefSet(profile, s.systemModuleDeps())
		if err != nil {
			log.L().Error().Err(err).Msg("failed to resolve visible household services for epg")
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "epg/read_failed", "Failed to Read EPG", problemcode.CodeReadFailed, "Failed to resolve visible household services", nil)
			return
		}
	}

	resp := make([]EpgItem, 0, len(entries))
	for _, e := range entries {
		if visibleRefs != nil {
			if _, ok := visibleRefs[e.ServiceRef]; !ok {
				continue
			}
		}
		// Capture variables for pointer assignment
		id := e.ID
		sRef := e.ServiceRef
		title := e.Title
		desc := e.Desc
		start := int(e.Start)
		end := int(e.End)
		dur := int(e.Duration)

		resp = append(resp, EpgItem{
			Id:         &id,
			ServiceRef: sRef,
			Title:      title,
			Desc:       &desc,
			Start:      start,
			End:        end,
			Duration:   &dur,
		})
	}

	if len(resp) == 0 {
		resp = nil
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PostServicesNowNext implements POST /services/now-next.
func (s *Server) PostServicesNowNext(w http.ResponseWriter, r *http.Request) {
	s.handleNowNextEPG(w, r)
}
