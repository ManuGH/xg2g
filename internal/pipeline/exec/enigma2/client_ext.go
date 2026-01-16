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

func (c *Client) ResolveStreamURL(ctx context.Context, sref string) (string, error) {
	// Bypass if sRef is already a direct URL (HTTP/HTTPS) as used by recordings
	if _, ok := net.ParseDirectHTTPURL(sref); ok {
		return sref, nil
	}

	// Debug logging to verify streamPort configuration
	log.Info().Int("streamPort", c.StreamPort).Str("baseURL", c.BaseURL).Msg("ResolveStreamURL called")

	// If explicitly configured to use WebIF streams, always use /web/stream.m3u.
	// This lets the receiver decide the correct stream URL (and often fixes metadata/SPS/PPS issues).
	if c.UseWebIFStreams {
		log.Info().Msg("useWebIFStreams enabled, using /web/stream.m3u")
		goto webStream
	}

	// If streamPort is configured, build direct URL instead of querying /web/stream.m3u
	// This bypasses OSCam-emu relay issues and ensures predictable stream source
	if c.StreamPort > 0 {
		u, err := url.Parse(c.BaseURL)
		if err != nil {
			return "", fmt.Errorf("invalid base URL: %w", err)
		}

		// Build direct stream URL: http://host:port/SREF
		u.Host = fmt.Sprintf("%s:%d", u.Hostname(), c.StreamPort)
		u.Path = "/" + strings.ToUpper(sref)

		directURL := u.String()
		log.Info().Str("direct_url", directURL).Msg("Using direct stream URL (bypassing /web/stream.m3u)")
		return directURL, nil
	}

	log.Info().Msg("streamPort not configured, using /web/stream.m3u")

	// Legacy path: Request the M3U playlist from the receiver to let it decide the correct stream URL (port, transcoding, etc).
	// Endpoint: /web/stream.m3u?ref=... (Using "ref" ensures full URL is returned on some OWI versions)
webStream:
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

	// Parse M3U line by line to find the stream URL
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if resolved, ok := resolveStreamLine(c.BaseURL, line, sref); ok {
			log.Info().Str("resolved_url", resolved).Str("sref", sref).Msg("Stream URL resolved")
			return resolved, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("no stream url found in playlist")
}
