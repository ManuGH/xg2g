// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

var (
	ErrWorkerClosed = errors.New("audio variant worker closed")
)

// AudioVariantWorker manages an FFmpeg audio-transcoding subprocess that consumes
// the master MPEG-TS stream from MasterRing, copies video elementary streams bit-exact,
// transcodes the specified audio PID to AAC-LC, and publishes the resulting MPEG-TS into a VariantRing.
type AudioVariantWorker struct {
	key         AudioVariantKey
	masterRing  *ring.MasterRing
	variantRing *ring.MasterRing

	cancel context.CancelFunc
	doneCh chan struct{}
	runErr error
	errMu  sync.Mutex

	subscriberCount atomic.Int64
	lastIdleTime    time.Time
	idleMu          sync.Mutex
}

// NewAudioVariantWorker initializes a worker for the given key and upstream MasterRing.
func NewAudioVariantWorker(key AudioVariantKey, masterRing *ring.MasterRing, ringCapacity int) *AudioVariantWorker {
	if ringCapacity <= 0 {
		ringCapacity = 8 * 1024 * 1024 // 8 MB default ring buffer
	}
	variantRing := ring.NewMasterRingWithProgram(ringCapacity, key.ProgramNumber)

	return &AudioVariantWorker{
		key:         key,
		masterRing:  masterRing,
		variantRing: variantRing,
		doneCh:      make(chan struct{}),
	}
}

// Key returns the variant key this worker serves.
func (w *AudioVariantWorker) Key() AudioVariantKey {
	return w.key
}

// Ring returns the downstream VariantRing.
func (w *AudioVariantWorker) Ring() *ring.MasterRing {
	return w.variantRing
}

// SubscriberCount returns active subscriber count.
func (w *AudioVariantWorker) SubscriberCount() int64 {
	return w.subscriberCount.Load()
}

// AddSubscriber increments subscriber count.
func (w *AudioVariantWorker) AddSubscriber() {
	w.subscriberCount.Add(1)
	w.idleMu.Lock()
	w.lastIdleTime = time.Time{}
	w.idleMu.Unlock()
}

// RemoveSubscriber decrements subscriber count and marks idle timestamp if 0.
func (w *AudioVariantWorker) RemoveSubscriber() {
	if val := w.subscriberCount.Add(-1); val <= 0 {
		w.subscriberCount.Store(0)
		w.idleMu.Lock()
		w.lastIdleTime = time.Now()
		w.idleMu.Unlock()
	}
}

// IdleDuration returns how long this worker has had 0 subscribers.
func (w *AudioVariantWorker) IdleDuration() time.Duration {
	w.idleMu.Lock()
	defer w.idleMu.Unlock()
	if w.lastIdleTime.IsZero() || w.subscriberCount.Load() > 0 {
		return 0
	}
	return time.Since(w.lastIdleTime)
}

// Start launches the FFmpeg worker process in the background.
func (w *AudioVariantWorker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go func() {
		defer close(w.doneCh)
		defer w.variantRing.Close()

		err := w.run(ctx)
		w.errMu.Lock()
		w.runErr = err
		w.errMu.Unlock()
	}()
}

// Stop terminates the worker.
func (w *AudioVariantWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	<-w.doneCh
}

// Err returns the fatal error of the worker if one occurred.
func (w *AudioVariantWorker) Err() error {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.runErr
}

func (w *AudioVariantWorker) run(ctx context.Context) error {
	logger := log.L().With().
		Str("variant", w.key.String()).
		Uint16("audioPID", w.key.AudioPID).
		Str("targetCodec", w.key.TargetCodec).
		Logger()

	logger.Info().Msg("starting audio variant transcode worker (video copy, audio transcode)")

	// Build FFmpeg command:
	// -c:v copy preserves the exact video elementary stream without re-encoding
	// -map 0:i:0xPID binds explicitly to the exact source audio PID
	// -muxdelay 0 -muxpreload 0 ensures minimal container latency
	sampleRate := w.key.SampleRate
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	channels := w.key.Channels
	if channels <= 0 {
		channels = 2
	}
	bitrate := w.key.BitrateKbps
	if bitrate <= 0 {
		bitrate = 192
	}

	audioPIDSpec := fmt.Sprintf("0:i:0x%x", w.key.AudioPID)

	args := []string{
		"-y", "-nostdin", "-hide_banner", "-loglevel", "warning",
		"-fflags", "+nobuffer+flush_packets+genpts",
		"-analyzeduration", "1000000",
		"-probesize", "1000000",
		"-f", "mpegts", "-i", "pipe:0",
		"-map", "0:v:0?", "-c:v", "copy",
		"-map", audioPIDSpec, "-c:a", w.key.TargetCodec,
		"-b:a", fmt.Sprintf("%dk", bitrate),
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-flush_packets", "1",
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	defer stdout.Close()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	defer stderr.Close()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	var wg sync.WaitGroup

	// Log stderr from FFmpeg in the background
	wg.Add(1)
	go func() {
		defer wg.Done()
		errBuf := make([]byte, 4096)
		for {
			n, rerr := stderr.Read(errBuf)
			if n > 0 {
				logger.Warn().Str("ffmpeg_stderr", string(errBuf[:n])).Msg("ffmpeg audio variant worker log")
			}
			if rerr != nil {
				return
			}
		}
	}()

	// Feed upstream MasterRing into FFmpeg stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdin.Close()

		// Wait for primed attach from master ring to get PAT/PMT preamble
		var attach ring.PrimedAttachPoint
		var reader *ring.SubscriberReader
		var aerr error

		for attempts := 0; attempts < 50; attempts++ {
			if ctx.Err() != nil {
				return
			}
			attach, reader, aerr = w.masterRing.NewPrimedSubscriber()
			if aerr == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}

		if aerr != nil {
			reader = w.masterRing.NewSubscriberReader(-1)
		} else if len(attach.Preamble) > 0 {
			if _, werr := stdin.Write(attach.Preamble); werr != nil {
				reader.Close()
				return
			}
		}
		defer reader.Close()

		buf := make([]byte, 32*1024)
		for {
			if ctx.Err() != nil {
				return
			}
			n, rerr := reader.Read(buf)
			if n > 0 {
				if _, werr := stdin.Write(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	// Read transcoded MPEG-TS from FFmpeg stdout into VariantRing
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				w.variantRing.Push(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	if ctx.Err() != nil {
		return nil
	}
	return waitErr
}

// PrimedAttachWithTimeout waits up to timeout for the VariantRing to contain a valid keyframe and preamble.
func (w *AudioVariantWorker) PrimedAttachWithTimeout(ctx context.Context, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		attach, reader, err := w.variantRing.NewPrimedSubscriber()
		if err == nil {
			return attach, reader, nil
		}

		if errors.Is(err, ring.ErrRingClosed) || errors.Is(err, ring.ErrScrambledStream) {
			return ring.PrimedAttachPoint{}, nil, err
		}

		select {
		case <-ctx.Done():
			return ring.PrimedAttachPoint{}, nil, ctx.Err()
		case <-w.doneCh:
			if err := w.Err(); err != nil {
				return ring.PrimedAttachPoint{}, nil, fmt.Errorf("variant worker terminated: %w", err)
			}
			return ring.PrimedAttachPoint{}, nil, ErrWorkerClosed
		case <-ticker.C:
			if time.Now().After(deadline) {
				return ring.PrimedAttachPoint{}, nil, fmt.Errorf("timed out waiting for primed attach on audio variant (%s)", w.key.String())
			}
		}
	}
}
