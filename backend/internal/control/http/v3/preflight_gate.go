package v3

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type streamURLProvider interface {
	StreamURL(ctx context.Context, ref, name string) (string, error)
}

func enforcePreflight(ctx context.Context, w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps, serviceRef string) bool {
	provider := deps.preflight
	if provider == nil {
		return false
	}

	sourceRef, err := resolvePreflightSource(ctx, deps, serviceRef)
	if err != nil {
		return rejectPreflight(w, r, preflight.PreflightInternal, err)
	}

	res, err := provider.Check(ctx, sourceRef)
	outcome := res.Outcome
	if err != nil || outcome == "" {
		return rejectPreflight(w, r, preflight.PreflightInternal, err)
	}
	if outcome == preflight.PreflightOK {
		return false
	}
	return rejectPreflight(w, r, outcome, err)
}

func resolvePreflightSource(ctx context.Context, deps sessionsModuleDeps, serviceRef string) (preflight.SourceRef, error) {
	cfg := deps.cfg
	snap := deps.snap

	serviceRef = strings.TrimSpace(serviceRef)
	if serviceRef == "" {
		return preflight.SourceRef{}, fmt.Errorf("serviceRef empty")
	}

	if u, ok := platformnet.ParseDirectHTTPURL(serviceRef); ok {
		src, err := buildPreflightSourceRef(u.String())
		if err != nil {
			return preflight.SourceRef{}, err
		}
		if err := validatePreflightOutboundURL(ctx, src.URL, outboundPolicyFromConfig(cfg)); err != nil {
			return preflight.SourceRef{}, err
		}
		return src, nil
	}

	if deps.receiver == nil {
		return preflight.SourceRef{}, fmt.Errorf("receiver control factory unavailable")
	}
	receiver := deps.receiver(cfg, snap)
	streamer, ok := receiver.(streamURLProvider)
	if !ok {
		return preflight.SourceRef{}, fmt.Errorf("stream url provider unavailable")
	}
	rawURL, err := streamer.StreamURL(ctx, serviceRef, "")
	if err != nil {
		return preflight.SourceRef{}, err
	}
	src, err := buildPreflightSourceRef(rawURL)
	if err != nil {
		return preflight.SourceRef{}, err
	}
	if src.Username == "" && strings.TrimSpace(cfg.Enigma2.Username) != "" {
		src.Username = cfg.Enigma2.Username
		src.Password = cfg.Enigma2.Password
	}
	// Defense in depth: validate receiver-derived URLs against outbound policy.
	// Even though Enigma2.BaseURL is admin-controlled, we apply the same SSRF
	// protection (scheme/host/port allowlist + DNS rebinding block) for consistency.
	if err := validatePreflightOutboundURL(ctx, src.URL, outboundPolicyFromConfig(cfg)); err != nil {
		return preflight.SourceRef{}, err
	}
	return src, nil
}

func buildPreflightSourceRef(rawURL string) (preflight.SourceRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return preflight.SourceRef{}, err
	}

	src := preflight.SourceRef{}
	if parsed.User != nil {
		src.Username = parsed.User.Username()
		src.Password, _ = parsed.User.Password()
	}

	sanitizedURL := *parsed
	sanitizedURL.User = nil
	src.URL = sanitizedURL.String()
	return src, nil
}

func validatePreflightOutboundURL(ctx context.Context, rawURL string, policy platformnet.OutboundPolicy) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}

	validateURL := *parsed
	validateURL.User = nil

	_, err = platformnet.ValidateOutboundURL(ctx, validateURL.String(), policy)
	return err
}

func rejectPreflight(w http.ResponseWriter, r *http.Request, outcome preflight.PreflightOutcome, err error) bool {
	status, _, _ := preflight.MapPreflightOutcome(outcome)
	detail := preflightDetail(outcome)
	spec := resolvePreflightProblem(outcome)

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

	writeRegisteredProblem(w, r, status, spec.ProblemType, spec.Title, spec.Code, detail, nil)
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

func resolvePreflightProblem(outcome preflight.PreflightOutcome) problemcode.Resolved {
	switch outcome {
	case preflight.PreflightUnreachable:
		return problemcode.MustResolve(problemcode.CodePreflightUnreachable, "")
	case preflight.PreflightTimeout:
		return problemcode.MustResolve(problemcode.CodePreflightTimeout, "")
	case preflight.PreflightUnauthorized:
		return problemcode.MustResolve(problemcode.CodePreflightUnauthorized, "")
	case preflight.PreflightForbidden:
		return problemcode.MustResolve(problemcode.CodePreflightForbidden, "")
	case preflight.PreflightNotFound:
		return problemcode.MustResolve(problemcode.CodePreflightNotFound, "")
	case preflight.PreflightBadGateway:
		return problemcode.MustResolve(problemcode.CodePreflightBadGateway, "")
	default:
		return problemcode.MustResolve(problemcode.CodePreflightInternal, "")
	}
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
