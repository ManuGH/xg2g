package enigma2

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/rs/zerolog/log"
)

func normalizeStreamURL(rawURL, sref string) string {
	if rawURL == "" || sref == "" {
		return rawURL
	}
	upperRef := strings.ToUpper(sref)
	if strings.Contains(strings.ToUpper(rawURL), upperRef) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/" + upperRef
		return u.String()
	}
	if strings.HasSuffix(rawURL, "/") {
		return rawURL + upperRef
	}
	return rawURL
}

func resolveStreamLine(baseURL, line, sref string) (string, bool) {
	if line == "" {
		return "", false
	}
	if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		return normalizeStreamURL(line, sref), true
	}
	if strings.HasPrefix(line, "/") {
		base, err := url.Parse(baseURL)
		if err != nil {
			return "", false
		}
		rel, err := url.Parse(line)
		if err != nil {
			return "", false
		}
		return normalizeStreamURL(base.ResolveReference(rel).String(), sref), true
	}
	return "", false
}

func (c *Client) buildDirectStreamURL(sref string) (string, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	u.Host = fmt.Sprintf("%s:%d", u.Hostname(), c.StreamPort)
	u.Path = "/" + strings.ToUpper(sref)
	if c.Username != "" {
		u.User = url.UserPassword(c.Username, c.Password)
	}

	return u.String(), nil
}

func (c *Client) resolveViaWebStream(ctx context.Context, sref string) (string, error) {
	params := url.Values{}
	params.Set("ref", strings.ToUpper(sref))
	params.Set("name", "Stream")
	params.Set("device", "etc")
	params.Set("fname", "Stream")

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = "/web/stream.m3u"
	u.RawQuery = params.Encode()

	resp, err := c.doGet(ctx, u.String())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: stream api returned status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if resolved, ok := resolveStreamLine(c.BaseURL, line, sref); ok {
			if c.Username != "" {
				if ru, err := url.Parse(resolved); err == nil && strings.EqualFold(ru.Hostname(), u.Hostname()) {
					ru.User = url.UserPassword(c.Username, c.Password)
					resolved = ru.String()
				}
			}
			return resolved, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("no stream url found in playlist")
}

func (c *Client) ResolveStreamURL(ctx context.Context, sref string) (string, error) {
	// Bypass if sRef is already a direct URL (HTTP/HTTPS) as used by recordings
	if _, ok := net.ParseDirectHTTPURL(sref); ok {
		return sref, nil
	}

	// Debug logging to verify streamPort configuration
	log.Info().
		Int("streamPort", c.StreamPort).
		Str("baseURL", net.SanitizeURL(c.BaseURL)).
		Msg("ResolveStreamURL called")

	// If explicitly configured to use WebIF streams, always use /web/stream.m3u.
	// This lets the receiver decide the correct stream URL (and often fixes metadata/SPS/PPS issues).
	if c.UseWebIFStreams {
		log.Info().Msg("useWebIFStreams enabled, using /web/stream.m3u")
		resolved, err := c.resolveViaWebStream(ctx, sref)
		if err != nil {
			return "", err
		}
		log.Info().
			Str("resolved_url", net.SanitizeURL(resolved)).
			Str("sref", sref).
			Msg("Stream URL resolved")
		return resolved, nil
	}

	// If streamPort is configured, still ask OpenWebIF first so the receiver can
	// perform its normal stream-resolution side effects before we fall back to a
	// locally constructed direct URL.
	if c.StreamPort > 0 {
		resolved, err := c.resolveViaWebStream(ctx, sref)
		if err == nil {
			log.Info().
				Str("resolved_url", net.SanitizeURL(resolved)).
				Str("sref", sref).
				Msg("Stream URL resolved via OpenWebIF")
			return resolved, nil
		}

		directURL, directErr := c.buildDirectStreamURL(sref)
		if directErr != nil {
			return "", directErr
		}

		log.Warn().
			Err(err).
			Str("direct_url", net.SanitizeURL(directURL)).
			Msg("OpenWebIF stream resolution failed, falling back to direct stream URL")
		return directURL, nil
	}

	log.Info().Msg("streamPort not configured, using /web/stream.m3u")

	resolved, err := c.resolveViaWebStream(ctx, sref)
	if err != nil {
		return "", err
	}
	log.Info().
		Str("resolved_url", net.SanitizeURL(resolved)).
		Str("sref", sref).
		Msg("Stream URL resolved")
	return resolved, nil
}
