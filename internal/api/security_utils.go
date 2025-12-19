package api

import (
	"net/http"
	"regexp"

	"github.com/ManuGH/xg2g/internal/auth"
)

var segmentAllowList = regexp.MustCompile(`^[a-zA-Z0-9._-]+\.(ts|m4s|mp4|aac|vtt|m3u8)$`)

// extractToken delegates to the shared internal/auth package to ensure parity with valid proxy auth.
func extractToken(r *http.Request, allowQuery bool) string {
	return auth.ExtractToken(r, allowQuery)
}
