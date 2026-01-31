package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// GetReceiverCurrent implements the receiver current service endpoint
// GET /api/v3/receiver/current
func (s *Server) GetReceiverCurrent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get OpenWebIF client
	owiClient := s.owi(s.cfg, s.snap)
	client, ok := owiClient.(*openwebif.Client)
	if !ok || client == nil {
		writeProblem(w, r, http.StatusServiceUnavailable,
			"receiver/client_unavailable",
			"OpenWebIF Client Unavailable",
			"CLIENT_UNAVAILABLE",
			"Cannot query receiver: client not initialized", nil)
		return
	}
	// Check power state first to avoid "Phantom Live" status
	// If receiver is in standby, we should report unavailable/idle instead of the last active channel.
	// Use a short timeout (2s) so the dashboard doesn't hang in "Loading..." if the box is offline.
	statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	st, err := client.GetStatusInfo(statusCtx)
	if err != nil {
		// If status check fails (timeout, network error, etc.), assume receiver is unavailable/standby.
		// Returning 503 causes the UI to stick in "Loading..." or error state.
		// Returning "unavailable" status allows the UI to show "Receiver in Standby" / "Idle".
		log.L().Warn().Err(err).Msg("failed to check receiver status, assuming unavailable")

		unavailable := CurrentServiceInfoStatusUnavailable
		resp := CurrentServiceInfo{
			Status: &unavailable,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if st.InStandby == "true" {
		unavailable := CurrentServiceInfoStatusUnavailable
		resp := CurrentServiceInfo{
			Status: &unavailable,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Get current service info with singleflight protection
	// Get current service info with singleflight protection
	val, err, _ := s.receiverSfg.Do("getcurrent", func() (interface{}, error) {
		return client.GetCurrent(ctx)
	})

	if err != nil {
		writeProblem(w, r, http.StatusBadGateway,
			"receiver/upstream_error",
			"Failed to Query Current Service",
			"UPSTREAM_ERROR",
			err.Error(), nil)
		return
	}

	current := val.(*openwebif.CurrentInfo)

	// Check for missing "Now" event data (Phantom Live / No EPG issue)
	// Some receivers return empty Now data in GetCurrent even if EPG is available.
	if current.Now.EventTitle == "" && current.Info.ServiceRef != "" {
		log.L().Debug().Msg("missing now event, attempting epg fallback")

		// Use the existing statusCtx (2s timeout) or create a new short one
		// We reuse statusCtx logic but we need a fresh timeout here since the previous one might be used up
		epgCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		events, err := client.GetServiceEPG(epgCtx, current.Info.ServiceRef)
		if err == nil {
			nowTs := time.Now().Unix()
			for _, event := range events {
				// Find event that overlaps with now
				if nowTs >= event.Begin && nowTs < (event.Begin+event.Duration) {
					log.L().Info().Str("title", event.Title).Msg("found fallback epg event")
					current.Now.EventTitle = event.Title
					current.Now.EventDescription = event.Description // Short desc in OWI mapping
					// OWI Client might map Description to Short/Long differently, passing Description usually safe
					// Let's verify mapping: EPGEvent.Description is ShortDesc.

					current.Now.EventStart = event.Begin
					current.Now.EventDuration = int(event.Duration)
					break
				}
			}
		} else {
			log.L().Warn().Err(err).Msg("failed to fetch fallback epg")
		}
	}

	// Build response
	okStatus := CurrentServiceInfoStatusOk
	resp := CurrentServiceInfo{
		Status: &okStatus,
		Channel: &struct {
			Name *string `json:"name,omitempty"`
			Ref  *string `json:"ref,omitempty"`
		}{
			Name: &current.Info.ServiceName,
			Ref:  &current.Info.ServiceRef,
		},
		Now: &struct {
			BeginTimestamp *int64  `json:"beginTimestamp,omitempty"`
			Description    *string `json:"description,omitempty"`
			DurationSec    *int    `json:"durationSec,omitempty"`
			Title          *string `json:"title,omitempty"`
		}{
			Title:          &current.Now.EventTitle,
			Description:    &current.Now.EventDescription,
			BeginTimestamp: &current.Now.EventStart,
			DurationSec:    &current.Now.EventDuration,
		},
		Next: &struct {
			Title *string `json:"title,omitempty"`
		}{
			Title: &current.Next.EventTitle,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
