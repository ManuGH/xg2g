package proxy

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"strings"
) // Helper to convert direct stream URL to Enigma2 Web API URL
// User Request: http://10.10.55.64/web/stream.m3u?ref=...
// This allows xg2g to Use the official Enigma2 endpoint which handles
// Tuner allocation and Session management.
// Confirmed: User has 8 FBC Tuners, so "Zapping" via API does NOT disturb the main TV.
func convertToWebAPI(targetURL, serviceRef string) string {
	// targetURL is typically http://<host>:8001/<REF> or http://<host>:17999/<REF>
	// We want: http(s)://<host>/web/stream.m3u?ref=<REF>&name=Stream
	// Always prefer the provided serviceRef if available; fallback to the path.
	if strings.Contains(targetURL, "/web/stream.m3u") {
		return targetURL
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return targetURL
	}

	host := strings.Split(parsed.Host, ":")[0]
	realRef := serviceRef
	if realRef == "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) > 0 {
			realRef = parts[len(parts)-1]
		}
	}
	if realRef == "" {
		return targetURL
	}

	escapedRef := url.QueryEscape(realRef)
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/web/stream.m3u?ref=%s&name=Stream", scheme, host, escapedRef)
}

// resolveWebAPI performs the HTTP request to the Enigma2 Web API to "Zap" the channel
// and returns the actual video stream URL from the M3U response.
func resolveWebAPI(apiURL string) (string, error) {
	// 1. Perform GET request (Zaps the channel)
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to call Web API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Web API returned status %d", resp.StatusCode)
	}

	// 2. Parse M3U response to find the stream URL
	// Response format:
	// #EXTM3U
	// #EXTINF:-1,ORF 1 HD
	// http://10.10.55.64:8001/1:0:19:132F:3EF:1:C00000:0:0:0:
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "http") {
			return line, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading M3U: %w", err)
	}

	return "", fmt.Errorf("no stream URL found in M3U response")
}
