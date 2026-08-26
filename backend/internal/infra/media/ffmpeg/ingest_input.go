// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

const (
	// sharedIngestAttachTimeout bounds the wait for a random access point. The
	// ingest may have just started, in which case there is nothing to join yet.
	sharedIngestAttachTimeout = 10 * time.Second

	// sharedIngestSpoolMaxBytes caps the startup buffer. Past this the spool stops
	// pulling ahead and becomes a pass-through, which is the steady state anyway.
	sharedIngestSpoolMaxBytes = 8 << 20

	// sharedIngestSnapshotMinBytes is how much of the head the probes want before
	// they run. It is a floor, not a target: whatever has arrived when the wait
	// ends is what they get.
	sharedIngestSnapshotMinBytes = 2 << 20

	// sharedIngestSnapshotTimeout bounds that wait so a thin or stalled stream
	// cannot hold up the spawn indefinitely.
	sharedIngestSnapshotTimeout = 6 * time.Second
)

// errSpoolEndedEarly reports that the spool stopped before the probes had the
// head they asked for.
var errSpoolEndedEarly = errors.New("startup spool ended before the requested head was buffered")

// sharedIngestInput is a live transcode's attachment to shared ingest.
//
// One receiver connection exists for the service, opened by the ingest connector
// and shared with every other consumer. This type takes one subscriber cursor off
// it and puts a bounded spool in front, which serves two readers that would
// otherwise need two connections: FFmpeg reads it as its stdin, and the startup
// probes read a snapshot of its buffered head from a file. The spool replays what
// it buffered, so probing the head does not take bytes away from FFmpeg.
type sharedIngestInput struct {
	source ports.LiveSource
	spool  *boundedStartupSpool
	reader io.ReadCloser

	// snapshotPath is a temp file holding the buffered head. The probes are given
	// this path where they used to be given a receiver URL; their arguments and
	// their parsing are unchanged, only the transport is.
	snapshotPath string

	// releaseOnce makes Release safe to call from both paths that can own the
	// input: Start releases it when the spawn never happens, and the monitor
	// releases it when the process exits. Both can run for the same input, and
	// this must not depend on the LiveSource behind it being idempotent too.
	releaseOnce sync.Once
}

// acquireSharedIngestInput joins the shared ingest of spec's service and prepares
// the spool and the probe snapshot.
//
// It never falls back to opening the receiver: a live transcode that cannot get
// bytes from shared ingest has to fail, or the parallel path this replaces would
// quietly survive as an error handler.
func (a *LocalAdapter) acquireSharedIngestInput(ctx context.Context, spec ports.StreamSpec) (*sharedIngestInput, error) {
	if a.LiveSources == nil {
		return nil, fmt.Errorf("shared ingest is not configured; refusing to open the receiver directly")
	}

	source, err := a.LiveSources.AcquireLiveSource(ctx, spec.Source.ID)
	if err != nil {
		return nil, fmt.Errorf("acquire shared ingest for %q: %w", spec.Source.ID, err)
	}

	in := &sharedIngestInput{source: source}
	ok := false
	defer func() {
		if !ok {
			in.Release()
		}
	}()

	preamble, reader, err := source.Attach(ctx, sharedIngestAttachTimeout)
	if err != nil {
		return nil, err
	}
	in.reader = reader

	// The preamble is prepended rather than written separately so it is simply the
	// head of the same byte stream: whatever reads the spool - FFmpeg or a probe -
	// sees the PSI before the payload it describes, without either of them having
	// to know a preamble exists.
	in.spool = newBoundedStartupSpool(io.MultiReader(bytes.NewReader(preamble), reader), spec.SessionID, a)
	go in.spool.run(sharedIngestSpoolMaxBytes)

	facts := source.Facts()
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "shared_ingest_attached").
		Str("service_ref", spec.Source.ID).
		Uint64("generation", facts.Generation).
		Str("video_codec", facts.VideoCodec).
		Int("preamble_bytes", len(preamble)).
		Bool("descrambled", facts.Descrambled()).
		Bool("joinable", facts.Joinable()).
		Msg("live input attached to shared ingest")

	// A failed snapshot is not fatal. The probes then fall back exactly as they do
	// today when a probe fails, and the transcode still runs on shared ingest
	// bytes - which is the property this change exists to establish.
	path, err := a.writeStartupSnapshot(ctx, spec.SessionID, in.spool)
	if err != nil {
		a.Logger.Warn().Err(err).
			Str("session_id", spec.SessionID).
			Str("startup_phase", "shared_ingest_snapshot_failed").
			Msg("startup probes will fall back to defaults; no receiver connection was opened")
	} else {
		in.snapshotPath = path
	}

	ok = true
	return in, nil
}

// ProbePath is what the startup probes read instead of a receiver URL. It is
// empty when no snapshot could be taken, which the probes treat as an unreadable
// input and answer with their existing fallbacks.
func (in *sharedIngestInput) ProbePath() string {
	if in == nil {
		return ""
	}
	return in.snapshotPath
}

// Stdin is the byte stream handed to FFmpeg.
func (in *sharedIngestInput) Stdin() io.Reader {
	if in == nil {
		return nil
	}
	return in.spool
}

// Release ends this transcode's claim on the shared ingest and drops the
// snapshot. It must not run before the FFmpeg process has exited: the upstream
// stays alive only while a holder is attached, and releasing early would pull the
// stream out from under a process still reading it.
func (in *sharedIngestInput) Release() {
	if in == nil {
		return
	}
	in.releaseOnce.Do(in.release)
}

func (in *sharedIngestInput) release() {
	if in.spool != nil {
		in.spool.close()
	}
	if in.reader != nil {
		_ = in.reader.Close()
	}
	if in.snapshotPath != "" {
		_ = os.Remove(in.snapshotPath)
		in.snapshotPath = ""
	}
	if in.source != nil {
		in.source.Release()
	}
}

// writeStartupSnapshot waits for the spool to buffer a usable head and writes it
// to a temp file for the probes.
func (a *LocalAdapter) writeStartupSnapshot(ctx context.Context, sessionID string, spool *boundedStartupSpool) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, sharedIngestSnapshotTimeout)
	defer cancel()

	head, err := waitForSpoolHead(waitCtx, spool, sharedIngestSnapshotMinBytes)
	if len(head) == 0 {
		if err == nil {
			err = errSpoolEndedEarly
		}
		return "", err
	}

	tmp, err := os.CreateTemp("", "xg2g-ingest-head-"+sanitizeTempSuffix(sessionID)+"-*.ts")
	if err != nil {
		return "", fmt.Errorf("create startup snapshot: %w", err)
	}
	name := tmp.Name()
	if _, werr := tmp.Write(head); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("write startup snapshot: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close startup snapshot: %w", cerr)
	}
	return name, nil
}

// waitForSpoolHead blocks until the spool holds minBytes, the stream ends, or the
// context expires, and returns whatever was buffered by then. A short head is
// still worth returning: the probes cap their own reads anyway, and a partial
// answer beats no answer.
func waitForSpoolHead(ctx context.Context, spool *boundedStartupSpool, minBytes int) ([]byte, error) {
	if spool == nil {
		return nil, errSpoolEndedEarly
	}

	// The spool signals on its own condition variable, which a context expiring
	// does not touch. This wakes the waiter so the loop can re-check.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			spool.mu.Lock()
			spool.cond.Broadcast()
			spool.mu.Unlock()
		case <-stop:
		}
	}()

	spool.mu.Lock()
	for spool.totalBytes < minBytes && spool.err == nil && ctx.Err() == nil && spool.state != stateClosed {
		spool.cond.Wait()
	}
	head := make([]byte, 0, spool.totalBytes)
	for _, chunk := range spool.chunks {
		head = append(head, chunk...)
	}
	spoolErr := spool.err
	spool.mu.Unlock()

	if len(head) == 0 {
		if spoolErr != nil {
			return nil, spoolErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errSpoolEndedEarly
	}
	return head, nil
}

// sanitizeTempSuffix keeps a session id usable inside a file name.
func sanitizeTempSuffix(s string) string {
	if len(s) > 16 {
		s = s[:16]
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
