package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	preflightMinBytes          = 188 * 3  // sync floor: never raised, keeps preflight latency/timeout behaviour unchanged
	preflightScanBytes         = 188 * 48 // best-effort upper bound scanned for scrambling (9024B) on a source that is not lock-prone
	preflightTimeout           = 2 * time.Second
	preflightMaxTries          = 3
	preflightScrambleTries     = 5
	preflightDirectWarmupTries = 8
)

var ffmpegURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^'"\s]+`)

// isStreamRelayURL reports whether an input URL points at the relay port.
//
// DEPRECATED: the relay input path is scheduled for removal after
// config.RelayInputRemoveAfter. A receiver that serves the same content on its
// standard streaming port never produces such a URL, so on current setups this
// returns false for every input and the relay-specific branches it guards are
// inert. Do not add new callers; new input handling belongs on the default
// path. Note the port alone was never a reliable marker of a distinct upstream
// mode — a receiver can be configured to hand out the relay port for all
// services, including ones that need no relay at all.
func isStreamRelayURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}
	return port == "17999"
}

func defaultPortForScheme(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func sanitizeURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

func sanitizeFFmpegLogLine(line string) string {
	return ffmpegURLPattern.ReplaceAllStringFunc(line, sanitizeURLForLog)
}

type streamWarmupResult struct {
	bytes      int
	httpStatus int
	latencyMs  int64
}

func buildAuthenticatedRequest(ctx context.Context, method, rawURL string) (*http.Request, *url.URL, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, nil, fmt.Errorf("request url empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}

	reqURL := *parsed
	user := reqURL.User
	reqURL.User = nil

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	if user != nil {
		username := user.Username()
		password, _ := user.Password()
		if username != "" || password != "" {
			req.SetBasicAuth(username, password)
		}
	}

	return req, parsed, nil
}

func isHTTPInputURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func (a *LocalAdapter) warmupInputStream(ctx context.Context, rawURL string, duration time.Duration) (result streamWarmupResult, err error) {
	start := time.Now()
	defer func() {
		result.latencyMs = time.Since(start).Milliseconds()
	}()

	if duration <= 0 {
		return result, nil
	}

	// A tuner source is warmed by shared ingest itself: by the time this runs the
	// spool already holds a primed head. Only an external URL source has an
	// upstream that this could still usefully open, and building a request for
	// anything else would be an HTTP dial where the byte source is not HTTP.
	if !isHTTPInputURL(rawURL) {
		return result, nil
	}

	warmupCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	req, _, err := buildAuthenticatedRequest(warmupCtx, http.MethodGet, rawURL)
	if err != nil {
		return result, err
	}

	client := a.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: duration,
			Transport: &http.Transport{
				ResponseHeaderTimeout: duration,
			},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		if warmupCtx.Err() != nil {
			return result, nil
		}
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.httpStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("warmup http status %d", resp.StatusCode)
	}

	buf := make([]byte, preflightMinBytes)
	n, readErr := resp.Body.Read(buf)
	result.bytes = n
	if readErr != nil && !errors.Is(readErr, io.EOF) && warmupCtx.Err() == nil {
		return result, readErr
	}
	return result, nil
}
