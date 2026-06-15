package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/version"
)

// StatusResponse represents the system status contract.
type StatusResponse struct {
	Status  string                   `json:"status"` // healthy, degraded, recovering
	Release string                   `json:"release"`
	Digest  string                   `json:"digest"`
	Runtime RuntimeInfo              `json:"runtime"`
	Drift   *verification.DriftState `json:"drift,omitempty"`
}

type RuntimeInfo struct {
	FFmpeg string `json:"ffmpeg"`
	Go     string `json:"go"`
}

// StatusHandler returns the system status.
type StatusHandler struct {
	store     verification.Store
	ffmpegBin string

	mu        sync.Mutex
	ffmpegVer string // cached ONLY after a successful probe; "" until then
}

func NewStatusHandler(store verification.Store, ffmpegBin string) *StatusHandler {
	return &StatusHandler{
		store:     store,
		ffmpegBin: ffmpegBin,
	}
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := StatusResponse{
		Status:  "healthy", // Logic could be enhanced based on drift detected
		Release: version.Version,
		Digest:  version.Commit,
		Runtime: RuntimeInfo{
			// Report the TRUTH, not a fabricated literal: a status endpoint exists to describe
			// the actual runtime, so a hardcoded version would defeat its purpose.
			FFmpeg: h.ffmpegVersion(r.Context()),
			Go:     runtime.Version(),
		},
	}

	if h.store != nil {
		if drift, ok := h.store.Get(r.Context()); ok {
			resp.Drift = &drift
			if drift.Detected {
				resp.Status = "degraded" // Simple logic: drift = degraded
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ffmpegVersion resolves the ffmpeg version lazily on first use and caches ONLY a successful
// result. A failed probe (binary missing/timeout) returns "unknown" WITHOUT caching, so a later
// request retries once ffmpeg becomes available — a status that reported "unknown" because
// ffmpeg was briefly absent at the first request and never corrected would be the same kind of
// lie this fix removes. The lock is held only around the cache read/write, never during the
// exec, so a slow/missing binary cannot block concurrent status requests.
func (h *StatusHandler) ffmpegVersion(ctx context.Context) string {
	h.mu.Lock()
	cached := h.ffmpegVer
	h.mu.Unlock()
	if cached != "" {
		return cached
	}

	v := detectFFmpegVersion(ctx, h.ffmpegBin)
	if v == "" {
		return "unknown"
	}

	h.mu.Lock()
	h.ffmpegVer = v
	h.mu.Unlock()
	return v
}

// detectFFmpegVersion runs `<bin> -version` with a bounded timeout (the call must not hang the
// status endpoint) and returns the parsed version, or "" on any failure (missing binary,
// timeout, unparseable output).
func detectFFmpegVersion(ctx context.Context, bin string) string {
	if strings.TrimSpace(bin) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// #nosec G204 - bin is the operator-configured ffmpeg path, same trust as the transcoder.
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return ""
	}
	return parseFFmpegVersion(string(out))
}

// parseFFmpegVersion extracts the raw token after "version" on the first line of `ffmpeg
// -version` output (e.g. "ffmpeg version 7.0.2 Copyright ..." -> "7.0.2"). It returns the token
// VERBATIM, not a parsed semver: real builds vary widely — "4.4.2-0ubuntu0.22.04.1" (distro),
// "n6.1" (git), "7.0.2" (release) — and forcing a major.minor schema would drop exactly the
// distro suffix that tells an operator which package is running. Returns "" if the expected
// shape isn't found.
func parseFFmpegVersion(output string) string {
	line := output
	if i := strings.IndexByte(output, '\n'); i >= 0 {
		line = output[:i]
	}
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "version" {
			return fields[i+1]
		}
	}
	return ""
}
