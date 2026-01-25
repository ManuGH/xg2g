package v3

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type streamURLProvider interface {
	StreamURL(ctx context.Context, ref, name string) (string, error)
}

func (s *Server) enforcePreflight(ctx context.Context, w http.ResponseWriter, r *http.Request, cfg config.AppConfig, serviceRef string) bool {
	s.mu.RLock()
	provider := s.preflightProvider
	snap := s.snap
	s.mu.RUnlock()

	if provider == nil {
		return false
	}

	sourceURL, err := s.resolvePreflightSourceURL(ctx, cfg, snap, serviceRef)
	if err != nil {
		return s.rejectPreflight(w, r, preflight.PreflightInternal, err)
	}

	res, err := provider.Check(ctx, preflight.SourceRef{URL: sourceURL})
	outcome := res.Outcome
	if err != nil || outcome == "" {
		return s.rejectPreflight(w, r, preflight.PreflightInternal, err)
	}
	if outcome == preflight.PreflightOK {
		return false
	}
	return s.rejectPreflight(w, r, outcome, err)
}

func (s *Server) resolvePreflightSourceURL(ctx context.Context, cfg config.AppConfig, snap config.Snapshot, serviceRef string) (string, error) {
	serviceRef = strings.TrimSpace(serviceRef)
	if serviceRef == "" {
		return "", fmt.Errorf("serviceRef empty")
	}

	if u, ok := platformnet.ParseDirectHTTPURL(serviceRef); ok {
		return u.String(), nil
	}

	receiver := s.owi(cfg, snap)
	streamer, ok := receiver.(streamURLProvider)
	if !ok {
		return "", fmt.Errorf("stream url provider unavailable")
	}
	return streamer.StreamURL(ctx, serviceRef, "")
}

func (s *Server) rejectPreflight(w http.ResponseWriter, r *http.Request, outcome preflight.PreflightOutcome, err error) bool {
	status, problemType, title := preflight.MapPreflightOutcome(outcome)
	detail := preflightDetail(outcome)
	code := preflightProblemCode(outcome)

	metrics.IncVODPreflightFail(string(outcome))

	logger := log.WithComponentFromContext(r.Context(), "api")
	reqID := log.RequestIDFromContext(r.Context())
	event := logger.With().
		Str("request_id", reqID).
		Str("outcome", string(outcome)).
		Int("status", status).
		Logger()

	if isPreflightExpectedFailure(outcome) {
		event.Warn().Msg("preflight rejected")
	} else {
		if err != nil {
			event.Error().Err(err).Msg("preflight failed")
		} else {
			event.Error().Msg("preflight failed")
		}
	}

	writeProblem(w, r, status, problemType, title, code, detail, nil)
	return true
}

func isPreflightExpectedFailure(outcome preflight.PreflightOutcome) bool {
	switch outcome {
	case preflight.PreflightUnreachable,
		preflight.PreflightTimeout,
		preflight.PreflightUnauthorized,
		preflight.PreflightForbidden,
		preflight.PreflightNotFound,
		preflight.PreflightBadGateway:
		return true
	default:
		return false
	}
}

func preflightProblemCode(outcome preflight.PreflightOutcome) string {
	code := strings.ToUpper(string(outcome))
	if code == "" {
		code = "INTERNAL"
	}
	return "PREFLIGHT_" + code
}

func preflightDetail(outcome preflight.PreflightOutcome) string {
	switch outcome {
	case preflight.PreflightUnreachable:
		return "Cannot reach source"
	case preflight.PreflightTimeout:
		return "Preflight exceeded timeout"
	case preflight.PreflightUnauthorized:
		return "Source requires authentication"
	case preflight.PreflightForbidden:
		return "Insufficient permissions to access source"
	case preflight.PreflightNotFound:
		return "Source endpoint not found"
	case preflight.PreflightBadGateway:
		return "Invalid upstream response"
	case preflight.PreflightInternal:
		return "Preflight failed due to an internal error"
	default:
		return "Preflight failed"
	}
}
