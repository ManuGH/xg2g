// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package llhls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tracker follows one live session's HLS directory: it mirrors the segment
// list FFmpeg maintains in index.m3u8 and indexes the CMAF fragments of the
// segment currently being written, so the playlist handler can advertise
// EXT-X-PART entries and answer blocking-reload requests.
type Tracker struct {
	dir          string
	partTargetMs int

	mu           sync.Mutex
	cond         *sync.Cond
	base         basePlaylist
	current      openSegment
	closed       bool
	everHadParts bool
}

type basePlaylist struct {
	raw       string // ffmpeg playlist verbatim
	mediaSeq  int
	segments  []string // segment filenames in playlist order
	targetDur int
}

type openSegment struct {
	name       string
	parts      []Fragment
	scanOffset int64
}

var liveSegmentNameRe = regexp.MustCompile(`^seg_(\d+)\.m4s$`)

// NewTracker starts following dir. It repairs the leaked init once (see
// RepairLeakedInit) before advertising anything, then polls the playlist
// and the growing segment until ctx is done.
func NewTracker(ctx context.Context, dir string, partTargetMs int) *Tracker {
	t := &Tracker{dir: dir, partTargetMs: partTargetMs}
	t.cond = sync.NewCond(&t.mu)
	go t.run(ctx)
	return t
}

func (t *Tracker) run(ctx context.Context) {
	defer func() {
		t.mu.Lock()
		t.closed = true
		t.cond.Broadcast()
		t.mu.Unlock()
	}()

	// Wake blocked playlist readers periodically so their deadline checks run.
	wake := time.NewTicker(250 * time.Millisecond)
	defer wake.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake.C:
				t.cond.Broadcast()
			}
		}
	}()

	repaired := false

	// Start with slow polling (1s) for standard HLS: FFmpeg's hls muxer
	// writes each fMP4 segment to disk only on completion, so a fast scan
	// would never find fragments — only waste CPU and disk I/O. Once the
	// tracker actually indexes a real CMAF fragment (via the ingest server
	// or another streaming source), the poll speeds up to 100ms.
	const slowInterval = 1 * time.Second
	const fastInterval = 100 * time.Millisecond
	poll := time.NewTicker(slowInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		}

		base, err := readBasePlaylist(filepath.Join(t.dir, "index.m3u8"))
		if err != nil {
			continue // playlist not written yet
		}

		if !repaired && len(base.segments) > 0 {
			// One-time init repair, before any LL playlist is served.
			_, rerr := RepairLeakedInit(
				filepath.Join(t.dir, "init.mp4"),
				filepath.Join(t.dir, base.segments[0]),
			)
			if rerr == nil {
				repaired = true
			}
		}
		if !repaired {
			continue
		}

		currentName, ok := nextSegmentName(base.segments)
		changed := false

		t.mu.Lock()
		if base.raw != t.base.raw {
			t.base = base
			changed = true
		}
		if ok && currentName != t.current.name {
			t.current = openSegment{name: currentName}
			changed = true
		}
		cur := t.current
		t.mu.Unlock()

		if ok {
			if frags, next, scanned := scanOpenSegment(filepath.Join(t.dir, cur.name), cur.scanOffset); scanned && len(frags) > 0 {
				t.mu.Lock()
				if t.current.name == cur.name {
					t.current.parts = append(t.current.parts, frags...)
					t.current.scanOffset = next
					if !t.everHadParts {
						t.everHadParts = true
						poll.Reset(fastInterval)
					}
					changed = true
				}
				t.mu.Unlock()
			}
		}

		if changed {
			t.mu.Lock()
			t.cond.Broadcast()
			t.mu.Unlock()
		}
	}
}

func scanOpenSegment(path string, from int64) ([]Fragment, int64, bool) {
	f, err := os.Open(path) // #nosec G304 -- session-confined artifact path
	if err != nil {
		return nil, from, false
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, from, false
	}
	frags, next, err := ScanFragments(f, st.Size(), from)
	if err != nil {
		return nil, from, false
	}
	return frags, next, true
}

// nextSegmentName derives the filename FFmpeg is currently writing: the
// numerically next segment after the newest playlist entry.
func nextSegmentName(segments []string) (string, bool) {
	if len(segments) == 0 {
		return "", false
	}
	m := liveSegmentNameRe.FindStringSubmatch(segments[len(segments)-1])
	if m == nil {
		return "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("seg_%0*d.m4s", len(m[1]), n+1), true
}

func readBasePlaylist(path string) (basePlaylist, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- session-confined artifact path
	if err != nil {
		return basePlaylist{}, err
	}
	base := basePlaylist{raw: string(data)}
	for _, line := range strings.Split(base.raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			base.mediaSeq, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			base.targetDur, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))
		case line != "" && !strings.HasPrefix(line, "#"):
			base.segments = append(base.segments, line)
		}
	}
	if len(base.segments) == 0 {
		return basePlaylist{}, fmt.Errorf("playlist has no segments yet")
	}
	return base, nil
}

// HasParts reports whether the tracker has ever indexed a CMAF fragment in
// this session. Until that happens the LL playlist must not be served:
// advertising CAN-BLOCK-RELOAD and PART-HOLD-BACK without ever delivering
// EXT-X-PART entries locks native players into full-segment blocking
// reloads at a part-sized hold-back, which drains their buffer into
// periodic stalls. FFmpeg's hls muxer buffers each fMP4 segment in memory
// and writes it only on completion, so parts appear on disk only when the
// segment pipeline actually streams fragments (e.g. via the ingest server).
func (t *Tracker) HasParts() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.everHadParts
}

// snapshot returns a consistent view for rendering.
func (t *Tracker) snapshot() (basePlaylist, openSegment, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.current
	if len(cur.parts) > 0 {
		cur.parts = append([]Fragment(nil), cur.parts...)
	}
	return t.base, cur, t.closed
}

// AwaitAndRender blocks per the LL-HLS blocking-reload contract until the
// playlist contains media sequence number wantMsn (and, when wantPart >= 0,
// part index wantPart within it), then renders the LL playlist. A negative
// wantMsn renders immediately. The wait is bounded by deadline.
func (t *Tracker) AwaitAndRender(ctx context.Context, wantMsn, wantPart int, deadline time.Time) (string, error) {
	t.mu.Lock()
	for wantMsn >= 0 && !t.closed {
		if t.satisfiedLocked(wantMsn, wantPart) {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		t.cond.Wait()
	}
	t.mu.Unlock()

	base, cur, closed := t.snapshot()
	if closed && base.raw == "" {
		return "", fmt.Errorf("session closed")
	}
	if base.raw == "" {
		return "", fmt.Errorf("playlist not ready")
	}
	return renderLLPlaylist(base, cur, t.partTargetMs), nil
}

// satisfiedLocked implements the _HLS_msn/_HLS_part readiness rule.
func (t *Tracker) satisfiedLocked(wantMsn, wantPart int) bool {
	if t.base.raw == "" {
		return false
	}
	lastFull := t.base.mediaSeq + len(t.base.segments) - 1
	if wantMsn <= lastFull {
		return true
	}
	currentMsn := t.base.mediaSeq + len(t.base.segments)
	if wantMsn > currentMsn {
		// Spec: a request too far ahead must not block forever; treat the
		// next-but-one sequence as satisfiable only once it starts.
		return false
	}
	if wantMsn == currentMsn {
		if wantPart < 0 {
			// Full-segment request: satisfied once the segment completes,
			// which flips it into base.segments (handled above).
			return false
		}
		return len(t.current.parts) > wantPart
	}
	return false
}
