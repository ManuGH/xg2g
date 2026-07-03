// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
)

// EnableInMemoryIngest activates the internal HTTP ingest server for Zero-Copy HLS streaming.
func (a *LocalAdapter) EnableInMemoryIngest(registry *ringbuffer.Registry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inMemoryIngest = true
	if a.ingestServer == nil {
		srv, err := ringbuffer.NewIngestServer(0, a.HLSRoot, registry, a.Logger, a.shouldRecordSession)
		if err != nil {
			return err
		}
		a.ingestServer = srv
		a.ingestPort = srv.Port()
		srv.Start()
	}
	return nil
}

func (a *LocalAdapter) shouldRecordSession(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for handle, prof := range a.finalizedProfiles {
		if strings.HasPrefix(string(handle), sessionID+"-") {
			return prof.DVRWindowSec > 0
		}
	}
	return false
}
