// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package livesource

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

const (
	// snapshotMinBytes is the head a prober wants before it runs, when it does not
	// ask for a size of its own. It is a floor, not a target: whatever arrived when
	// the read ends is what it gets.
	snapshotMinBytes = 2 << 20

	// snapshotMaxBytes caps what a caller may ask for, so one probe cannot buffer
	// an unbounded slice of a broadcast in memory.
	snapshotMaxBytes = 16 << 20

	// snapshotReadTimeout bounds the read of one snapshotMinBytes-sized head so a
	// thin or stalled stream cannot hold a probe open on the receiver. A larger
	// request scales it, up to snapshotReadTimeoutMax.
	snapshotReadTimeout    = 8 * time.Second
	snapshotReadTimeoutMax = 20 * time.Second
)

// sessionKey builds the ingest session identity for serviceRef. It is the same
// key AcquireLiveSource uses, because a probe asking about a service and a
// transcode playing it must land on the one connection.
func (p *Provider) sessionKey(serviceRef string) (session.SessionKey, error) {
	key := session.NewSessionKey(p.receiverHost, p.streamPort, serviceRef)
	key.TargetProgram = targetProgramFromServiceRef(serviceRef)
	if err := key.Validate(); err != nil {
		return session.SessionKey{}, fmt.Errorf("invalid service reference %q: %w", serviceRef, err)
	}
	return key, nil
}

// IsLive reports whether shared ingest already holds a receiver connection for
// serviceRef. A caller that would otherwise dial the receiver itself asks this
// first: a second connection to the same service makes the receiver rebuild the
// CA PMT for that program when either of them closes, and the rebuild takes
// descrambling away from the session that is still running.
func (p *Provider) IsLive(serviceRef string) bool {
	if p == nil || p.manager == nil {
		return false
	}
	manager := p.manager()
	if manager == nil {
		return false
	}
	key, err := p.sessionKey(serviceRef)
	if err != nil {
		return false
	}
	return manager.HasSession(key)
}

// SnapshotHead writes the head of the shared ingest for serviceRef to a temp file
// and returns its path together with a release func that removes it.
//
// This is what a prober gets instead of a receiver URL. Joining the shared ingest
// means a probe that runs while a session plays the same service costs no second
// receiver connection - the two coalesce onto the one upstream - and a probe of a
// service nobody is watching goes through the same tuner admission every other
// consumer does, rather than around it.
//
// minBytes is how much of the head the caller wants; zero asks for the default.
// It is a floor, not a promise - a short head is still returned, because a prober
// caps its own reads anyway and a partial answer beats no answer.
func (p *Provider) SnapshotHead(ctx context.Context, serviceRef string, minBytes int) (string, func(), error) {
	if p == nil || p.manager == nil {
		return "", nil, fmt.Errorf("live source provider not configured")
	}

	minBytes, readTimeout := snapshotBudget(minBytes)

	source, err := p.AcquireLiveSource(ctx, serviceRef)
	if err != nil {
		return "", nil, err
	}
	defer source.Release()

	ls, ok := source.(*liveSource)
	if !ok {
		return "", nil, fmt.Errorf("shared ingest source does not expose a pipeline")
	}

	// The live head, not a primed attach: a prober reads PSI and packets, it does
	// not decode, so it needs neither a random access point nor a descrambled
	// picture. Requiring either would make a probe of an encrypted channel fail
	// where reading the PMT still answers what the channel carries.
	reader, err := ls.pipe.LiveAttach()
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = reader.Close() }()

	head, err := readHead(ctx, reader, reader, minBytes, readTimeout)
	if len(head) == 0 {
		if err == nil {
			err = fmt.Errorf("shared ingest delivered no bytes for %q", serviceRef)
		}
		return "", nil, err
	}

	tmp, err := os.CreateTemp("", "xg2g-probe-head-*.ts")
	if err != nil {
		return "", nil, fmt.Errorf("create probe snapshot: %w", err)
	}
	name := tmp.Name()
	if _, werr := tmp.Write(head); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", nil, fmt.Errorf("write probe snapshot: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(name)
		return "", nil, fmt.Errorf("close probe snapshot: %w", cerr)
	}

	return name, func() { _ = os.Remove(name) }, nil
}

// snapshotBudget turns a requested head size into the size actually read and the
// time allowed for it. The time scales with the size, because the stream arrives
// at broadcast rate however much of it is asked for.
func snapshotBudget(minBytes int) (int, time.Duration) {
	if minBytes <= 0 {
		minBytes = snapshotMinBytes
	}
	if minBytes > snapshotMaxBytes {
		minBytes = snapshotMaxBytes
	}

	timeout := time.Duration(minBytes) * snapshotReadTimeout / time.Duration(snapshotMinBytes)
	if timeout < snapshotReadTimeout {
		timeout = snapshotReadTimeout
	}
	if timeout > snapshotReadTimeoutMax {
		timeout = snapshotReadTimeoutMax
	}
	return minBytes, timeout
}

// readHead reads up to minBytes from src, giving up after timeout or when the
// caller's context ends. A short head is still returned: the prober caps its own
// reads anyway, and a partial answer beats no answer.
//
// unblock is closed to wake a Read that is parked on the ring waiting for bytes
// that are not coming; without it the timeout could not end the read.
func readHead(ctx context.Context, src io.Reader, unblock io.Closer, minBytes int, timeout time.Duration) ([]byte, error) {
	type result struct {
		buf []byte
		err error
	}

	done := make(chan result, 1)
	go func() {
		buf := make([]byte, 0, minBytes)
		chunk := make([]byte, 64<<10)
		for len(buf) < minBytes {
			n, err := src.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
			}
			if err != nil {
				done <- result{buf: buf, err: err}
				return
			}
		}
		done <- result{buf: buf}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-done:
		if res.err != nil && res.err != io.EOF {
			return res.buf, res.err
		}
		return res.buf, nil
	case <-timer.C:
	case <-ctx.Done():
	}

	// The read is still parked in the ring. Closing the subscriber wakes it, and
	// whatever it had collected by then is the head.
	if unblock != nil {
		_ = unblock.Close()
	}
	res := <-done
	return res.buf, nil
}
