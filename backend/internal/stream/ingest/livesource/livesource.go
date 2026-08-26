// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package livesource adapts shared ingest to the media path.
//
// It exists so a media path can hold live bytes without knowing the ingest
// session API, and - more to the point - without being able to reach the pieces
// of it that would let it open a receiver connection of its own. What it hands
// out is a reader and the facts the ingest already established; there is no URL
// anywhere in the surface.
package livesource

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/stream/ingest/pipeline"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// Provider acquires shared ingest sessions for a single receiver.
type Provider struct {
	manager      func() *session.Manager
	receiverHost string
	streamPort   int
}

// NewProvider builds a provider bound to one receiver's stream endpoint. The
// host and port are the ingest session's identity, not something a caller may
// vary per request: two callers asking for the same service must land on the
// same upstream or the coalescing this package exists for does not happen.
func NewProvider(manager *session.Manager, receiverHost string, streamPort int) *Provider {
	return NewLazyProvider(func() *session.Manager { return manager }, receiverHost, streamPort)
}

// NewLazyProvider is NewProvider for the case where the manager does not exist
// yet at wiring time.
//
// The live route builds its manager when its router is built, which happens after
// the media path has been assembled. Resolving on first use keeps both sides
// sharing the one manager without reordering the bootstrap around it - and a
// provider that resolves to nil refuses the source rather than inventing a second
// manager, which would silently double the receiver connections.
func NewLazyProvider(manager func() *session.Manager, receiverHost string, streamPort int) *Provider {
	return &Provider{
		manager:      manager,
		receiverHost: receiverHost,
		streamPort:   streamPort,
	}
}

// AcquireLiveSource joins the shared ingest of serviceRef, starting it if this is
// the first holder.
func (p *Provider) AcquireLiveSource(ctx context.Context, serviceRef string) (ports.LiveSource, error) {
	if p == nil || p.manager == nil {
		return nil, fmt.Errorf("live source provider not configured")
	}
	manager := p.manager()
	if manager == nil {
		return nil, fmt.Errorf("shared ingest manager is not available yet")
	}

	key := session.NewSessionKey(p.receiverHost, p.streamPort, serviceRef)
	key.TargetProgram = targetProgramFromServiceRef(serviceRef)
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid service reference %q: %w", serviceRef, err)
	}

	lease, err := manager.Acquire(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("acquire shared ingest: %w", err)
	}

	payload := lease.Session().Payload()
	pipe, ok := payload.(*pipeline.SessionPipeline)
	if !ok || pipe == nil {
		lease.Release()
		return nil, fmt.Errorf("shared ingest session holds no usable pipeline")
	}

	return &liveSource{lease: lease, pipe: pipe}, nil
}

// targetProgramFromServiceRef reads the DVB program number out of an Enigma2
// service reference. Zero means "the first program in the PAT", which is what a
// reference without a usable service id should fall back to.
func targetProgramFromServiceRef(serviceRef string) uint16 {
	parts := strings.Split(serviceRef, ":")
	if len(parts) < 4 {
		return 0
	}
	val, err := strconv.ParseUint(parts[3], 16, 16)
	if err != nil {
		return 0
	}
	return uint16(val)
}

// liveSource is one holder's attachment. The lease is what keeps the shared
// upstream alive for this holder; the pipeline is what the bytes come from.
type liveSource struct {
	lease       *session.Lease
	pipe        *pipeline.SessionPipeline
	releaseOnce sync.Once
}

func (s *liveSource) Attach(ctx context.Context, timeout time.Duration) ([]byte, io.ReadCloser, error) {
	attach, reader, err := s.pipe.PrimedAttachWithTimeout(ctx, timeout)
	if err != nil {
		// The pipeline's own error says why the upstream failed, where err only
		// says the attach did not happen in time.
		if runErr := s.pipe.Err(); runErr != nil {
			return nil, nil, fmt.Errorf("attach to shared ingest: %w (upstream: %v)", err, runErr)
		}
		return nil, nil, fmt.Errorf("attach to shared ingest: %w", err)
	}
	return attach.Preamble, reader, nil
}

func (s *liveSource) Facts() ports.LiveSourceFacts {
	masterRing := s.pipe.MasterRing()
	if masterRing == nil {
		return ports.LiveSourceFacts{}
	}
	return factsFrom(masterRing.ReadinessFacts())
}

func (s *liveSource) Release() {
	s.releaseOnce.Do(func() {
		s.lease.Release()
	})
}

func factsFrom(f ring.ReadinessFacts) ports.LiveSourceFacts {
	return ports.LiveSourceFacts{
		Generation:            f.Generation,
		HasPAT:                f.HasPAT,
		HasPMT:                f.HasPMT,
		VideoPID:              f.VideoPID,
		VideoCodec:            string(f.VideoCodec),
		ParameterSetsSeen:     f.ParameterSetsSeen,
		CleanEntryPoints:      f.CleanEntryPoints,
		CleanAccessUnits:      f.CleanAccessUnits,
		ScrambledVideoPackets: f.Scrambling.VideoScrambled,
		ClearVideoPackets:     f.Scrambling.VideoClear,
	}
}
