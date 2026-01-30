//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type IntentHandler struct {
	Store store.StateStore
	Bus   bus.Bus
	// TTL for idempotency key mapping.
	IdempotencyTTL time.Duration
	// DVRWindowSec overrides DVR window for DVR profiles; 0 uses internal default.
	DVRWindowSec int
	// LeaseTTL controls admission lease duration; 0 uses default.
	LeaseTTL time.Duration
	// TunerSlots is the admission capacity set; empty means no capacity configured.
	TunerSlots []int
}

func (h IntentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	var req IntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ServiceRef) == "" {
		http.Error(w, "serviceRef is required", http.StatusBadRequest)
		return
	}
	correlationID, err := NormalizeCorrelationID(req.CorrelationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode := model.ModeLive
	if raw := strings.TrimSpace(req.Params["mode"]); raw != "" {
		if strings.EqualFold(raw, model.ModeRecording) {
			http.Error(w, "recording playback uses /recordings", http.StatusBadRequest)
			return
		}
		if !strings.EqualFold(raw, model.ModeLive) {
			http.Error(w, "unsupported playback mode", http.StatusBadRequest)
			return
		}
	}

	// Idempotency: Prefer header, fall back to request field.
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		idem = strings.TrimSpace(req.IdempotencyKey)
	}

	if h.IdempotencyTTL <= 0 {
		h.IdempotencyTTL = 5 * time.Minute
	}

	if idem != "" {
		if existing, ok, err := h.Store.GetIdempotency(ctx, idem); err == nil && ok {
			respCorrelationID := correlationID
			if respCorrelationID == "" {
				if session, err := h.Store.GetSession(ctx, existing); err == nil && session != nil {
					respCorrelationID = session.CorrelationID
				}
			}
			h.respond202(ctx, w, existing, respCorrelationID)
			return
		} else if err != nil {
			http.Error(w, "idempotency lookup failed", http.StatusInternalServerError)
			return
		}
	}

	sessionID := newID()
	if correlationID == "" {
		correlationID = newID()
	}
	if h.LeaseTTL <= 0 {
		h.LeaseTTL = 30 * time.Second
	}
	if len(h.TunerSlots) == 0 {
		http.Error(w, "no tuner slots configured", http.StatusServiceUnavailable)
		return
	}
	dvrWindowSec := h.DVRWindowSec
	if dvrWindowSec <= 0 {
		dvrWindowSec = 300
	}

	// ADR-00X: Streaming Profiles are removed.
	// We enforce the single "universal" delivery policy.
	// Legacy fields (profile/pID) are ignored if present in JSON payload.
	// Actually, schema removal means they won't be unmarshalled into the struct if struct tag is gone.
	// So we don't need to check them.

	// Use "universal" policy
	policyID := "universal"

	// Resolve profile (universal)
	prof := profiles.Resolve(policyID, r.UserAgent(), dvrWindowSec, nil, hardware.HasVAAPI(), profiles.HWAccelAuto)

	var acquiredLeases []store.Lease
	releaseLeases := func() {
		for _, l := range acquiredLeases {
			_ = h.Store.ReleaseLease(ctx, l.Key(), l.Owner())
		}
	}

	if mode == model.ModeLive {
		dedupKey := lease.LeaseKeyService(req.ServiceRef)
		dedupLease, ok, err := h.Store.TryAcquireLease(ctx, dedupKey, sessionID, h.LeaseTTL)
		if err != nil {
			http.Error(w, "lease acquisition failed", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "lease busy", http.StatusConflict)
			return
		}
		acquiredLeases = append(acquiredLeases, dedupLease)

		tunerLease, ok, err := tryAcquireTunerLease(ctx, h.Store, sessionID, h.TunerSlots, h.LeaseTTL)
		if err != nil {
			releaseLeases()
			http.Error(w, "tuner lease acquisition failed", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			releaseLeases()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "lease busy", http.StatusConflict)
			return
		}
		acquiredLeases = append(acquiredLeases, tunerLease)
	}

	rec := lifecycle.NewSessionRecord(time.Now())
	rec.SessionID = sessionID
	rec.ServiceRef = req.ServiceRef
	rec.Profile = prof
	rec.CorrelationID = correlationID
	rec.ContextData = map[string]string{
		model.CtxKeyMode: mode,
	}
	_, _ = lifecycle.Dispatch(rec, lifecycle.PhaseFromState(rec.State), lifecycle.Event{Kind: lifecycle.EvStartRequested}, nil, false, time.Now())
	if err := h.Store.PutSession(ctx, rec); err != nil {
		releaseLeases()
		http.Error(w, "failed to persist session", http.StatusInternalServerError)
		return
	}
	if idem != "" {
		_ = h.Store.PutIdempotency(ctx, idem, sessionID, h.IdempotencyTTL)
	}

	// Publish intent event for workers. No blocking.
	// We map the request to a StartSessionEvent.
	if err := h.Bus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
		Type:       model.EventStartSession,
		SessionID:  sessionID,
		ServiceRef: req.ServiceRef,
		// ProfileID is now implicitly universal, but if the struct still has it, we might need to set it or remove it from struct.
		// If StartSessionEvent struct still has ProfileID, we should set it to "universal".
		ProfileID:     "universal",
		CorrelationID: correlationID,
		// Options/Params mapping if needed
	}); err != nil {
		releaseLeases()
		http.Error(w, "failed to dispatch intent", http.StatusInternalServerError)
		return
	}

	h.respond202(ctx, w, sessionID, correlationID)
}

func tryAcquireTunerLease(ctx context.Context, st store.StateStore, owner string, slots []int, ttl time.Duration) (store.Lease, bool, error) {
	for _, s := range slots {
		key := lease.LeaseKeyTunerSlot(s)
		l, got, err := st.TryAcquireLease(ctx, key, owner, ttl)
		if err != nil {
			return nil, false, err
		}
		if got {
			return l, true, nil
		}
	}
	return nil, false, nil
}

func (h IntentHandler) respond202(ctx context.Context, w http.ResponseWriter, sessionID, correlationID string) {
	_ = ctx
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(IntentResponse{
		SessionID:     sessionID,
		Status:        "accepted",
		CorrelationID: correlationID,
	})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
