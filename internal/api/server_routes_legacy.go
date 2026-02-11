// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/go-chi/chi/v5"
)

func (s *Server) registerLegacyRoutes(r chi.Router, lanGuard *middleware.LANGuard) {
	// PROTECTED: Discovery / Legacy Endpoints
	r.Group(func(r chi.Router) {
		r.Use(lanGuard.RequireLAN)

		// HDHomeRun emulation endpoints (versionless - hardware emulation protocol)
		if s.hdhr != nil {
			r.Get("/discover.json", s.hdhr.HandleDiscover)
			r.Get("/lineup_status.json", s.hdhr.HandleLineupStatus)
			r.Get("/lineup.json", s.handleLineupJSON)
			r.Post("/lineup.json", s.hdhr.HandleLineupPost)
			r.HandleFunc("/lineup.post", s.hdhr.HandleLineupPost) // supports both GET and POST
			r.Get("/device.xml", s.hdhr.HandleDeviceXML)
		}

		// XMLTV endpoint (versionless - standard format)
		r.Method(http.MethodGet, "/xmltv.xml", http.HandlerFunc(s.handleXMLTV))
		r.Method(http.MethodHead, "/xmltv.xml", http.HandlerFunc(s.handleXMLTV))

		// Internal playlist export
		// Internal playlist export
		// Legacy endpoint: /playlist.m3u (serves the current playlist file, whatever it is)
		r.Get("/playlist.m3u", func(w http.ResponseWriter, r *http.Request) {
			s.mu.RLock()
			cfg := s.cfg
			snap := s.snap
			s.mu.RUnlock()

			playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
			if err != nil {
				log.L().Error().Err(err).Msg("playlist path rejected")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			http.ServeFile(w, r, playlistPath)
		})

		// Modern endpoint: /playlist.m3u8 (sets correct MIME type)
		r.Get("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
			s.mu.RLock()
			cfg := s.cfg
			snap := s.snap
			s.mu.RUnlock()

			playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, snap.Runtime.PlaylistFilename)
			if err != nil {
				log.L().Error().Err(err).Msg("playlist path rejected")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", controlhttp.ContentTypeHLSPlaylist)
			http.ServeFile(w, r, playlistPath)
		})
	})

	// PUBLIC (or Internal Auth): Logo Proxy (Needs access from Players)
	// Some players (esp mobile) might come from outside if strict LAN isn't perfect,
	// but for now we treat logos as discovery assets.
	r.With(lanGuard.RequireLAN).Get("/logos/{ref}.png", s.handlePicons)
	r.With(lanGuard.RequireLAN).Head("/logos/{ref}.png", s.handlePicons)

	// Harden file server: disable directory listing and use a secure handler
	// NOTE: fileserver applies its own allowlist, but we add LAN guard for depth.
	r.With(lanGuard.RequireLAN).Handle("/files/*", http.StripPrefix("/files/", controlhttp.SecureFileServer(s.cfg.DataDir, controlhttp.NewPromFileMetrics())))
}
