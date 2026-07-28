// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// CompatibilityHandler captures the subset of v3 handlers that are mounted as
// compatibility/manual routes outside of generated OpenAPI routing.
type CompatibilityHandler interface {
	GetRecordingPlaybackInfo(http.ResponseWriter, *http.Request, string)
	StreamRecordingDirect(http.ResponseWriter, *http.Request, string)
	HandleRecordingResume(http.ResponseWriter, *http.Request)
	HandleRecordingsContinue(http.ResponseWriter, *http.Request)
	PostItemsPlaybackInfo(http.ResponseWriter, *http.Request, string)
}

// RegisterCompatibilityRoutes mounts compatibility routes that still exist
// alongside canonical OpenAPI-generated v3 endpoints.
func RegisterCompatibilityRoutes(rRead, rWrite chi.Router, handler CompatibilityHandler) {
	if handler == nil {
		return
	}

	rRead.Get(V3BaseURL+"/vod/{recordingId}", func(w http.ResponseWriter, r *http.Request) {
		recordingID := chi.URLParam(r, "recordingId")
		handler.GetRecordingPlaybackInfo(w, r, recordingID)
	})

	rRead.Head(V3BaseURL+"/recordings/{recordingId}/stream.mp4", func(w http.ResponseWriter, r *http.Request) {
		recordingID := chi.URLParam(r, "recordingId")
		handler.StreamRecordingDirect(w, r, recordingID)
	})

	rWrite.Put(V3BaseURL+"/recordings/{recordingId}/resume", handler.HandleRecordingResume)

	// Chi prefers static segments over {recordingId}, so /recordings/continue
	// cannot collide with the parameterized recording routes.
	rRead.Get(V3BaseURL+"/recordings/continue", handler.HandleRecordingsContinue)

	// Supports DirectPlay decision logic without backend coupling.
	rRead.Post("/Items/{itemId}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		itemID := chi.URLParam(r, "itemId")
		handler.PostItemsPlaybackInfo(w, r, itemID)
	})
}

// RegisterCompatibilityRoutesWithRegistrars registers the four /api/v3
// compatibility routes exactly once while preserving their read/write stacks.
func RegisterCompatibilityRoutesWithRegistrars(
	readRegistrar, writeRegistrar RouteRegistrar,
	handler CompatibilityHandler,
) error {
	if readRegistrar == nil || writeRegistrar == nil {
		return fmt.Errorf("compatibility route registrar cannot be nil")
	}
	if handler == nil {
		return fmt.Errorf("compatibility route handler cannot be nil")
	}

	if err := readRegistrar.Register(http.MethodGet, V3BaseURL+"/vod/{recordingId}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.GetRecordingPlaybackInfo(w, r, chi.URLParam(r, "recordingId"))
	})); err != nil {
		return err
	}
	if err := readRegistrar.Register(http.MethodHead, V3BaseURL+"/recordings/{recordingId}/stream.mp4", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.StreamRecordingDirect(w, r, chi.URLParam(r, "recordingId"))
	})); err != nil {
		return err
	}
	if err := writeRegistrar.Register(http.MethodPut, V3BaseURL+"/recordings/{recordingId}/resume", http.HandlerFunc(handler.HandleRecordingResume)); err != nil {
		return err
	}
	return readRegistrar.Register(http.MethodGet, V3BaseURL+"/recordings/continue", http.HandlerFunc(handler.HandleRecordingsContinue))
}
