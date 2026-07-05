// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package v3

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/hls/llhls"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// llhlsRegistry is created on first use so the eviction goroutine only runs
// when low-latency HLS is actually enabled and requested.
var llhlsRegistry = sync.OnceValue(func() *llhls.Registry {
	return llhls.NewRegistry(llhls.DefaultPartTargetMs)
})

// llhlsBlockingReloadTimeout bounds how long a blocking playlist reload may
// wait server-side. Apple's spec expects the server to respond as soon as
// the requested part exists; the bound only guards against stalled encoders.
const llhlsBlockingReloadTimeout = 6 * time.Second

// serveLLHLSPlaylist renders the low-latency playlist for a live session,
// honoring the _HLS_msn/_HLS_part blocking-reload contract. It returns false
// when the LL path cannot serve (session invalid, playlist not ready yet),
// in which case the caller falls back to plain file serving so session
// startup behaves exactly as before.
func (s *Server) serveLLHLSPlaylist(w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps, sessionID string) bool {
	store := deps.store
	rec, err := store.GetSession(r.Context(), sessionID)
	if err != nil || rec == nil {
		return false
	}
	if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
		return false
	}
	validState := rec.State == model.SessionReady ||
		rec.State == model.SessionDraining ||
		rec.State == model.SessionStarting ||
		rec.State == model.SessionNew ||
		rec.State == model.SessionPriming
	if !validState {
		return false
	}

	dir := paths.LiveSessionDir(deps.cfg.HLS.Root, sessionID)
	tracker := llhlsRegistry().Get(dir, sessionID)

	// Serve the plain playlist until the tracker has indexed real parts (see
	// Tracker.HasParts). Clients that never saw the LL tags do plain reloads
	// at standard latency instead of starving behind a part-sized hold-back.
	if !tracker.HasParts() {
		return false
	}

	msn, part := parseBlockingReloadParams(r)
	out, err := tracker.AwaitAndRender(r.Context(), msn, part, time.Now().Add(llhlsBlockingReloadTimeout))
	if err != nil {
		return false
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	// Blocking-reload responses are inherently per-request; never cache.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Encoding", "identity")
	_, _ = w.Write([]byte(out))
	return true
}

// parseBlockingReloadParams extracts _HLS_msn/_HLS_part. A missing or
// malformed msn disables blocking (render immediately, per spec both params
// are optional); a part without msn is ignored.
func parseBlockingReloadParams(r *http.Request) (msn, part int) {
	msn, part = -1, -1
	q := r.URL.Query()
	if v := q.Get("_HLS_msn"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			msn = n
		}
	}
	if msn >= 0 {
		if v := q.Get("_HLS_part"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				part = n
			}
		}
	}
	return msn, part
}
