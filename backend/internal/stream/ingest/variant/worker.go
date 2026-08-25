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
	"github.com/ManuGH/xg2g/internal/stream/ingest/ingeststats"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

var (
	ErrWorkerClosed = errors.New("audio variant worker closed")

	// ErrUpstreamGenerationChanged reports that the upstream ring changed topology
	// epoch while this worker was running: a PMT version bump, or a different
	// program. It is an expected cut, not a failure.
	//
	// FFmpeg binds its decoders when it probes the input and keeps them for the
	// life of the process. Measured against two spliced captures on identical PIDs,
	// a running process fed H.264 and then HEVC decoded 77 of the 1578 available
	// frames and labelled the whole output h264; the milder case, AAC replaced by
	// MP2 with the video untouched, broke the audio branch from the change onward.
	// Restating PAT/PMT does not help and cannot: a transport stream repeats its
	// PSI continuously anyway, so the process has always already seen it.
	//
	// A worker therefore serves exactly one generation. The manager builds the next
	// one, and its subscribers re-attach primed on the new topology.
	ErrUpstreamGenerationChanged = errors.New("upstream topology generation changed")
)

// workerStopReason classifies how a worker's run loop ended, for the one metric
// that must not conflate the two: a topology cut is the lifecycle working as
// designed, and counting it as a crash would make a channel that legitimately
// changes its PMT look like a transcoder that keeps falling over.
func workerStopReason(err error) string {
	switch {
	case errors.Is(err, ErrUpstreamGenerationChanged):
		return ingeststats.WorkerStopGenerationChange
	case err == nil, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ingeststats.WorkerStopShutdown
	default:
		return ingeststats.WorkerStopError
	}
}

const (
	// masterAttachTimeout bounds the wait for the upstream ring to offer a random
	// access point. A GOP boundary on DVB is well under a second; a stream that has
	// produced none by now is not one this worker should keep a process open for.
	masterAttachTimeout = 5 * time.Second

	// attachPollInterval is how often a pending attach re-checks the ring.
	attachPollInterval = 10 * time.Millisecond
)

// attachPrimedMaster waits for an atomic attach point on the upstream ring.
//
// A primed attach is the only legal entry into TS data: preamble, keyframe offset,
// generation and reader all come from one snapshot taken under a single ring lock,
// so no interleaved PMT version bump can pair the topology of one generation with
// the bytes of another.
//
// ErrNoKeyframeAvailable is transient - the next GOP boundary is still ahead, and
// it is also the expected state for a few hundred milliseconds after every PMT
// change, because the invalidation drops the keyframe index. It is retried. A
// scrambled or closed ring cannot improve by waiting and is returned immediately.
//
// There is deliberately no fallback to the ring tail. The tail is the oldest byte
// still held, not a point a decoder can start on, and handing it to an encoder is
// what this function exists to prevent.
func attachPrimedMaster(ctx context.Context, r *ring.MasterRing, timeout time.Duration) (ring.PrimedAttachPoint, *ring.SubscriberReader, error) {
	ticker := time.NewTicker(attachPollInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(timeout)

	for {
		attach, reader, err := r.NewPrimedSubscriber()
		if err == nil {
			return attach, reader, nil
		}
		if errors.Is(err, ring.ErrRingClosed) || errors.Is(err, ring.ErrScrambledStream) {
			return ring.PrimedAttachPoint{}, nil, err
		}

		select {
		case <-ctx.Done():
			return ring.PrimedAttachPoint{}, nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return ring.PrimedAttachPoint{}, nil, fmt.Errorf("timed out waiting for primed attach on master ring: %w", err)
			}
		}
	}
}

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

	// staleGeneration records the upstream epoch that ended this worker, or zero
	// if it ended for any other reason. Generations only ever advance, so a cut is
	// always non-zero and the sentinel is unambiguous.
	staleGeneration atomic.Uint64
}

// NewAudioVariantWorker initializes a worker for the given key and upstream MasterRing.
func NewAudioVariantWorker(key AudioVariantKey, masterRing *ring.MasterRing, ringCapacity int) *AudioVariantWorker {
	if ringCapacity <= 0 {
		ringCapacity = 8 * 1024 * 1024 // 8 MB default ring buffer
	}
	variantRing := ring.NewMasterRing(ringCapacity)

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

		ingeststats.RecordVariantWorkerStopped(workerStopReason(err))
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
		Str("srcVideo", w.key.SourceVideoCodec).
		Str("tgtVideo", w.key.TargetVideoCodec).
		Uint16("audioPID", w.key.AudioPID).
		Str("tgtAudio", w.key.TargetAudioCodec).
		Logger()

	logger.Info().Msg("starting stream variant transcode worker")

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

	targetVideo := w.key.TargetVideoCodec
	if targetVideo == "" {
		targetVideo = "copy"
	}
	targetAudio := w.key.TargetAudioCodec
	if targetAudio == "" {
		targetAudio = "copy"
	}

	args := []string{
		"-y", "-nostdin", "-hide_banner", "-loglevel", "warning",
		"-fflags", "+nobuffer+flush_packets+genpts",
		"-analyzeduration", "1000000",
		"-probesize", "1000000",
		"-f", "mpegts", "-i", "pipe:0",
	}

	// 1. Video stream mapping and encoding
	args = append(args, "-map", "0:v:0?")
	if targetVideo == "h264" {
		if w.key.ScanPolicy == "deinterlace_50p" || w.key.ScanPolicy == "" {
			args = append(args, "-vf", "bwdif=mode=send_field:parity=auto:deint=all")
		}
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-g", "50",
		)
	} else {
		args = append(args, "-c:v", "copy")
	}

	// 2. Audio stream mapping and encoding
	if w.key.AudioPID > 0 {
		audioPIDSpec := fmt.Sprintf("0:i:0x%x", w.key.AudioPID)
		args = append(args, "-map", audioPIDSpec)
		if targetAudio == "aac" {
			args = append(args,
				"-c:a", "aac",
				"-b:a", fmt.Sprintf("%dk", bitrate),
				"-ar", fmt.Sprintf("%d", sampleRate),
				"-ac", fmt.Sprintf("%d", channels),
			)
		} else {
			args = append(args, "-c:a", "copy")
		}
	} else {
		args = append(args, "-map", "0:a:0?")
		if targetAudio == "aac" {
			args = append(args,
				"-c:a", "aac",
				"-b:a", fmt.Sprintf("%dk", bitrate),
				"-ar", fmt.Sprintf("%d", sampleRate),
				"-ac", fmt.Sprintf("%d", channels),
			)
		} else {
			args = append(args, "-c:a", "copy")
		}
	}

	if w.key.Language != "" && w.key.Language != "und" {
		args = append(args, "-metadata:s:a:0", fmt.Sprintf("language=%s", w.key.Language))
	}
	if w.key.ProgramNumber > 0 {
		args = append(args, "-mpegts_service_id", fmt.Sprintf("%d", w.key.ProgramNumber))
	}

	args = append(args,
		"-flush_packets", "1",
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	)

	// The input attach happens before FFmpeg exists. A scrambled or closed upstream
	// then fails the worker without leaving a process behind, and the reason reaches
	// Err() instead of being swallowed by a stdin pipe that simply closes.
	attach, masterReader, err := attachPrimedMaster(ctx, w.masterRing, masterAttachTimeout)
	if err != nil {
		return fmt.Errorf("attach variant input: %w", err)
	}
	defer func() { _ = masterReader.Close() }()

	logger.Info().
		Int64("keyframe_offset", attach.KeyframeOffset).
		Uint64("generation", attach.Generation).
		Int("preamble_bytes", len(attach.Preamble)).
		Msg("variant input attached at primed random access point")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	defer func() { _ = stdin.Close() }()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	defer func() { _ = stdout.Close() }()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	defer func() { _ = stderr.Close() }()

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
		defer func() { _ = stdin.Close() }()

		// Preamble and reader come from the same PrimedAttachPoint. Reading them
		// separately would let a PMT version bump land between the two calls, and
		// FFmpeg would be told the topology of one generation while being fed the
		// bytes of another.
		if len(attach.Preamble) > 0 {
			if _, werr := stdin.Write(attach.Preamble); werr != nil {
				return
			}
		}

		go func() {
			<-ctx.Done()
			_ = masterReader.Close()
		}()

		// The upstream subscription is sampled on the loop that already reads it.
		// Its lag is the one that decides whether this worker survives: an overtaken
		// variant worker loses its FFmpeg process, where a native client would only
		// re-enter at the next random access point.
		sampler := ingeststats.NewSubscriberSampler(ingeststats.RoleVariantWorker, masterReader)
		defer sampler.Flush()

		buf := make([]byte, 32*1024)
		for {
			if ctx.Err() != nil {
				return
			}
			n, rerr := masterReader.Read(buf)
			sampler.Sample()

			// Checked between the read and the write, not before the read: the read
			// can block for a whole GOP while waiting to recover, and the topology
			// may change during that wait. Testing afterwards is what guarantees
			// that no byte of a newer generation ever reaches this process.
			if gen := w.masterRing.Generation(); gen != attach.Generation {
				w.staleGeneration.Store(gen)
				return
			}

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

	// Read transcoded MPEG-TS from FFmpeg stdout into VariantRing with strict 188-byte TS packet alignment
	wg.Add(1)
	go func() {
		defer wg.Done()
		var carry []byte
		buf := make([]byte, 32*1024)
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				chunk := append(carry, buf[:n]...)
				start := 0
				for start < len(chunk) && chunk[start] != ring.SyncByte {
					start++
				}
				chunk = chunk[start:]
				pushLen := (len(chunk) / ring.TSPacketSize) * ring.TSPacketSize
				if pushLen > 0 {
					// Two failures are possible and neither is actionable here:
					// a non-packet-sized push, which pushLen has just ruled out, and
					// a closed ring, which means this worker is being torn down and
					// the read loop below is about to end anyway.
					_, _ = w.variantRing.Push(chunk[:pushLen])
				}
				rem := len(chunk) - pushLen
				if rem > 0 {
					carry = append(carry[:0], chunk[pushLen:]...)
				} else {
					carry = carry[:0]
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	// A topology cut outranks whatever FFmpeg exited with. Closing stdin makes it
	// exit on its own, and the exit code that produces says nothing about why the
	// worker ended - reporting it would make an ordinary PMT change look like a
	// crash in logs and metrics.
	if newGen := w.staleGeneration.Load(); newGen != 0 {
		logger.Info().
			Uint64("attached_generation", attach.Generation).
			Uint64("upstream_generation", newGen).
			Msg("upstream topology generation changed; ending variant worker for a clean rebuild")
		return fmt.Errorf("%w: attached at generation %d, upstream is at %d",
			ErrUpstreamGenerationChanged, attach.Generation, newGen)
	}

	if ctx.Err() != nil {
		return nil
	}
	return waitErr
}

// Terminated reports whether this worker's run loop has finished. A finished
// worker is never revived: its VariantRing is closed, and its FFmpeg is bound to
// a topology that may no longer apply.
func (w *AudioVariantWorker) Terminated() bool {
	select {
	case <-w.doneCh:
		return true
	default:
		return false
	}
}

// Done returns a channel closed once the worker's run loop has finished. Err then
// reports why, and ErrUpstreamGenerationChanged distinguishes an expected topology
// cut from a failure.
func (w *AudioVariantWorker) Done() <-chan struct{} {
	return w.doneCh
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
