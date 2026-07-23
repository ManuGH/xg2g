package v3

import (
	"context"
	"net/http"

	v3hls "github.com/ManuGH/xg2g/internal/control/http/v3/hls"
	"github.com/ManuGH/xg2g/internal/log"
)

type hlsSLOAdapter struct {
	server *Server
}

func (a *hlsSLOAdapter) MarkOutcome(sessionID, schema, mode, outcome string) {
	if a == nil || a.server == nil || a.server.playbackSLO == nil {
		return
	}
	res := a.server.playbackSLO.MarkOutcome(playbackSessionMeta{
		SessionID: sessionID,
		Schema:    schema,
		Mode:      mode,
	}, outcome)
	if res.TTFFObserved {
		log.L().Info().
			Str("event", "playback.slo.ttff").
			Str("session_id", sessionID).
			Str("schema", res.Schema).
			Str("mode", res.Mode).
			Str("outcome", res.Outcome).
			Float64("ttff_seconds", res.TTFFSeconds).
			Msg("live playback ttff outcome observed")
	}
}

func (a *hlsSLOAdapter) MarkMediaSuccess(sessionID, schema, mode string) {
	if a == nil || a.server == nil || a.server.playbackSLO == nil {
		return
	}
	obs := a.server.playbackSLO.MarkMediaSuccess(playbackSessionMeta{
		SessionID: sessionID,
		Schema:    schema,
		Mode:      mode,
	})
	if obs.TTFFObserved {
		log.L().Info().
			Str("event", "playback.slo.ttff").
			Str("session_id", sessionID).
			Str("schema", obs.Schema).
			Str("mode", obs.Mode).
			Str("outcome", "ok").
			Float64("ttff_seconds", obs.TTFFSeconds).
			Msg("live playback ttff observed")
	}
	if obs.RebufferSeverity != "" {
		log.L().Warn().
			Str("event", "playback.slo.rebuffer").
			Str("session_id", sessionID).
			Str("schema", obs.Schema).
			Str("mode", obs.Mode).
			Str("severity", obs.RebufferSeverity).
			Msg("live playback rebuffer proxy event observed")
	}
}

func (s *Server) hlsProcessor() *v3hls.Service {
	deps := s.sessionsModuleDeps()
	return v3hls.NewService(
		deps.cfg,
		deps.store,
		deps.storeRegistry,
		func(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
			writeRegisteredProblem(w, r, status, problemType, title, code, detail, extra)
		},
		func(w http.ResponseWriter) (http.ResponseWriter, any) {
			return wrapResponseWriter(w)
		},
		func(w http.ResponseWriter, r *http.Request, root string, segmentSeconds int, ffmpegBin, sessionID string) {
			s.serveLivePreviewFrame(w, r, root, segmentSeconds, ffmpegBin, sessionID)
		},
		func(ctx context.Context, sessionID string) {
			s.renewLeaseFromConsumption(ctx, sessionID)
		},
		&hlsSLOAdapter{server: s},
		func(filename string) string {
			return playbackStageLabelFromLiveFilename(filename)
		},
		func(status int) string {
			return playbackErrorCodeFromStatus(status)
		},
		func(ctx context.Context) string {
			return requestID(ctx)
		},
	)
}
