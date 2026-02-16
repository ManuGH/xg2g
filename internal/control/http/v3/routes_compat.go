// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// CompatibilityHandler captures the subset of v3 handlers that are mounted as
// compatibility/manual routes outside of generated OpenAPI routing.
type CompatibilityHandler interface {
	GetRecordingPlaybackInfo(http.ResponseWriter, *http.Request, string)
	StreamRecordingDirect(http.ResponseWriter, *http.Request, string)
	HandleRecordingResume(http.ResponseWriter, *http.Request)
	HandleRecordingResumeOptions(http.ResponseWriter, *http.Request)
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
	rWrite.Options(V3BaseURL+"/recordings/{recordingId}/resume", handler.HandleRecordingResumeOptions)

	// Supports DirectPlay decision logic without backend coupling.
	rRead.Post("/Items/{itemId}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		itemID := chi.URLParam(r, "itemId")
		handler.PostItemsPlaybackInfo(w, r, itemID)
	})
}
