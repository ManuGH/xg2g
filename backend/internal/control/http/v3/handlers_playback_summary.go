package v3

import (
	"encoding/json"
	"net/http"
	"sync"
	"unsafe"

	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

const (
	playbackSummaryMaxRefs    = 100
	playbackSummaryConcurrent = 8
)

// PostLivePlaybackSummary implements ServerInterface (batch EPG badges).
//
// Resolves the playback decision for many live services in one request. This
// is a passive display endpoint: it always runs in the epg_badge request
// context (no cold-relay probe fan-out, no decision tokens), and services
// that fail to resolve are omitted from the response instead of failing the
// whole batch.
func (s *Server) PostLivePlaybackSummary(w http.ResponseWriter, r *http.Request) {
	var req PostLivePlaybackSummaryJSONRequestBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		v3playbackinfo.WritePlaybackInfoInputProblem(w, r, &v3playbackinfo.PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "Failed to parse request body: " + err.Error(),
		})
		return
	}
	if req.Capabilities.CapabilitiesVersion < 1 {
		v3playbackinfo.WritePlaybackInfoInputProblem(w, r, &v3playbackinfo.PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidCapabilities,
			Detail:      "capabilities_version must be >= 1",
		})
		return
	}
	if len(req.ServiceRefs) == 0 || len(req.ServiceRefs) > playbackSummaryMaxRefs {
		v3playbackinfo.WritePlaybackInfoInputProblem(w, r, &v3playbackinfo.PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "serviceRefs must contain between 1 and 100 entries",
		})
		return
	}

	profile := household.NormalizeProfile(s.currentHouseholdProfile(r.Context()))
	var visibleRefs map[string]struct{}
	if household.HasServiceRestrictionsNormalized(profile) {
		refs, err := s.householdVisibleServiceRefSet(profile, s.systemModuleDeps())
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/service_resolution_failed", "Household Service Resolution Failed", problemcode.CodeReadFailed, "Failed to resolve visible household services", nil)
			return
		}
		visibleRefs = refs
	}

	// Force the passive preview context for the whole batch so downstream
	// resolution never triggers cold-relay probes or issues decision tokens.
	r.Header.Set(v3recordings.PlaybackInfoContextHeader, v3recordings.PlaybackInfoContextEpgBadge)

	caps := (*v3playbackinfo.PlaybackCapabilities)(unsafe.Pointer(&req.Capabilities))
	deps := s.recordingsModuleDeps()

	type refJob struct {
		original string
		resolved string
	}
	jobs := make([]refJob, 0, len(req.ServiceRefs))
	seen := make(map[string]struct{}, len(req.ServiceRefs))
	for _, raw := range req.ServiceRefs {
		serviceRef := normalize.ServiceRef(raw)
		if serviceRef == "" || recordings.ValidateLiveRef(serviceRef) != nil {
			continue
		}
		if _, dup := seen[serviceRef]; dup {
			continue
		}
		seen[serviceRef] = struct{}{}
		if visibleRefs != nil {
			if _, ok := visibleRefs[read.CanonicalServiceRef(serviceRef)]; !ok {
				continue
			}
		}
		jobs = append(jobs, refJob{original: raw, resolved: serviceRef})
	}

	items := make(map[string]v3playbackinfo.PlaybackInfo, len(jobs))
	var itemsMu sync.Mutex
	sem := make(chan struct{}, playbackSummaryConcurrent)
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(job refJob) {
			defer wg.Done()
			defer func() { <-sem }()
			serviceRequest := v3playbackinfo.BuildPlaybackInfoServiceRequest(r, job.resolved, caps, "v3.1", "live")
			dto, err := s.buildPlaybackInfoHTTPResponse(r.Context(), deps, job.resolved, caps, "live", serviceRequest)
			if err != nil {
				return // omitted from the batch by design
			}
			itemsMu.Lock()
			items[job.original] = dto
			itemsMu.Unlock()
		}(job)
	}
	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}
