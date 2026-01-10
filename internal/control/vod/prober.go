package vod

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/rs/zerolog/log"
)

// probeRequest represents a request to enrich a recording's metadata.
type probeRequest struct {
	// ServiceRef is the recording service reference.
	ServiceRef string
	InputPath  string
}

const (
	MaxConcurrentProbes = 4
	ProbeQueueSize      = 100
	ProbeTimeout        = 30 * time.Second
	StatTimeout         = 5 * time.Second
)

// StartProberPool initializes the background workers.
func (m *Manager) StartProberPool(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	for i := 0; i < MaxConcurrentProbes; i++ {
		m.workerWg.Add(1)
		go m.probeWorker(ctx)
	}
}

func (m *Manager) probeWorker(ctx context.Context) {
	defer m.workerWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-m.probeCh:
			m.processProbe(req)
		}
	}
}

func (m *Manager) processProbe(req probeRequest) {
	// Stampede prevention
	_, err, _ := m.sfg.Do(req.ServiceRef, func() (interface{}, error) {
		if runErr := m.runProbe(req); runErr != nil {
			return nil, runErr
		}
		return nil, nil
	})
	if err != nil {
		log.Error().Err(err).Str("serviceRef", req.ServiceRef).Msg("probe failed")
	}
}

func (m *Manager) runProbe(req probeRequest) error {
	id := req.ServiceRef
	input := req.InputPath

	// 1. Resolve Path if not provided
	if input == "" {
		m.mu.Lock()
		if current, exists := m.metadata[id]; exists && current.ResolvedPath != "" {
			input = current.ResolvedPath
		}
		m.mu.Unlock()
	}
	if input == "" && m.pathMapper != nil {
		receiverPath := recordings.ExtractPathFromServiceRef(id)
		if p, ok := m.pathMapper.ResolveLocalExisting(receiverPath); ok {
			input = p
		}
	}

	if input == "" {
		m.UpdateMetadata(id, Metadata{
			State:     ArtifactStateFailed,
			Error:     "missing or unresolvable input path",
			UpdatedAt: time.Now().Unix(),
		})
		log.Warn().Str("id", id).Msg("probe failed: missing input path")
		return errors.New("missing input path")
	}

	// 2. Stat and Fingerprint
	info, err := os.Stat(input)
	if err != nil {
		state := ArtifactStateFailed
		if os.IsNotExist(err) {
			state = ArtifactStateMissing
		}
		m.UpdateMetadata(id, Metadata{
			State:     state,
			Error:     err.Error(),
			UpdatedAt: time.Now().Unix(),
		})
		log.Warn().Err(err).Str("id", id).Msg("probe failed: stat error")
		return err
	}

	// Logic: If it's FAILED, we might want to retry later.
	// If it's MISSING, it stays MISSING.

	fp := Fingerprint{
		Size:  info.Size(),
		MTime: info.ModTime().Unix(),
	}

	// 3. Efficiency Check: Fingerprint-based skip
	m.mu.Lock()
	current, exists := m.metadata[id]
	if exists && current.State == ArtifactStateReady && current.Fingerprint == fp {
		// Valid cache hit? Only if ArtifactPath logic aligns with file extension.
		// If ArtifactPath is set, it MUST be an MP4. If it's a TS, ArtifactPath should be empty.
		// If we see a mismatch (e.g. ArtifactPath set but file is TS), we must re-probe to fix it.
		isMP4 := len(input) > 4 && input[len(input)-4:] == ".mp4"
		hasArtifactPath := current.ArtifactPath != ""

		if isMP4 == hasArtifactPath {
			m.mu.Unlock()
			log.Debug().Str("id", id).Msg("fingerprint match, skipping re-probe")
			return nil
		}
		// Fallthrough: Cache invalid due to ArtifactPath mismatch (stale metadata)
		log.Info().Str("id", id).Msg("fingerprint match but artifact mismatch, forcing re-probe")
	}
	m.mu.Unlock()

	// 4. Probe Duration (Long Pole: Isolation & Timeout)
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancelProbe()

	log.Info().Str("id", id).Str("path", input).Msg("probing recording duration")

	dur := 0.0
	if m.prober != nil {
		res, err := m.prober.Probe(probeCtx, input)
		if err == nil && res != nil {
			dur = res.Video.Duration
		} else if err != nil {
			log.Warn().Err(err).Str("id", id).Msg("probe failed")
		}
	}

	// 5. Update Success (Atomicity)
	meta := Metadata{
		State:        ArtifactStateReady,
		ResolvedPath: input,
		Duration:     dur,
		Fingerprint:  fp,
		UpdatedAt:    time.Now().Unix(),
	}

	// Only treat as artifact if it's an MP4
	if len(input) > 4 && input[len(input)-4:] == ".mp4" {
		meta.ArtifactPath = input
	}

	m.UpdateMetadata(id, meta)

	log.Debug().Str("id", id).Float64("duration", dur).Msg("recording probe complete")
	return nil
}
