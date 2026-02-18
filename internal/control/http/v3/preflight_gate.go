package v3

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type streamURLProvider interface {
	StreamURL(ctx context.Context, ref, name string) (string, error)
}

func enforcePreflight(ctx context.Context, w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps, serviceRef string) bool {
	provider := deps.preflight
	if provider == nil {
		return false
	}

	sourceURL, err := resolvePreflightSourceURL(ctx, deps, serviceRef)
	if err != nil {
		return rejectPreflight(w, r, preflight.PreflightInternal, err)
	}

	res, err := provider.Check(ctx, preflight.SourceRef{URL: sourceURL})
	outcome := res.Outcome
	if err != nil || outcome == "" {
		return rejectPreflight(w, r, preflight.PreflightInternal, err)
	}
	if outcome == preflight.PreflightOK {
		return false
	}
	return rejectPreflight(w, r, outcome, err)
}

func resolvePreflightSourceURL(ctx context.Context, deps sessionsModuleDeps, serviceRef string) (string, error) {
	cfg := deps.cfg
	snap := deps.snap

	serviceRef = strings.TrimSpace(serviceRef)
	if serviceRef == "" {
		return "", fmt.Errorf("serviceRef empty")
	}

	if u, ok := platformnet.ParseDirectHTTPURL(serviceRef); ok {
		normalized, err := platformnet.ValidateOutboundURL(ctx, u.String(), outboundPolicyFromConfig(cfg))
		if err != nil {
			return "", err
		}
		return normalized, nil
	}

	if deps.receiver == nil {
		return "", fmt.Errorf("receiver control factory unavailable")
	}
	receiver := deps.receiver(cfg, snap)
	streamer, ok := receiver.(streamURLProvider)
	if !ok {
		return "", fmt.Errorf("stream url provider unavailable")
	}
	rawURL, err := streamer.StreamURL(ctx, serviceRef, "")
	if err != nil {
		return "", err
	}
	// Defense in depth: validate receiver-derived URLs against outbound policy.
	// Even though Enigma2.BaseURL is admin-controlled, we apply the same SSRF
	// protection (scheme/host/port allowlist + DNS rebinding block) for consistency.
	return platformnet.ValidateOutboundURL(ctx, rawURL, outboundPolicyFromConfig(cfg))
}

func rejectPreflight(w http.ResponseWriter, r *http.Request, outcome preflight.PreflightOutcome, err error) bool {
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
