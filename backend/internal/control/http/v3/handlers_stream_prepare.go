// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/problemcode"
)

// StartStreamPrepare implements ServerInterface (POST /stream/prepare).
func (s *Server) StartStreamPrepare(w http.ResponseWriter, r *http.Request, params StartStreamPrepareParams) {
	// The zap preparation endpoint is mounted and served through the ingest pipeline router in internal/api.
	writeRegisteredProblem(w, r, http.StatusNotImplemented, "stream/prepare_not_mounted", "Stream Prepare Not Mounted", problemcode.CodeUnavailable, "Stream preparation is served through the primary ingest router", nil)
}

// CancelStreamPrepare implements ServerInterface (DELETE /stream/prepare/{preparationId}).
func (s *Server) CancelStreamPrepare(w http.ResponseWriter, r *http.Request, preparationId string) {
	writeRegisteredProblem(w, r, http.StatusNotImplemented, "stream/prepare_not_mounted", "Stream Prepare Not Mounted", problemcode.CodeUnavailable, "Stream preparation is served through the primary ingest router", nil)
}

// GetStreamPrepareStatus implements ServerInterface (GET /stream/prepare/{preparationId}).
func (s *Server) GetStreamPrepareStatus(w http.ResponseWriter, r *http.Request, preparationId string) {
	writeRegisteredProblem(w, r, http.StatusNotImplemented, "stream/prepare_not_mounted", "Stream Prepare Not Mounted", problemcode.CodeUnavailable, "Stream preparation is served through the primary ingest router", nil)
}

// CommitStreamPrepare implements ServerInterface (POST /stream/prepare/{preparationId}/commit).
func (s *Server) CommitStreamPrepare(w http.ResponseWriter, r *http.Request, preparationId string, params CommitStreamPrepareParams) {
	writeRegisteredProblem(w, r, http.StatusNotImplemented, "stream/prepare_not_mounted", "Stream Prepare Not Mounted", problemcode.CodeUnavailable, "Stream preparation is served through the primary ingest router", nil)
}
