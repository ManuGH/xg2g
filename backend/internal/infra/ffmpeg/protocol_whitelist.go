package ffmpeg

import (
	"net/url"
	"strings"
)

const networkInputProtocolWhitelist = "crypto,http,https,tcp,tls"

// InputProtocolWhitelist returns the narrow FFmpeg protocol whitelist for
// remote HTTP(S) inputs. Local file paths intentionally return false so callers
// do not weaken file-based ingest semantics.
func InputProtocolWhitelist(rawInput string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawInput))
	if err != nil {
		return "", false
	}
	if parsed.Host == "" || parsed.Fragment != "" {
		return "", false
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return networkInputProtocolWhitelist, true
	default:
		return "", false
	}
}
