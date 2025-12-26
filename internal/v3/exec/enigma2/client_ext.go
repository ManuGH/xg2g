package enigma2

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) ResolveStreamURL(ctx context.Context, sref string) (string, error) {
	// Request the M3U playlist from the receiver to let it decide the correct stream URL (port, transcoding, etc).
	// Endpoint: /web/stream.m3u?ref=... (Using "ref" ensures full URL is returned on some OWI versions)
	params := url.Values{}
	params.Set("ref", strings.ToUpper(sref))

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = "/web/stream.m3u"
	u.RawQuery = params.Encode()

	resp, err := c.doGet(ctx, "resolve_stream", u.String())
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
		if strings.HasPrefix(line, "http") {
			// CRITICAL FIX: Some receivers (or specific port configurations like 17999) return just the root URL
			// (e.g. http://ip:17999/) without the sRef path.
			// If the URL ends in slash and appears to be a root, we append the sRef to form a valid request.
			if strings.HasSuffix(line, "/") {
				// Naive check: does it look like it's missing the sRef?
				// If the sRef isn't in the URL, append it.
				if !strings.Contains(line, sref) {
					return line + strings.ToUpper(sref), nil
				}
			}
			return line, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("no stream url found in playlist")
}
