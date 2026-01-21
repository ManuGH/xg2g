package v3

import (
	"encoding/json"
	"net/http"

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
