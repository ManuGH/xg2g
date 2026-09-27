package v3

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const (
	maxSessionPlaybackTraceBodyBytes = 16 * 1024
	maxSessionPlaybackTraceEvents    = 128
)

var sessionPlaybackTraceEventStages = map[ClientPlaybackTraceEventEvent]ClientPlaybackTraceEventStage{
	ClientTracePlaybackStarted:      ClientTraceStageLifecycle,
	ClientTraceRequestStarted:       ClientTraceStageNetwork,
	ClientTraceHttpResponse:         ClientTraceStageNetwork,
	ClientTraceFirstByte:            ClientTraceStageNetwork,
	ClientTraceTransportGap:         ClientTraceStageNetwork,
	ClientTraceStreamClosed:         ClientTraceStageNetwork,
	ClientTracePsiReady:             ClientTraceStageDemux,
	ClientTraceVideoParametersReady: ClientTraceStageDecode,
	ClientTraceFirstIdr:             ClientTraceStageDecode,
	ClientTraceFirstDecodedFrame:    ClientTraceStageDecode,
	ClientTraceFirstPictureRendered: ClientTraceStageRender,
	ClientTraceFirstPictureVisible:  ClientTraceStageRender,
	ClientTraceContinuityError:      ClientTraceStageTransport,
	ClientTracePtsDiscontinuity:     ClientTraceStageTransport,
	ClientTracePesError:             ClientTraceStageTransport,
	ClientTraceDecodeError:          ClientTraceStageDecode,
	ClientTraceDecoderRecovery:      ClientTraceStageDecode,
	ClientTraceAudioUnderrun:        ClientTraceStageAudio,
	ClientTraceAudioClockStarted:    ClientTraceStageAudio,
	ClientTraceAudioClockStopped:    ClientTraceStageAudio,
	ClientTraceFrameDrop:            ClientTraceStageRender,
	ClientTraceFrameLate:            ClientTraceStageRender,
}

// PostSessionPlaybackTrace accepts an anomaly-triggered event window from a
// native session player. The trace is logged, not persisted or turned into a
// metric label; Android's session path does not use stream preparations.
func (s *Server) PostSessionPlaybackTrace(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSessionPlaybackTraceBodyBytes))
	if err != nil {
		RespondError(w, r, http.StatusRequestEntityTooLarge, ErrInvalidInput, "trace payload exceeds 16 KiB")
		return
	}
	var batch ClientPlaybackTraceBatch
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid playback trace batch")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid playback trace batch")
		return
	}
	if err := validateSessionPlaybackTrace(batch, time.Now()); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "playback trace events are invalid or out of order")
		return
	}

	deps := s.sessionsModuleDeps()
	if deps.store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "session store is not initialized")
		return
	}
	session, err := deps.store.GetSession(r.Context(), sessionID.String())
	if err != nil || session == nil {
		RespondError(w, r, http.StatusNotFound, ErrSessionFeedbackNotFound)
		return
	}

	logger := log.L().Info()
	if sessionPlaybackTraceHasAnomaly(batch.Events) {
		logger = log.L().Warn()
	}
	logger.
		Str("event", "client.playback.trace").
		Str("session_id", sessionID.String()).
		Str("client_family", session.ContextData[model.CtxKeyClientFamily]).
		Time("observed_at", batch.ObservedAt).
		Interface("timeline", batch.Events).
		Msg("client playback trace window")
	w.WriteHeader(http.StatusNoContent)
}

func validateSessionPlaybackTrace(batch ClientPlaybackTraceBatch, now time.Time) error {
	if len(batch.Events) == 0 || len(batch.Events) > maxSessionPlaybackTraceEvents || batch.ObservedAt.IsZero() ||
		now.Sub(batch.ObservedAt) > 2*time.Minute || batch.ObservedAt.Sub(now) > 30*time.Second {
		return errInvalidSessionPlaybackTrace
	}
	var previousSequence, previousElapsed int64
	for index, event := range batch.Events {
		expectedStage, knownEvent := sessionPlaybackTraceEventStages[event.Event]
		if event.Sequence < 1 || event.Sequence > 1_000_000_000 || event.ElapsedMs < 0 || event.ElapsedMs > int64((7*24*time.Hour)/time.Millisecond) ||
			(index > 0 && (event.Sequence <= previousSequence || event.ElapsedMs < previousElapsed)) ||
			batch.Events[len(batch.Events)-1].ElapsedMs-event.ElapsedMs > 30_000 ||
			!knownEvent || expectedStage != event.Stage ||
			(event.ValueMs != nil && (*event.ValueMs < 0 || *event.ValueMs > 300_000 || math.IsNaN(float64(*event.ValueMs)) || math.IsInf(float64(*event.ValueMs), 0))) {
			return errInvalidSessionPlaybackTrace
		}
		previousSequence, previousElapsed = event.Sequence, event.ElapsedMs
	}
	return nil
}

func sessionPlaybackTraceHasAnomaly(events []ClientPlaybackTraceEvent) bool {
	for _, event := range events {
		switch event.Event {
		case ClientTraceContinuityError, ClientTracePtsDiscontinuity, ClientTracePesError, ClientTraceDecodeError,
			ClientTraceDecoderRecovery, ClientTraceAudioUnderrun, ClientTraceFrameDrop, ClientTraceFrameLate,
			ClientTraceTransportGap:
			return true
		}
	}
	return false
}

var errInvalidSessionPlaybackTrace = &sessionPlaybackTraceValidationError{}

type sessionPlaybackTraceValidationError struct{}

func (*sessionPlaybackTraceValidationError) Error() string { return "invalid session playback trace" }
