package vod

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
)

var (
	probeQueueLength = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vod_probe_queue_length",
		Help: "Current number of pending probe requests",
	})
	probeDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vod_probe_dropped_total",
		Help: "Total number of dropped probe requests",
	})
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
		case req, ok := <-m.probeCh:
			if !ok {
				return
			}
			m.processProbe(ctx, req)
		}
	}
}

func (m *Manager) processProbe(ctx context.Context, req probeRequest) {
	// Stampede prevention
	_, err, _ := m.sfg.Do(req.ServiceRef, func() (interface{}, error) {
		if runErr := m.runProbe(ctx, req); runErr != nil {
			return nil, runErr
		}
		return nil, nil
	})
	if err != nil {
		log.Error().Err(err).Str("serviceRef", req.ServiceRef).Msg("probe failed")
	}
}

func (m *Manager) runProbe(ctx context.Context, req probeRequest) error {
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
		m.MarkFailure(id, ArtifactStateFailed, "missing or unresolvable input path", "", nil)
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
		m.MarkFailure(id, state, err.Error(), "", nil)
		log.Warn().Err(err).Str("id", id).Msg("probe failed: stat error")
		return err
	}

	// Logic: If it's FAILED, we might want to retry later.
	// If it's MISSING, it stays MISSING.

	fp := Fingerprint{
		Size:  info.Size(),
		MTime: info.ModTime().Unix(),
	}

	// 4. Efficiency Check: Fingerprint-based skip
	m.mu.Lock()
	current, exists := m.metadata[id]
	if exists {
		// A. MISSING Backoff
		if current.State == ArtifactStateMissing {
			// Throttle re-probes for missing files (e.g. NAS unavailable)
			// Backoff: 1 minute
			lastProbe := time.Unix(0, current.UpdatedAt)
			since := time.Since(lastProbe)
			if since < 0 {
				since = 0
			}
			if since < 1*time.Minute {
				m.mu.Unlock()
				log.Debug().Str("id", id).Msg("skipping probe for MISSING artifact (throttled)")
				return nil
			}
		}

		// B. READY Fingerprint Match
		if current.State == ArtifactStateReady && current.Fingerprint == fp {
			// Valid cache hit? Only if ArtifactPath logic aligns with file extension.
			// If ArtifactPath is set, it MUST be an MP4. If it's a TS, ArtifactPath should be empty.
			// If we see a mismatch (e.g. ArtifactPath set but file is TS), we must re-probe to fix it.
			isMP4 := strings.EqualFold(filepath.Ext(input), ".mp4")
			hasArtifactPath := current.ArtifactPath != ""

			if isMP4 == hasArtifactPath {
				m.mu.Unlock()
				log.Debug().Str("id", id).Msg("fingerprint match, skipping re-probe")
				return nil
			}
			// Fallthrough: Cache invalid due to ArtifactPath mismatch (stale metadata)
			log.Info().Str("id", id).Msg("fingerprint match but artifact mismatch, forcing re-probe")
		}
	}
	m.mu.Unlock()

	// 5. Probe Duration & Truth Enforcement
	// Propagate caller context (worker lifecycle) but wrap with a specialized timeout.
	probeCtx, cancelProbe := context.WithTimeout(ctx, ProbeTimeout)
	defer cancelProbe()

	log.Info().Str("id", id).Str("path", input).Msg("probing recording duration")

	var res *StreamInfo
	var probeErr error
	if m.prober != nil {
		res, probeErr = m.prober.Probe(probeCtx, input)
	}

	// B3/B4: Probe Failure Mapping
	if probeErr != nil {
		state := ArtifactStateFailed
		errMsg := probeErr.Error()

		if errors.Is(probeErr, context.DeadlineExceeded) {
			// B3: Timeout -> Preparing (Transient)
			state = ArtifactStatePreparing
			errMsg = "probe_timeout"
		} else if errors.Is(probeErr, context.Canceled) {
			state = ArtifactStateFailed
			errMsg = "probe_canceled"
		} else {
			// B4: Corrupt/Missing -> Failed (Terminal)
			errMsg = "probe_failed: " + errMsg
		}

		m.MarkFailure(id, state, errMsg, input, &fp)
		log.Warn().Err(probeErr).Str("id", id).Msg("probe failed")
		return probeErr
	}

	var dur int64
	if res != nil {
		dur = int64(math.Round(res.Video.Duration))
	}

	// B2: Duration <= 0 Guard
	if dur <= 0 {
		m.MarkFailure(id, ArtifactStateFailed, "probe_duration_invalid", input, &fp)
		// Duration is preserved by MarkFailure's read-modify-write contract.
		// See: TestTruth_Write_B5_ProbeFailPreservesDuration

		log.Warn().Str("id", id).Int64("duration", dur).Msg("probe returned invalid duration")
		return errors.New("probe_duration_invalid")
	}

	// 5. Update Success (B1)
	m.MarkProbed(id, input, res, &fp)

	log.Debug().Str("id", id).Int64("duration", dur).Msg("recording probe complete")
	return nil
}
