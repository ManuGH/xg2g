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

	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
	"github.com/ManuGH/xg2g/internal/v3/store"
)

type IntentHandler struct {
	Store store.StateStore
	Bus   bus.Bus
	// TTL for idempotency key mapping.
	IdempotencyTTL time.Duration
	// DVRWindowSec overrides DVR window for DVR profiles; 0 uses internal default.
	DVRWindowSec int
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
			h.respond202(ctx, w, existing)
			return
		} else if err != nil {
			http.Error(w, "idempotency lookup failed", http.StatusInternalServerError)
			return
		}
	}

	sessionID := newID()
	dvrWindowSec := h.DVRWindowSec
	if dvrWindowSec <= 0 {
		dvrWindowSec = 300
	}
	prof := profiles.Resolve(req.ProfileID, r.UserAgent(), dvrWindowSec)

	rec := &model.SessionRecord{
		SessionID:     sessionID,
		ServiceRef:    req.ServiceRef,
		Profile:       prof,
		State:         model.SessionStarting,
		CreatedAtUnix: time.Now().Unix(),
		UpdatedAtUnix: time.Now().Unix(),
	}
	if err := h.Store.PutSession(ctx, rec); err != nil {
		http.Error(w, "failed to persist session", http.StatusInternalServerError)
		return
	}
	if idem != "" {
		_ = h.Store.PutIdempotency(ctx, idem, sessionID, h.IdempotencyTTL)
	}

	// Publish intent event for workers. No blocking.
	// We map the request to a StartSessionEvent.
	_ = h.Bus.Publish(ctx, string(model.EventStartSession), model.StartSessionEvent{
		Type:       model.EventStartSession,
		SessionID:  sessionID,
		ServiceRef: req.ServiceRef,
		ProfileID:  req.ProfileID,
		// Options/Params mapping if needed
	})

	h.respond202(ctx, w, sessionID)
}

func (h IntentHandler) respond202(ctx context.Context, w http.ResponseWriter, sessionID string) {
	_ = ctx
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(IntentResponse{
		SessionID: sessionID,
		State:     string(model.SessionStarting),
	})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
