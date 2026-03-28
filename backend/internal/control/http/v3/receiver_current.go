package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type scheduleEntry struct {
	title string
	desc  string
	start int64
	end   int64
}

// GetReceiverCurrent implements the receiver current service endpoint
// GET /api/v3/receiver/current
func (s *Server) GetReceiverCurrent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get OpenWebIF client
	owiClient := s.owi(s.cfg, s.snap)
	client, ok := owiClient.(*openwebif.Client)
	if !ok || client == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable,
			"receiver/client_unavailable",
			"OpenWebIF Client Unavailable",
			problemcode.CodeClientUnavailable,
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

	if standbyKnown, inStandby := parseOWIBoolString(st.InStandby); standbyKnown && inStandby {
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
		writeRegisteredProblem(w, r, http.StatusBadGateway,
			"receiver/upstream_error",
			"Failed to Query Current Service",
			problemcode.CodeUpstreamError,
			err.Error(), nil)
		return
	}

	current := val.(*openwebif.CurrentInfo)

	// Check for missing "Now" event data (Phantom Live / No EPG issue).
	// Some receivers return empty current EPG in /api/getcurrent even if schedule data exists.
	if current.Now.EventTitle == "" && current.Info.ServiceRef != "" {
		log.L().Debug().Msg("missing now event, attempting epg fallback")

		epgCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		events, err := client.GetServiceEPG(epgCtx, current.Info.ServiceRef)
		if err == nil {
			enriched := mergeCurrentInfoFromSchedule(current, time.Now(), scheduleEntriesFromOWIEPG(events))
			if enriched {
				log.L().Info().Str("service_ref", current.Info.ServiceRef).Msg("filled current epg from receiver epg")
			}
		} else {
			log.L().Warn().Err(err).Msg("failed to fetch fallback epg")
		}

		if current.Now.EventTitle == "" {
			if enriched, err := s.fillCurrentFromLocalEPG(epgCtx, current); err != nil {
				log.L().Warn().Err(err).Msg("failed to load local epg fallback")
			} else if enriched {
				log.L().Info().Str("service_ref", current.Info.ServiceRef).Msg("filled current epg from local xmltv")
			}
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

func (s *Server) fillCurrentFromLocalEPG(ctx context.Context, current *openwebif.CurrentInfo) (bool, error) {
	s.mu.RLock()
	src := s.epgSource
	s.mu.RUnlock()
	if src == nil {
		return false, nil
	}

	programmes, err := src.GetPrograms(ctx)
	if err != nil {
		return false, err
	}

	return mergeCurrentInfoFromSchedule(
		current,
		time.Now(),
		scheduleEntriesFromProgrammes(current.Info.ServiceRef, programmes),
	), nil
}

func scheduleEntriesFromOWIEPG(events []openwebif.EPGEvent) []scheduleEntry {
	entries := make([]scheduleEntry, 0, len(events))
	for _, event := range events {
		if event.Title == "" || event.Begin <= 0 || event.Duration <= 0 {
			continue
		}
		desc := event.Description
		if desc == "" {
			desc = event.DescriptionFallback
		}
		if desc == "" {
			desc = event.LongDesc
		}
		if desc == "" {
			desc = event.LongDescFallback
		}
		entries = append(entries, scheduleEntry{
			title: event.Title,
			desc:  desc,
			start: event.Begin,
			end:   event.Begin + event.Duration,
		})
	}
	return entries
}

func scheduleEntriesFromProgrammes(serviceRef string, programmes []epg.Programme) []scheduleEntry {
	targetRef := read.CanonicalServiceRef(serviceRef)
	if targetRef == "" {
		return nil
	}

	entries := make([]scheduleEntry, 0, len(programmes))
	for _, programme := range programmes {
		if read.CanonicalServiceRef(programme.Channel) != targetRef {
			continue
		}

		start, err := time.Parse("20060102150405 -0700", programme.Start)
		if err != nil {
			continue
		}
		end, err := time.Parse("20060102150405 -0700", programme.Stop)
		if err != nil {
			continue
		}
		if !end.After(start) || programme.Title.Text == "" {
			continue
		}

		desc := ""
		if programme.Desc != nil {
			desc = programme.Desc.Text
		}

		entries = append(entries, scheduleEntry{
			title: programme.Title.Text,
			desc:  desc,
			start: start.Unix(),
			end:   end.Unix(),
		})
	}

	return entries
}

func mergeCurrentInfoFromSchedule(current *openwebif.CurrentInfo, now time.Time, entries []scheduleEntry) bool {
	nowTS := now.Unix()
	var currentEntry *scheduleEntry
	var nextEntry *scheduleEntry

	for i := range entries {
		entry := &entries[i]
		if entry.end <= entry.start || entry.title == "" {
			continue
		}
		if nowTS >= entry.start && nowTS < entry.end {
			if currentEntry == nil || entry.start > currentEntry.start {
				currentEntry = entry
			}
			continue
		}
		if entry.start > nowTS && (nextEntry == nil || entry.start < nextEntry.start) {
			nextEntry = entry
		}
	}

	var changed bool
	if currentEntry != nil {
		if current.Now.EventTitle == "" {
			current.Now.EventTitle = currentEntry.title
			changed = true
		}
		if current.Now.EventDescription == "" && currentEntry.desc != "" {
			current.Now.EventDescription = currentEntry.desc
			changed = true
		}
		if current.Now.EventStart == 0 {
			current.Now.EventStart = currentEntry.start
			changed = true
		}
		if current.Now.EventDuration <= 0 {
			current.Now.EventDuration = int(currentEntry.end - currentEntry.start)
			changed = true
		}
	}

	if nextEntry != nil {
		if current.Next.EventTitle == "" {
			current.Next.EventTitle = nextEntry.title
			changed = true
		}
		if current.Next.EventDescription == "" && nextEntry.desc != "" {
			current.Next.EventDescription = nextEntry.desc
			changed = true
		}
		if current.Next.EventStart == 0 {
			current.Next.EventStart = nextEntry.start
			changed = true
		}
		if current.Next.EventDuration <= 0 {
			current.Next.EventDuration = int(nextEntry.end - nextEntry.start)
			changed = true
		}
	}

	return changed
}
