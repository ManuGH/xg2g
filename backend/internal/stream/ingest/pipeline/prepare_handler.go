// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// HTTP surface for zap preparation.
//
// Four operations, all keyed by identifiers rather than by anything about the
// channel: start a preparation, ask what became of it, take it, abandon it. No
// service name, provider or bouquet position appears anywhere in here — a service
// reference identifies what to tune, and nothing about it selects behaviour.
//
// Ownership is explicit. A preparation belongs to the client that started it, and
// only that client may commit or cancel it; a client cannot reach into another's
// channel change even by guessing an identifier.

const (
	// clientIDHeader identifies which client a preparation belongs to. One
	// preparation per client is enforced against this value.
	clientIDHeader = "X-Xg2g-Client-Id"
	// maxClientIDLength caps what is accepted, like the zap identifier.
	maxClientIDLength = 64
)

// PrepareHandler serves the preparation endpoints.
type PrepareHandler struct {
	preparations *PreparationManager
	receiverHost string
	streamPort   int
}

// NewPrepareHandler creates the handler.
func NewPrepareHandler(preparations *PreparationManager, receiverHost string, streamPort int) *PrepareHandler {
	if receiverHost == "" {
		receiverHost = "10.10.55.64"
	}
	if streamPort <= 0 {
		streamPort = 8001
	}
	return &PrepareHandler{
		preparations: preparations,
		receiverHost: receiverHost,
		streamPort:   streamPort,
	}
}

// prepareResponse is what every endpoint answers with, so a client parses one shape
// whatever it asked for.
type prepareResponse struct {
	PreparationID string `json:"preparationId"`
	ZapID         string `json:"zapId,omitempty"`
	ServiceRef    string `json:"serviceRef,omitempty"`
	State         string `json:"state"`
	Outcome       string `json:"outcome,omitempty"`
	// Generation identifies the stream proven ready. A commit must quote it back.
	Generation uint64 `json:"generation,omitempty"`
	// ReadyAfterMs is how long transport readiness took.
	ReadyAfterMs int64 `json:"readyAfterMs,omitempty"`
	// Pending names the criteria still outstanding and why, so a failure is
	// diagnosable from the response alone.
	Pending map[string]string `json:"pending,omitempty"`
	Detail  string            `json:"detail,omitempty"`
}

func toResponse(st PreparationStatus) prepareResponse {
	resp := prepareResponse{
		PreparationID: st.ID,
		ZapID:         st.ZapID,
		ServiceRef:    st.ServiceRef,
		State:         string(st.State),
		Outcome:       string(st.Outcome),
		Generation:    st.Generation,
		ReadyAfterMs:  st.ReadyAfter.Milliseconds(),
		Detail:        st.Detail,
	}
	if len(st.Pending) > 0 {
		resp.Pending = make(map[string]string, len(st.Pending))
		for c, reason := range st.Pending {
			resp.Pending[string(c)] = reason
		}
	}
	return resp
}

// ServeHTTP routes the preparation endpoints.
//
//	POST   /api/v3/stream/prepare              start a preparation
//	GET    /api/v3/stream/prepare/{id}         what became of it
//	POST   /api/v3/stream/prepare/{id}/commit  take it
//	DELETE /api/v3/stream/prepare/{id}         abandon it
func (h *PrepareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/v3/stream/prepare"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	rest = strings.Trim(rest, "/")

	clientID := sanitizeIdentifier(r.Header.Get(clientIDHeader), maxClientIDLength)
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "missing "+clientIDHeader)
		return
	}

	switch {
	case rest == "" && r.Method == http.MethodPost:
		h.start(w, r, clientID)
	case rest == "":
		writeError(w, http.StatusMethodNotAllowed, "use POST to start a preparation")
	default:
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "commit" && r.Method == http.MethodPost:
			h.commit(w, r, clientID, id)
		case action == "" && r.Method == http.MethodGet:
			h.status(w, clientID, id)
		case action == "" && r.Method == http.MethodDelete:
			h.cancel(w, clientID, id)
		default:
			writeError(w, http.StatusNotFound, "unknown preparation endpoint")
		}
	}
}

func (h *PrepareHandler) start(w http.ResponseWriter, r *http.Request, clientID string) {
	// Query().Get has already percent-decoded this. Decoding it a second time would
	// corrupt any reference that legitimately contains an escaped percent sign, and
	// a service reference is data - it does not get to be decoded twice because the
	// live route, which reads it out of the path, has to decode it once.
	serviceRef := r.URL.Query().Get("sref")
	if serviceRef == "" {
		writeError(w, http.StatusBadRequest, "missing sref")
		return
	}

	key := session.NewSessionKey(h.receiverHost, h.streamPort, serviceRef)
	key.TargetProgram = targetProgramFromServiceRef(serviceRef)
	if err := key.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid sref: %v", err))
		return
	}

	zapID := sanitizeZapID(r.Header.Get(zapIDHeader))
	prep, err := h.preparations.Prepare(PrepareRequest{
		ClientID: clientID,
		ZapID:    zapID,
		Key:      key,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.L().Info().
		Str("event", "zap.prepare.started").
		Str("preparation_id", prep.ID()).
		Str("zap_id", zapID).
		Str("serviceRef", key.ServiceRef).
		Msg("preparation started")

	// 202: accepted and running. Readiness is not a property of this response, which
	// is the whole point — an HTTP status has never said anything about a broadcast.
	writeJSON(w, http.StatusAccepted, toResponse(prep.Status()))
}

func (h *PrepareHandler) status(w http.ResponseWriter, clientID, id string) {
	st, ok := h.resolve(w, clientID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toResponse(st))
}

// resolve looks a preparation up and checks that it belongs to this client.
//
// Unknown is 404 and someone else's is 403, the same way for every verb. The three
// endpoints used to disagree - reading an unknown identifier answered 404 while
// committing or cancelling one answered 403 - so whether an identifier existed
// depended on which verb you asked with.
func (h *PrepareHandler) resolve(w http.ResponseWriter, clientID, id string) (PreparationStatus, bool) {
	st, err := h.preparations.Status(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return PreparationStatus{}, false
	}
	if !h.owns(clientID, id) {
		writeError(w, http.StatusForbidden, "preparation belongs to another client")
		return PreparationStatus{}, false
	}
	return st, true
}

func (h *PrepareHandler) commit(w http.ResponseWriter, r *http.Request, clientID, id string) {
	if _, ok := h.resolve(w, clientID, id); !ok {
		return
	}

	generation, err := strconv.ParseUint(r.URL.Query().Get("generation"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "commit requires the generation that was observed ready")
		return
	}

	st, err := h.preparations.Commit(id, generation)
	switch {
	case err == nil:
		log.L().Info().
			Str("event", "zap.commit").
			Str("preparation_id", id).
			Uint64("generation", generation).
			Msg("client committed to the prepared stream")
		writeJSON(w, http.StatusOK, toResponse(st))
	case errors.Is(err, ErrNoSuchPreparation):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrGenerationChanged):
		// The stream this preparation proved is gone. 409 rather than 400: the
		// request was well formed, the world moved.
		writeJSONWithError(w, http.StatusConflict, toResponse(st), err.Error())
	case errors.Is(err, ErrPreparationNotReady):
		// Not an error the client made — it asked too early, or the preparation
		// failed. The body says which.
		writeJSONWithError(w, http.StatusPreconditionFailed, toResponse(st), err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *PrepareHandler) cancel(w http.ResponseWriter, clientID, id string) {
	if _, ok := h.resolve(w, clientID, id); !ok {
		return
	}
	// Idempotent: cancelling something already finished is success, not an error. A
	// client cleaning up must never have to care whether it won the race.
	h.preparations.Cancel(id, "cancelled by client")
	st, err := h.preparations.Status(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toResponse(st))
}

// owns reports whether the preparation was started by this client.
//
// Checked against the recorded owner rather than against the client's current
// preparation, so a superseded one can still be inspected and cancelled by the
// client that started it.
func (h *PrepareHandler) owns(clientID, id string) bool {
	owner, ok := h.preparations.Owner(id)
	return ok && owner == clientID
}

// targetProgramFromServiceRef reads the programme number an Enigma2 reference
// carries in its fourth field. Parsing the reference is not channel-specific logic:
// the same rule applies to every service on every receiver.
func targetProgramFromServiceRef(serviceRef string) uint16 {
	parts := strings.Split(serviceRef, ":")
	if len(parts) < 4 {
		return 0
	}
	val, err := strconv.ParseUint(parts[3], 16, 16)
	if err != nil {
		return 0
	}
	return uint16(val)
}

// sanitizeIdentifier keeps a client-supplied identifier to characters that are safe
// to put in a log line, and bounds its length.
func sanitizeIdentifier(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) > maxLen {
		raw = raw[:maxLen]
	}
	cleaned := make([]rune, 0, len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			cleaned = append(cleaned, r)
		}
	}
	return string(cleaned)
}

func writeJSON(w http.ResponseWriter, status int, body prepareResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONWithError(w http.ResponseWriter, status int, body prepareResponse, detail string) {
	if body.Detail == "" {
		body.Detail = detail
	}
	writeJSON(w, status, body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
