// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package legacy

import (
	"net/http"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/go-chi/chi/v5"
)

const (
	deprecationPolicyURL = "https://github.com/ManuGH/xg2g/blob/main/docs/DEPRECATION_POLICY.md"
	sunsetPlanURL        = "https://github.com/ManuGH/xg2g/blob/main/docs/ops/DEPRECATION_SUNSET.md"
)

// RegisterRoutes wires legacy/versionless endpoints onto the shared router.
func RegisterRoutes(r chi.Router, runtime Runtime, lanGuard *middleware.LANGuard) {
	if runtime == nil {
		panic("legacy routes: runtime is nil")
	}
	if lanGuard == nil {
		panic("legacy routes: lan guard is nil")
	}

	r.Group(func(r chi.Router) {
		r.Use(lanGuard.RequireLAN, legacyDeprecationHeaders)

		if h := runtime.HDHomeRunServer(); h != nil {
			r.Get("/discover.json", h.HandleDiscover)
			r.Get("/lineup_status.json", h.HandleLineupStatus)
			r.Get("/lineup.json", func(w http.ResponseWriter, r *http.Request) {
				HandleLineupJSON(w, r, runtime)
			})
			r.Post("/lineup.json", h.HandleLineupPost)
			r.HandleFunc("/lineup.post", h.HandleLineupPost)
			r.Get("/device.xml", h.HandleDeviceXML)
		}

		r.Method(http.MethodGet, "/xmltv.xml", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			HandleXMLTV(w, r, runtime)
		}))
		r.Method(http.MethodHead, "/xmltv.xml", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			HandleXMLTV(w, r, runtime)
		}))

		r.Get("/playlist.m3u", func(w http.ResponseWriter, r *http.Request) {
			servePlaylist(w, r, runtime, false)
		})
		r.Get("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
			servePlaylist(w, r, runtime, true)
		})
	})

	r.With(lanGuard.RequireLAN, legacyDeprecationHeaders).Get("/logos/{ref}.png", func(w http.ResponseWriter, r *http.Request) {
		HandlePicons(w, r, runtime)
	})
	r.With(lanGuard.RequireLAN, legacyDeprecationHeaders).Head("/logos/{ref}.png", func(w http.ResponseWriter, r *http.Request) {
		HandlePicons(w, r, runtime)
	})

	cfg := runtime.CurrentConfig()
	r.With(lanGuard.RequireLAN, legacyDeprecationHeaders).Handle("/files/*", http.StripPrefix("/files/", controlhttp.SecureFileServer(cfg.DataDir, controlhttp.NewPromFileMetrics())))
}

func servePlaylist(w http.ResponseWriter, r *http.Request, runtime Runtime, withHLSContentType bool) {
	cfg := runtime.CurrentConfig()
	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, runtime.PlaylistFilename())
	if err != nil {
		log.L().Error().Err(err).Msg("playlist path rejected")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if withHLSContentType {
		w.Header().Set("Content-Type", controlhttp.ContentTypeHLSPlaylist)
	}
	http.ServeFile(w, r, playlistPath)
}

func legacyDeprecationHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Add("Link", "<"+deprecationPolicyURL+">; rel=\"deprecation\"")
		w.Header().Add("Link", "<"+sunsetPlanURL+">; rel=\"sunset\"")
		next.ServeHTTP(w, r)
	})
}
