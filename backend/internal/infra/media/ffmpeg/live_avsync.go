package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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
	avsyncPeekReadChunk  = 64 << 10  // relay read granularity
	avsyncPeekFirstProbe = 64 << 10 // start probing once this much head is buffered
	avsyncPeekProbeStep  = 64 << 10 // re-probe after each additional step
	avsyncPeekMaxBytes   = 6 << 20   // give up past here -> fall back to direct path

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
	if spec.Profile.TranscodeVideo {
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

// prepareAvsyncPipe opens the relay over a single connection, peeks the head,
// measures the orphan, and returns a stdin reader (buffered head + live tail)
// plus the measured orphan. ok=false means the caller must use the normal path.
func (a *LocalAdapter) prepareAvsyncPipe(ctx context.Context, rawURL, sessionID string) (float64, io.Reader, bool) {
	if !isHTTPInputURL(rawURL) {
		return 0, nil, false
	}
	start := time.Now()
	req, _, err := buildAuthenticatedRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return 0, nil, false
	}
	// Default user-agent (Go/Lavf-style) - the OSCam stream-relay honors the Host
	// header for non-"VLC" UAs, which is all the peek needs. No dependency on any
	// configurable UA override here, so this stays self-contained.
	req.Header.Set("Icy-MetaData", "1")

	// The shared adapter client carries a short overall Timeout meant for probes.
	// http.Client.Timeout also bounds the body read, which would kill this
	// long-lived stream mid-session (observed: ffmpeg dies ~10s in with "context
	// deadline exceeded while reading body"). Reuse its Transport (the CIDR
	// allowlist dialer) but drop the overall timeout - the session context, plus
	// the body-close goroutine below, govern the stream's lifetime instead.
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

	peekCtx, cancel := context.WithTimeout(ctx, avsyncPeekTimeout)
	defer cancel()

	// Close body on peek timeout to unblock any in-flight ReadFull; without this
	// a stalled relay hangs the peek for the full timeout even though the context
	// is cancelled. Use a channel to only apply during the peek phase - after a
	// successful peek the session-context closer takes over.
	peekDone := make(chan struct{})
	go func() {
		select {
		case <-peekCtx.Done():
			_ = resp.Body.Close()
		case <-peekDone:
		}
	}()

	head, orphan, ok := a.peekMeasure(peekCtx, resp.Body)
	close(peekDone) // peek phase done — close-on-peek-timeout goroutine exits
	if !ok {
		_ = resp.Body.Close()
		a.Logger.Info().
			Str("session_id", sessionID).
			Str("startup_phase", "avsync_atrim_skipped").
			Int64("peek_ms", time.Since(start).Milliseconds()).
			Msg("live-copy avsync: orphan not measurable, using direct copy path")
		return 0, nil, false
	}
	// Close the relay body when the session context ends (CommandContext also
	// kills ffmpeg), so the connection never outlives the process.
	go func() {
		<-ctx.Done()
		_ = resp.Body.Close()
	}()
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "avsync_atrim_armed").
		Float64("orphan_seconds", orphan).
		Int("peek_bytes", len(head)).
		Int64("peek_ms", time.Since(start).Milliseconds()).
		Msg("live-copy avsync atrim armed")
	return orphan, io.MultiReader(bytes.NewReader(head), resp.Body), true
}

// peekMeasure reads the head in growing checkpoints, measuring the orphan as soon
// as the first video keyframe is buffered.
func (a *LocalAdapter) peekMeasure(ctx context.Context, body io.Reader) ([]byte, float64, bool) {
	tmp, err := os.CreateTemp("", "xg2g-avsync-head-*.ts")
	if err != nil {
		return nil, 0, false
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpName) }()

	buf := make([]byte, 0, avsyncPeekMaxBytes)
	chunk := make([]byte, avsyncPeekReadChunk)
	nextProbe := avsyncPeekFirstProbe
	for len(buf) < avsyncPeekMaxBytes {
		if ctx.Err() != nil {
			return nil, 0, false
		}
		n, rerr := io.ReadFull(body, chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if len(buf) >= nextProbe {
			if orphan, ok := a.measureOrphan(ctx, tmpName, buf); ok {
				return buf, orphan, true
			}
			nextProbe = len(buf) + avsyncPeekProbeStep
			// Probe failed at this buffer size and we hit EOF — no more data
			// arriving, so don't re-probe unchanged buf.
			if rerr != nil {
				return nil, 0, false
			}
		} else if rerr != nil { // EOF / short read before any probe threshold — try once
			if orphan, ok := a.measureOrphan(ctx, tmpName, buf); ok {
				return buf, orphan, true
			}
			return nil, 0, false
		}
	}
	return nil, 0, false
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
