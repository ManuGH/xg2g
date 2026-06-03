package openwebif

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// StreamURL builds a streaming URL for the given service reference.
// Context is used for smart stream detection and tracing.
func (c *Client) StreamURL(ctx context.Context, ref, name string) (string, error) {
	_ = ctx

	parsed, err := parseOpenWebIFBaseURL(c.base)
	if err != nil {
		return "", err
	}

	if c.useWebIFStreams {
		return buildWebIFStreamURL(parsed, ref, name, c.username, c.password)
	}

	if u, ok := buildDirectStreamOverrideURL(c.streamBaseURL, ref); ok {
		return u, nil
	}

	return buildDirectTSStreamURL(parsed, ref, c.port)
}

func parseOpenWebIFBaseURL(rawBase string) (*url.URL, error) {
	base := strings.TrimSpace(rawBase)
	if base == "" {
		return nil, fmt.Errorf("openwebif base URL is empty")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse openwebif base URL %q: %w", base, err)
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("openwebif base URL %q missing host", base)
	}
	return parsed, nil
}

func buildWebIFStreamURL(parsed *url.URL, ref, name, username, password string) (string, error) {
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("openwebif base URL %q missing hostname", parsed.String())
	}

	q := url.Values{}
	q.Set("ref", ref)
	q.Set("name", name)

	u := &url.URL{
		Scheme:   parsed.Scheme,
		Host:     parsed.Host,
		Path:     "/web/stream.m3u",
		RawQuery: q.Encode(),
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String(), nil
}

func buildDirectStreamOverrideURL(rawStreamBase, ref string) (string, bool) {
	streamBase := strings.TrimSpace(rawStreamBase)
	if streamBase == "" {
		return "", false
	}

	streamParsed, err := url.Parse(streamBase)
	if err != nil || streamParsed.Host == "" {
		return "", false
	}

	u := &url.URL{
		Scheme: streamParsed.Scheme,
		Host:   streamParsed.Host,
		Path:   "/" + ref,
	}
	return u.String(), true
}

func buildDirectTSStreamURL(parsed *url.URL, ref string, streamPort int) (string, error) {
	host, err := resolveDirectTSHost(parsed, streamPort)
	if err != nil {
		return "", err
	}

	u := &url.URL{
		Scheme: parsed.Scheme,
		Host:   host,
		Path:   "/" + ref,
	}
	return u.String(), nil
}

func resolveDirectTSHost(parsed *url.URL, streamPort int) (string, error) {
	host := parsed.Host
	if host == "" {
		return "", fmt.Errorf("openwebif base URL %q missing host", parsed.String())
	}

	_, existingPort, err := net.SplitHostPort(host)
	if err == nil && existingPort != "" {
		return host, nil
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("openwebif base URL %q missing hostname", parsed.String())
	}

	if streamPort <= 0 {
		streamPort = defaultStreamPort
	}
	return net.JoinHostPort(hostname, strconv.Itoa(streamPort)), nil
}

func resolveStreamPort(port int) int {
	if port > 0 && port <= 65535 {
		return port
	}
	return defaultStreamPort
}
