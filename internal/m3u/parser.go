package m3u

import (
	"strings"
)

// Channel represents a single channel from the M3U playlist
type Channel struct {
	Number string `json:"number"`
	Name   string `json:"name"`
	TvgID  string `json:"tvg_id"`
	Logo   string `json:"logo"`
	Group  string `json:"group"`
	URL    string `json:"url"`
	HasEPG bool   `json:"has_epg"`
	Raw    string `json:"-"` // Raw EXTINF line
}

// Parse parses M3U content and returns a list of channels
func Parse(content string) []Channel {
	var channels []Channel
	lines := strings.Split(content, "\n")
	var current Channel

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse EXTINF
			// #EXTINF:-1 tvg-id="..." tvg-name="..." tvg-logo="..." group-title="..." tvg-chno="...",Display Name
			current = Channel{
				Raw: line,
			}

			// Extract attributes
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				end := strings.Index(line[idx+10:], `"`)
				if end != -1 {
					current.Number = line[idx+10 : idx+10+end]
				}
			}
			if idx := strings.Index(line, `tvg-id="`); idx != -1 {
				end := strings.Index(line[idx+8:], `"`)
				if end != -1 {
					current.TvgID = line[idx+8 : idx+8+end]
				}
			}
			if idx := strings.Index(line, `tvg-logo="`); idx != -1 {
				end := strings.Index(line[idx+10:], `"`)
				if end != -1 {
					current.Logo = line[idx+10 : idx+10+end]
				}
			}
			if idx := strings.Index(line, `group-title="`); idx != -1 {
				end := strings.Index(line[idx+13:], `"`)
				if end != -1 {
					current.Group = line[idx+13 : idx+13+end]
				}
			}

			// Name is after the last comma
			if idx := strings.LastIndex(line, ","); idx != -1 {
				current.Name = strings.TrimSpace(line[idx+1:])
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") {
			// URL line
			current.URL = line
			// Check if we have EPG data for this channel (simple check based on TvgID presence)
			current.HasEPG = current.TvgID != ""
			channels = append(channels, current)
		}
	}
	return channels
}
