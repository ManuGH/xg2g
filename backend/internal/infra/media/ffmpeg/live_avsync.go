package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// Live-copy A/V-sync ("orphan atrim"). DVB/OSCam stream-relay sources deliver
// audio up to ~2s before the first decodable video keyframe on tune-in. In
// -c:v copy + fMP4 output that lead becomes a per-track edit-list start offset
// in the init segment, which iOS AVPlayer applies as a constant "audio leads
// video" desync that survives seeking. Fix: over ONE relay connection, peek the
// stream head, measure the orphan (first video keyframe PTS - first audio PTS),
// and trim the leading audio so both tracks share an origin. Video stays copy;
// only the (already transcoded) audio loses its orphan-length head.
//
// Flag-gated (XG2G_LIVE_AVSYNC_ATRIM, default off) and fully fail-safe: any peek
// or measurement failure falls back to the unchanged direct-URL copy path.

const (
	avsyncMinOrphanSec = 0.05 // below this there is no perceptible desync
	avsyncMaxOrphanSec = 6.0  // above this the measurement is implausible
	avsyncPeekTimeout  = 8 * time.Second
)

const (
	// Peek read/probe granularity. We read the relay head in small chunks and
	// re-probe frequently so the peek ends as soon as the first video keyframe is
	// buffered - the relay delivers the tune-in head slowly, so every byte we don't
	// over-read shortens the startup window (during which the client has no playlist
	// yet and briefly shows a transient network error).
	avsyncPeekReadChunk  = 64 << 10 // relay read granularity
	avsyncPeekFirstProbe = 64 << 10 // start probing once this much head is buffered
	avsyncPeekProbeStep  = 64 << 10 // re-probe after each additional step
	avsyncPeekMaxBytes   = 6 << 20  // give up past here -> fall back to direct path
)

// shouldAvsyncAtrim reports whether the orphan-correction path applies to this
// spec: flag on, live fMP4 over an HTTP/relay source. Video-copy gets a leading
// audio atrim (video timestamps are immutable there); video-transcode gets an
// input-side -ss to the first keyframe, which drops the orphan audio AND the
// stray pre-GOP salvage frame the decoder emits from the corrupt head (observed
// as a 1-frame segment followed by a ~2s video hole while audio runs on).
func (a *LocalAdapter) shouldAvsyncAtrim(spec ports.StreamSpec) bool {
	if !a.LiveAvsyncAtrim {
		return false
	}
	if spec.Mode != ports.ModeLive {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return false
	}
	return spec.Source.Type != ports.SourceFile
}

var errSpoolLimit = errors.New("spool limit reached")

type spoolState int

const (
	stateBuffering spoolState = iota
	stateDecided
	stateClosed
)

// boundedStartupSpool continuously reads from body into a chunk queue in the background.
// Only the single producer goroutine (run) ever reads from resp.Body over the entire session.
// During startup (stateBuffering), chunks accumulate in RAM without discarding any so ffprobe
// can take a snapshot. Once decided (stateDecided), Read() drains chunks and drops consumed ones
// for GC, and if totalBytes exceeds maxBytes, run() blocks waiting for consumer backpressure.
type boundedStartupSpool struct {
	body       io.Reader
	sessionID  string
	adapter    *LocalAdapter
	mu         sync.Mutex
	cond       *sync.Cond
	chunks     [][]byte
	totalBytes int
	readOffset int
	state      spoolState
	err        error
}

func newBoundedStartupSpool(body io.Reader, sessionID string, adapter *LocalAdapter) *boundedStartupSpool {
	s := &boundedStartupSpool{
		body:      body,
		sessionID: sessionID,
		adapter:   adapter,
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *boundedStartupSpool) run(maxBytes int) {
	metrics.IncActiveAvsyncSpools()
	defer func() {
		metrics.DecActiveAvsyncSpools()
		if s.adapter != nil {
			dc := s.adapter.GetDiagnosticContext(s.sessionID)
			s.adapter.Logger.Info().
				Str("session_id", dc.SessionID).
				Str("generation_id", dc.GenerationID).
				Str("reason", dc.Reason).
				Int64("elapsed_since_stop_ms", dc.ElapsedSinceStopMs).
				Msg("avsync_spool_producer_exited")
		}
	}()
	for {
		chunk := make([]byte, 32<<10)
		n, err := s.body.Read(chunk)
		s.mu.Lock()
		if s.state == stateClosed {
			s.mu.Unlock()
			return
		}
		if n > 0 {
			ch := make([]byte, n)
			copy(ch, chunk[:n])
			s.chunks = append(s.chunks, ch)
			s.totalBytes += n
			s.cond.Broadcast()
		}
		if err != nil {
			s.err = err
			s.cond.Broadcast()
			s.mu.Unlock()
			return
		}
		// If we reach max limit while still buffering, force decision to unblock analyzer immediately
		if s.state == stateBuffering && s.totalBytes >= maxBytes {
			s.err = errSpoolLimit
			s.cond.Broadcast()
		}
		// If we hit limit during buffering, wait until decision transitions us out of stateBuffering
		for s.state == stateBuffering && s.totalBytes >= maxBytes && s.err == errSpoolLimit {
			s.cond.Wait()
			if s.state == stateClosed {
				s.mu.Unlock()
				return
			}
		}
		// If we are decided and the buffer is full, wait for consumer (Read) to drain before reading more
		for s.state == stateDecided && s.totalBytes >= maxBytes && s.err == nil {
			s.cond.Wait()
			if s.state == stateClosed {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
	}
}

func (s *boundedStartupSpool) markDecided() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == stateBuffering {
		s.state = stateDecided
		if s.err == errSpoolLimit {
			s.err = nil
		}
		s.cond.Broadcast()
	}
}

func (s *boundedStartupSpool) close() {
	s.mu.Lock()
	if s.state != stateClosed {
		s.state = stateClosed
		s.cond.Broadcast()
		s.mu.Unlock()
		if s.adapter != nil {
			dc := s.adapter.GetDiagnosticContext(s.sessionID)
			s.adapter.Logger.Info().
				Str("session_id", dc.SessionID).
				Str("generation_id", dc.GenerationID).
				Str("reason", dc.Reason).
				Int64("elapsed_since_stop_ms", dc.ElapsedSinceStopMs).
				Msg("avsync_spool_closing")
		}
		return
	}
	s.mu.Unlock()
}

func (s *boundedStartupSpool) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.totalBytes)
	off := 0
	for _, ch := range s.chunks {
		copy(out[off:], ch)
		off += len(ch)
	}
	return out
}

func (s *boundedStartupSpool) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.chunks) == 0 && s.err == nil && s.state != stateClosed {
		s.cond.Wait()
	}
	if s.state == stateClosed {
		return 0, io.ErrClosedPipe
	}
	if len(s.chunks) == 0 && s.err != nil {
		if s.err == errSpoolLimit {
			// Limit reached during buffering, but reader now needs data to drain.
			// Clear errSpoolLimit once drained so the producer loop can resume reading.
			s.err = nil
			if s.state == stateBuffering {
				s.state = stateDecided
			}
			s.cond.Broadcast()
			for len(s.chunks) == 0 && s.err == nil && s.state != stateClosed {
				s.cond.Wait()
			}
			if s.state == stateClosed {
				return 0, io.ErrClosedPipe
			}
			if len(s.chunks) == 0 && s.err != nil {
				return 0, s.err
			}
		} else {
			return 0, s.err
		}
	}

	first := s.chunks[0]
	n := copy(p, first[s.readOffset:])
	s.readOffset += n
	s.totalBytes -= n

	if s.readOffset >= len(first) {
		s.chunks[0] = nil // allow GC
		s.chunks = s.chunks[1:]
		s.readOffset = 0
	}

	s.cond.Broadcast() // wake up producer if it was blocked waiting on maxBytes
	return n, nil
}

// prepareAvsyncPipe opens the relay over a single connection, peeks the head,
// measures the orphan, and returns a stdin reader (buffered head + live tail)
// plus the measured orphan. ok=false means measurement failed, but stdin IS STILL
// returned so data is not lost (fallback to untrimmed stream).
func (a *LocalAdapter) prepareAvsyncPipe(ctx context.Context, rawURL, sessionID string) (float64, io.Reader, bool) {
	if !isHTTPInputURL(rawURL) {
		return 0, nil, false
	}
	start := time.Now()
	req, _, err := buildAuthenticatedRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return 0, nil, false
	}
	req.Header.Set("Icy-MetaData", "1")

	if a.httpClient == nil {
		return 0, nil, false
	}
	streamClient := *a.httpClient
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return 0, nil, false
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return 0, nil, false
	}

	metrics.IncActiveEnigma2Connections("spool")
	spool := newBoundedStartupSpool(resp.Body, sessionID, a)
	go spool.run(avsyncPeekMaxBytes)

	// Close the spool and relay body when the session context ends
	go func() {
		<-ctx.Done()
		spool.close()
		_ = resp.Body.Close()
		dc := a.GetDiagnosticContext(sessionID)
		a.Logger.Info().
			Str("session_id", dc.SessionID).
			Str("generation_id", dc.GenerationID).
			Str("reason", dc.Reason).
			Int64("elapsed_since_stop_ms", dc.ElapsedSinceStopMs).
			Msg("http_body_closed")
		metrics.DecActiveEnigma2Connections("spool")
	}()

	peekCtx, cancel := context.WithTimeout(ctx, avsyncPeekTimeout)
	defer cancel()

	orphan, ok := a.peekMeasure(peekCtx, spool)
	spool.markDecided() // transition from stateBuffering to stateDecided

	if !ok {
		a.Logger.Warn().
			Str("session_id", sessionID).
			Str("startup_phase", "avsync_atrim_skipped").
			Int64("peek_ms", time.Since(start).Milliseconds()).
			Msg("live-copy avsync: orphan not measurable, falling back to untrimmed spool")
		return 0, spool, false
	}

	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "avsync_atrim_armed").
		Float64("orphan_seconds", orphan).
		Int("peek_bytes", len(spool.snapshot())).
		Int64("peek_ms", time.Since(start).Milliseconds()).
		Msg("live-copy avsync atrim armed")
	return orphan, spool, true
}

// peekMeasure repeatedly probes snapshots from the continuous spool.
func (a *LocalAdapter) peekMeasure(ctx context.Context, spool *boundedStartupSpool) (float64, bool) {
	tmp, err := os.CreateTemp("", "xg2g-avsync-head-*.ts")
	if err != nil {
		return 0, false
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpName) }()

	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			spool.mu.Lock()
			spool.cond.Broadcast()
			spool.mu.Unlock()
		case <-stopWatch:
		}
	}()

	nextProbe := avsyncPeekFirstProbe
	for {
		if ctx.Err() != nil {
			return 0, false
		}

		spool.mu.Lock()
		for spool.totalBytes < nextProbe && spool.err == nil && ctx.Err() == nil && spool.state != stateClosed {
			spool.cond.Wait()
		}

		snapshot := make([]byte, spool.totalBytes)
		off := 0
		for _, ch := range spool.chunks {
			copy(snapshot[off:], ch)
			off += len(ch)
		}
		spoolErr := spool.err
		spool.mu.Unlock()

		if ctx.Err() != nil {
			return 0, false
		}

		if len(snapshot) >= nextProbe {
			if orphan, ok := a.measureOrphan(ctx, tmpName, snapshot); ok {
				return orphan, true
			}
			nextProbe = len(snapshot) + avsyncPeekProbeStep
		}

		if spoolErr != nil {
			// Stream ended or spool limit reached before we could measure it
			if len(snapshot) > 0 {
				if orphan, ok := a.measureOrphan(ctx, tmpName, snapshot); ok {
					return orphan, true
				}
			}
			return 0, false
		}
	}
}

// measureOrphan writes the buffered head to tmpName and derives the orphan as the
// first video keyframe PTS minus the first audio PTS.
func (a *LocalAdapter) measureOrphan(ctx context.Context, tmpName string, head []byte) (float64, bool) {
	if err := os.WriteFile(tmpName, head, 0o600); err != nil {
		return 0, false
	}
	vk, ok := a.firstPacketPTS(ctx, tmpName, "v:0", true)
	if !ok {
		return 0, false
	}
	a0, ok := a.firstPacketPTS(ctx, tmpName, "a:0", false)
	if !ok {
		return 0, false
	}
	orphan := vk - a0
	if orphan < avsyncMinOrphanSec || orphan > avsyncMaxOrphanSec {
		return 0, false
	}
	return orphan, true
}

// firstPacketPTS returns the PTS of the first packet of the selected stream
// (optionally the first keyframe packet) from a probed file.
func (a *LocalAdapter) firstPacketPTS(ctx context.Context, path, stream string, keyframeOnly bool) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ffprobeBin := strings.TrimSpace(a.FFprobeBin)
	if ffprobeBin == "" {
		ffprobeBin = "ffprobe"
	}
	// #nosec G204 - FFprobeBin is trusted from config; all other args are fixed
	// internal literals and the probed path is a server-created temp file.
	out, err := exec.CommandContext(ctx, ffprobeBin,
		"-v", "error",
		"-select_streams", stream,
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		path,
	).Output()
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if keyframeOnly && !strings.Contains(line, "K") {
			continue
		}
		fields := strings.SplitN(line, ",", 2)
		pts, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		if err != nil {
			continue
		}
		return pts, true
	}
	return 0, false
}

// transformArgsForAvsyncPipeMode rewrites a normal live argv for stdin
// (pipe:0) input: the HTTP input URL is replaced by pipe:0, HTTP-only input
// options are dropped (FFmpeg rejects/ignores them for a pipe), and — when
// insertTrim is set — the orphan correction is applied. For video-copy that is
// a leading-audio atrim before the audio encoder so copied video and
// transcoded audio share an origin. For video-transcode (transcodeVideo) it is
// an input-side "-ss <orphan>" instead: both tracks then start together at the
// first decodable keyframe, which also discards the salvage frame the decoder
// otherwise emits from the pre-keyframe garbage. Pure function: unit-tested
// independently of the live peek.
func transformArgsForAvsyncPipeMode(args []string, orphan float64, insertTrim bool, transcodeVideo bool) []string {
	stripValueFlag := map[string]bool{
		"-headers":                    true,
		"-user_agent":                 true,
		"-protocol_whitelist":         true,
		"-reconnect":                  true,
		"-reconnect_at_eof":           true,
		"-reconnect_streamed":         true,
		"-reconnect_delay_max":        true,
		"-reconnect_on_network_error": true,
		"-reconnect_on_http_error":    true,
	}
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "-i" && i+1 < len(args) {
			if insertTrim && transcodeVideo {
				out = append(out, "-ss", fmt.Sprintf("%.3f", orphan))
			}
			out = append(out, "-i", "pipe:0")
			i++ // skip the URL value
			continue
		}
		if stripValueFlag[tok] && i+1 < len(args) {
			i++ // skip the option value
			continue
		}
		if insertTrim && !transcodeVideo && tok == "-c:a" && i+1 < len(args) && args[i+1] != "copy" {
			out = append(out, "-af", fmt.Sprintf("aresample=async=1,atrim=start=%.3f", orphan))
		}
		out = append(out, tok)
	}
	return out
}
