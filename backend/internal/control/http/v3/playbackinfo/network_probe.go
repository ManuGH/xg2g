// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

import (
	"net"
	"net/http"
)

const (
	playbackProbeBytes      = 512 * 1024
	playbackProbeChunkBytes = 32 * 1024
	playbackProbeHeader     = "X-XG2G-Playback-Probe"
	playbackProbeLAN        = "lan"
	playbackProbeMeasured   = "measured"
)

// zeroChunk is a shared read-only zero-filled buffer reused across all probe requests.
// w.Write only reads from the slice, so sharing it is thread-safe.
var zeroChunk = make([]byte, playbackProbeChunkBytes)

// servePlaybackNetworkProbe provides a small, cache-free transfer over the
// exact route used for media. It intentionally returns no body for direct LAN
// clients: a private client address is only honoured when it is the peer or
// was supplied by a configured trusted proxy (exposureClientKey enforces that
// invariant). Public and carrier-grade-NAT clients receive the full sample.
func (s *Service) ServePlaybackNetworkProbe(w http.ResponseWriter, r *http.Request, clientIP string) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("Vary", "Cookie, Authorization")

	if isPrivatePlaybackClient(clientIP) {
		w.Header().Set(playbackProbeHeader, playbackProbeLAN)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set(playbackProbeHeader, playbackProbeMeasured)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", "524288")
	w.WriteHeader(http.StatusOK)

	for remaining := playbackProbeBytes; remaining > 0; {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		size := playbackProbeChunkBytes
		if remaining < size {
			size = remaining
		}
		n, err := w.Write(zeroChunk[:size])
		if err != nil || n != size {
			return
		}
		remaining -= n
	}
}

func isPrivatePlaybackClient(clientIP string) bool {
	ip := net.ParseIP(clientIP)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}
