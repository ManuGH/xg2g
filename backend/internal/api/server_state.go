// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// GetEvents implements dvr.EpgProvider interface
func (s *Server) GetEvents(from, to time.Time) ([]openwebif.EPGEvent, error) {
	s.mu.RLock()
	cache := s.epgCache
	s.mu.RUnlock()

	if cache == nil {
		return nil, nil // No EPG data
	}

	var events []openwebif.EPGEvent

	for _, p := range cache.Programs {
		// Parse times
		// formatXMLTVTime: "20060102150405 -0700"
		start, err := time.Parse("20060102150405 -0700", p.Start)
		if err != nil {
			continue
		}

		// Optimization: Skip if outside window
		if start.After(to) {
			continue
		}

		stop, err := time.Parse("20060102150405 -0700", p.Stop)
		if err != nil {
			// Fallback: 30 mins
			stop = start.Add(30 * time.Minute)
		}

		if stop.Before(from) {
			continue
		}

		// Convert to EPGEvent
		evt := openwebif.EPGEvent{
			Title:       p.Title.Text,
			Description: p.Desc.Text,
			Begin:       start.Unix(),
			Duration:    int64(stop.Sub(start).Seconds()),
			SRef:        p.Channel, // Channel ID in XMLTV is SRef
		}
		events = append(events, evt)
	}

	return events, nil
}

// HealthManager returns the health check manager
func (s *Server) HealthManager() *health.Manager {
	return s.healthManager
}

func (s *Server) GetConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// HDHomeRunServer returns the HDHomeRun server instance if enabled
func (s *Server) HDHomeRunServer() *hdhr.Server {
	return s.hdhr
}

// GetSeriesEngine returns the SeriesEngine instance (for scheduler wiring)
func (s *Server) GetSeriesEngine() *dvr.SeriesEngine {
	return s.seriesEngine
}

// GetStatus returns the current server status (thread-safe)
// This method is exposed for use by versioned API handlers
func (s *Server) GetStatus() jobs.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// UpdateStatus updates the server status (thread-safe)
func (s *Server) UpdateStatus(status jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}
