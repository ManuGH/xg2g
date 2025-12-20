// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreopenwebif "github.com/ManuGH/xg2g/internal/core/openwebif"
)

// Helper to convert direct stream URL to Enigma2 Web API URL.
func convertToWebAPI(targetURL, serviceRef string) string {
	return coreopenwebif.ConvertToWebAPI(targetURL, serviceRef)
}

type webAPIStreamInfo struct {
	URL       string
	ProgramID int
}

// resolveWebAPIStreamInfo performs the HTTP request to the Enigma2 Web API to "Zap" the channel
// and returns the actual stream URL plus optional playback hints (e.g. program id) parsed from
// the returned M3U playlist.
func resolveWebAPIStreamInfo(apiURL string) (webAPIStreamInfo, error) {
	// 1. Perform GET request (Zaps the channel)
	resp, err := http.Get(apiURL) // #nosec G107
	if err != nil {
		return webAPIStreamInfo{}, fmt.Errorf("failed to call Web API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return webAPIStreamInfo{}, fmt.Errorf("web API returned status %d", resp.StatusCode)
	}

	// 2. Parse M3U response to find the stream URL
	// Response format:
	// #EXTM3U
	// #EXTINF:-1,ORF 1 HD
	// http://10.10.55.64:8001/1:0:19:132F:3EF:1:C00000:0:0:0:
	var urlLine string
	var programID int

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTVLCOPT:") {
			// Example: #EXTVLCOPT:program=108
			if idx := strings.Index(line, "program="); idx != -1 {
				val := line[idx+len("program="):]
				end := 0
				for end < len(val) && val[end] >= '0' && val[end] <= '9' {
					end++
				}
				if end > 0 {
					if n, err := strconv.Atoi(val[:end]); err == nil && n > 0 {
						programID = n
					}
				}
			}
			continue
		}
		if strings.HasPrefix(line, "http") {
			urlLine = line
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return webAPIStreamInfo{}, fmt.Errorf("error reading M3U: %w", err)
	}

	if urlLine == "" {
		return webAPIStreamInfo{}, fmt.Errorf("no stream URL found in M3U response")
	}

	return webAPIStreamInfo{URL: urlLine, ProgramID: programID}, nil
}

// ZapAndResolveStream performs the full channel zap sequence with the required delay
// for preventing race conditions on encrypted channels. It wraps resolveWebAPIStreamInfo
// and handles the waiting period if the stream is detected as encrypted (or always, for safety).
func ZapAndResolveStream(apiURL string) (string, int, error) {
	// 1. Zap and get stream info
	info, err := resolveWebAPIStreamInfo(apiURL)
	if err != nil {
		return "", 0, err
	}

	// 2. Check encryption/port implications
	// Port 17999 implies oscam-emu (encrypted), which needs time to open the port.
	// Standard port 8001 (FTA) is usually faster, but a consistent delay is safer
	// across the board for Enigma2 tuners to stabilize.

	// We use a fixed 5s delay as validated in production to solve the race condition.
	// This serves as the single source of truth for this specific hardware timing quirk.
	time.Sleep(5 * time.Second)

	return info.URL, info.ProgramID, nil
}
