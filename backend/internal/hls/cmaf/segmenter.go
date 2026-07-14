// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package cmaf turns a single fragmented-MP4 stream (FFmpeg -f mp4 with
// frag_keyframe+frag_duration) into a live HLS session directory: init.mp4,
// growing seg_N.m4s files that receive each CMAF fragment the moment FFmpeg
// flushes it, and an index.m3u8 published atomically per completed segment.
//
// This exists because FFmpeg's hls muxer buffers every fMP4 segment in
// memory and writes it only on completion, so a file-scanning LL-HLS
// packager (internal/hls/llhls) never sees mid-segment fragments. With this
// segmenter the open segment grows on disk fragment by fragment, and the
// existing llhls Tracker advertises EXT-X-PART byte ranges from it without
// modification.
package cmaf

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/hls/llhls"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

// maxBoxSize bounds a single top-level box. 500ms of video at even absurd
// bitrates stays far below this; anything larger is a corrupt stream.
const maxBoxSize = 64 << 20

// fallbackTimescale is used when the moov cannot be parsed. 90kHz is the
// MPEG transport convention and keeps duration math sane until real
// timescale data is available.
const fallbackTimescale = 90000

// Config parameterizes one segmenter run (one live session).
type Config struct {
	// Dir is the session directory artifacts are written into.
	Dir string
	// TargetDurationSec is the nominal segment duration; rotation happens at
	// the first independent fragment at or past this boundary.
	TargetDurationSec int
	ListSize          int
	// Now returns the wall clock for EXT-X-PROGRAM-DATE-TIME stamps.
	// Defaults to time.Now when nil.
	Now func() time.Time

	ShadowPublisher *store.ShadowPublisher
	StreamID        store.StreamID

	Logger zerolog.Logger
}

// Run consumes the fMP4 stream until EOF or ctx cancellation. It always
// drains r so the producing FFmpeg process can never block on a full pipe,
// even after a parse error.
func Run(ctx context.Context, r io.Reader, cfg Config) error {
	defer func() {
		_, _ = io.Copy(io.Discard, r)
		if cfg.ShadowPublisher != nil {
			_ = cfg.ShadowPublisher.Close(context.Background())
		}
	}()
	err := run(ctx, r, cfg)
	if err != nil && ctx.Err() == nil {
		cfg.Logger.Error().Err(err).Str("dir", cfg.Dir).Msg("cmaf segmenter failed")
	}
	return err
}

func run(ctx context.Context, r io.Reader, cfg Config) error {
	if cfg.TargetDurationSec <= 0 {
		return fmt.Errorf("invalid target duration %d", cfg.TargetDurationSec)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return fmt.Errorf("session dir: %w", err)
	}

	br := bufio.NewReaderSize(r, 1<<20)

	// --- init phase: everything up to the first fragment box is the init ---
	var initBytes []byte
	var pending *rawBox
	for {
		b, err := readStreamBox(br)
		if err != nil {
			return fmt.Errorf("read init boxes: %w", err)
		}
		if isFragmentBoxType(b.typ) {
			pending = b
			break
		}
		initBytes = append(initBytes, b.data...)
	}
	if len(initBytes) == 0 {
		return fmt.Errorf("stream started with %q before any init box", pending.typ)
	}
	videoTrackID, timescale := parseMoovVideoTrack(initBytes)
	if timescale <= 0 {
		timescale = fallbackTimescale
	}
	if err := atomicWriteFile(filepath.Join(cfg.Dir, "init.mp4"), initBytes); err != nil {
		return fmt.Errorf("write init: %w", err)
	}
	if cfg.ShadowPublisher != nil {
		cfg.ShadowPublisher.Publish(ctx, cfg.StreamID, store.Object{
			Name:        "init.mp4",
			Kind:        store.ObjectInit,
			ContentType: "video/mp4",
			Data:        initBytes,
			PublishedAt: now(),
		})
	}

	cfg.Logger.Info().
		Uint32("video_track_id", videoTrackID).
		Uint32("timescale", timescale).
		Int("init_bytes", len(initBytes)).
		Msg("cmaf init published")

	pl, err := newPlaylistWriter(ctx, cfg.Dir, cfg.TargetDurationSec, cfg.ListSize, cfg.ShadowPublisher, cfg.StreamID)
	if err != nil {
		return fmt.Errorf("playlist init: %w", err)
	}

	seg := &segmentWriter{dir: cfg.Dir, index: pl.nextSegmentIndex}
	defer func() { _ = seg.closeFile() }()

	targetTicks := uint64(cfg.TargetDurationSec) * uint64(timescale)
	var lastDts uint64
	var haveDts bool

	// PROGRAM-DATE-TIME anchor: wall clock is sampled once at the first
	// dts-carrying fragment; every segment's PDT is then derived from its
	// media timeline offset against that anchor. Stamping now() per segment
	// instead produces jittering deltas (segments are flushed in bursts) and
	// duplicate PDTs on the startup burst, which breaks AVPlayer's live-edge
	// math.
	var anchorWall time.Time
	var anchorDts uint64
	var haveAnchor bool
	segmentWall := func(startDts uint64) time.Time {
		if !haveAnchor || startDts < anchorDts {
			return now()
		}
		offset := float64(startDts-anchorDts) / float64(timescale)
		return anchorWall.Add(time.Duration(offset * float64(time.Second)))
	}

	for ctx.Err() == nil {
		var frag []byte
		frag, pending, err = readFragment(br, pending)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read fragment: %w", err)
		}

		dts, dtsOK := fragmentVideoDts(frag, videoTrackID)
		independent := fragmentIndependent(frag)
		if dtsOK {
			lastDts, haveDts = dts, true
			if !haveAnchor {
				anchorWall, anchorDts, haveAnchor = now(), dts, true
			}
		}

		if seg.open() && independent && dtsOK && dts >= seg.startDts+targetTicks {
			dur := float64(dts-seg.startDts) / float64(timescale)
			if err := seg.rotate(); err != nil {
				return err
			}
			lastClosed := seg.lastClosedName()
			if cfg.ShadowPublisher != nil {
				segPath := filepath.Join(cfg.Dir, lastClosed)
				if data, err := os.ReadFile(segPath); err == nil {
					cfg.ShadowPublisher.Publish(ctx, cfg.StreamID, store.Object{
						Name:        lastClosed,
						Kind:        store.ObjectSegment,
						ContentType: "video/iso.segment",
						Data:        data,
						PublishedAt: now(),
					})
				} else {
					cfg.Logger.Warn().Err(err).Str("segment", lastClosed).Msg("failed to read segment for shadow store")
				}
			}
			if err := pl.appendSegment(lastClosed, dur, seg.lastStartWall); err != nil {
				return err
			}
		}
		if !seg.open() {
			startDts := lastDts
			if !haveDts {
				startDts = 0
			}
			if err := seg.start(startDts, segmentWall(startDts)); err != nil {
				return err
			}
		}
		if err := seg.append(frag); err != nil {
			return err
		}
	}

	// Final segment: duration from the last seen dts plus one nominal
	// fragment is unknowable; use what we measured and let ENDLIST close
	// the presentation.
	if seg.open() {
		dur := float64(0)
		if haveDts && lastDts > seg.startDts {
			dur = float64(lastDts-seg.startDts) / float64(timescale)
		}
		if dur <= 0 {
			dur = float64(cfg.TargetDurationSec)
		}
		if err := seg.rotate(); err != nil {
			return err
		}
		if err := pl.appendSegment(seg.lastClosedName(), dur, seg.lastStartWall); err != nil {
			return err
		}
	}
	if ctx.Err() == nil {
		if err := pl.finalize(); err != nil {
			return err
		}
	}
	return nil
}

// --- stream box reading ---

type rawBox struct {
	typ  string
	data []byte
}

func isFragmentBoxType(typ string) bool {
	switch typ {
	case "styp", "sidx", "moof", "prft":
		return true
	}
	return false
}

func readStreamBox(br *bufio.Reader) (*rawBox, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, err
	}
	size := uint64(binary.BigEndian.Uint32(hdr[:4]))
	typ := string(hdr[4:8])
	data := append([]byte{}, hdr[:]...)
	if size == 1 {
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, fmt.Errorf("largesize of %q: %w", typ, err)
		}
		size = binary.BigEndian.Uint64(ext[:])
		data = append(data, ext[:]...)
	}
	if size == 0 {
		return nil, fmt.Errorf("box %q extends to EOF (unsupported in live stream)", typ)
	}
	if size < uint64(len(data)) || size > maxBoxSize {
		return nil, fmt.Errorf("box %q has invalid size %d", typ, size)
	}
	payload := make([]byte, size-uint64(len(data)))
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, fmt.Errorf("payload of %q: %w", typ, err)
	}
	return &rawBox{typ: typ, data: append(data, payload...)}, nil
}

// readFragment assembles one CMAF chunk: optional styp/sidx/prft prefix
// boxes, a moof, and its mdat. pending carries the first box of the next
// fragment across calls.
func readFragment(br *bufio.Reader, pending *rawBox) (frag []byte, nextPending *rawBox, err error) {
	sawMoof := false
	for {
		var b *rawBox
		if pending != nil {
			b, pending = pending, nil
		} else {
			b, err = readStreamBox(br)
			if err != nil {
				if err == io.EOF && sawMoof {
					// stream ended between moof and mdat: drop the torso
					return nil, nil, io.EOF
				}
				return nil, nil, err
			}
		}
		switch {
		case b.typ == "moof":
			sawMoof = true
			frag = append(frag, b.data...)
		case b.typ == "mdat":
			if !sawMoof {
				return nil, nil, fmt.Errorf("mdat before moof")
			}
			frag = append(frag, b.data...)
			return frag, nil, nil
		case isFragmentBoxType(b.typ):
			frag = append(frag, b.data...)
		default:
			return nil, nil, fmt.Errorf("unexpected box %q inside fragment stream", b.typ)
		}
	}
}

// --- fragment inspection ---

// fragmentIndependent reuses the llhls box scanner: a fragment whose first
// video sample is a sync sample can start a segment.
func fragmentIndependent(frag []byte) bool {
	frags, _, err := llhls.ScanFragments(byteReaderAt(frag), int64(len(frag)), 0)
	if err != nil || len(frags) == 0 {
		return false
	}
	return frags[0].Independent
}

// fragmentVideoDts extracts the tfdt baseMediaDecodeTime of the traf
// belonging to videoTrackID (or the first traf when the track is unknown).
func fragmentVideoDts(frag []byte, videoTrackID uint32) (uint64, bool) {
	moof, ok := findTopLevelBox(frag, "moof")
	if !ok {
		return 0, false
	}
	var firstDts uint64
	var haveFirst bool
	for _, traf := range childBoxes(moof, "traf") {
		var trackID uint32
		var dts uint64
		var haveDts bool
		if tfhd, ok := findChildBox(traf, "tfhd"); ok && len(tfhd) >= 16 {
			trackID = binary.BigEndian.Uint32(tfhd[12:16])
		}
		if tfdt, ok := findChildBox(traf, "tfdt"); ok && len(tfdt) >= 12 {
			version := tfdt[8]
			if version == 1 && len(tfdt) >= 20 {
				dts, haveDts = binary.BigEndian.Uint64(tfdt[12:20]), true
			} else if version == 0 && len(tfdt) >= 16 {
				dts, haveDts = uint64(binary.BigEndian.Uint32(tfdt[12:16])), true
			}
		}
		if !haveDts {
			continue
		}
		if !haveFirst {
			firstDts, haveFirst = dts, true
		}
		if videoTrackID != 0 && trackID == videoTrackID {
			return dts, true
		}
	}
	return firstDts, haveFirst
}

// --- moov parsing (video track id + timescale) ---

// parseMoovVideoTrack walks moov/trak/{tkhd,mdia/{mdhd,hdlr}} and returns
// the track id and mdhd timescale of the first video handler track.
// Zero values mean "not found".
func parseMoovVideoTrack(init []byte) (trackID, timescale uint32) {
	moov, ok := findTopLevelBox(init, "moov")
	if !ok {
		return 0, 0
	}
	var firstID, firstTs uint32
	for _, trak := range childBoxes(moov, "trak") {
		var id, ts uint32
		var isVideo bool
		if tkhd, ok := findChildBox(trak, "tkhd"); ok && len(tkhd) >= 12 {
			switch tkhd[8] {
			case 0:
				if len(tkhd) >= 24 {
					id = binary.BigEndian.Uint32(tkhd[20:24])
				}
			case 1:
				if len(tkhd) >= 32 {
					id = binary.BigEndian.Uint32(tkhd[28:32])
				}
			}
		}
		if mdia, ok := findChildBox(trak, "mdia"); ok {
			if mdhd, ok := findChildBox(mdia, "mdhd"); ok && len(mdhd) >= 12 {
				switch mdhd[8] {
				case 0:
					if len(mdhd) >= 24 {
						ts = binary.BigEndian.Uint32(mdhd[20:24])
					}
				case 1:
					if len(mdhd) >= 32 {
						ts = binary.BigEndian.Uint32(mdhd[28:32])
					}
				}
			}
			if hdlr, ok := findChildBox(mdia, "hdlr"); ok && len(hdlr) >= 24 {
				isVideo = string(hdlr[16:20]) == "vide"
			}
		}
		if firstID == 0 {
			firstID, firstTs = id, ts
		}
		if isVideo {
			return id, ts
		}
	}
	return firstID, firstTs
}

// findTopLevelBox scans concatenated boxes for the first of the given type
// and returns its full bytes (header included).
func findTopLevelBox(data []byte, typ string) ([]byte, bool) {
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		if size < 8 || len(data)-off < size {
			return nil, false
		}
		if string(data[off+4:off+8]) == typ {
			return data[off : off+size], true
		}
		off += size
	}
	return nil, false
}

// childBoxes returns all direct children of the given type inside a
// container box (data includes the container's own 8-byte header).
func childBoxes(container []byte, typ string) [][]byte {
	var out [][]byte
	for off := 8; off+8 <= len(container); {
		size := int(binary.BigEndian.Uint32(container[off : off+4]))
		if size < 8 || len(container)-off < size {
			break
		}
		if string(container[off+4:off+8]) == typ {
			out = append(out, container[off:off+size])
		}
		off += size
	}
	return out
}

func findChildBox(container []byte, typ string) ([]byte, bool) {
	boxes := childBoxes(container, typ)
	if len(boxes) == 0 {
		return nil, false
	}
	return boxes[0], true
}

type byteReaderAt []byte

func (r byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// --- segment file management ---

type segmentWriter struct {
	dir   string
	index int

	file          *os.File
	startDts      uint64
	startWall     time.Time
	lastStartWall time.Time
	closedIndex   int
}

func (s *segmentWriter) open() bool { return s.file != nil }

func segmentName(index int) string { return fmt.Sprintf("seg_%06d.m4s", index) }

func (s *segmentWriter) start(startDts uint64, wall time.Time) error {
	f, err := os.OpenFile(filepath.Join(s.dir, segmentName(s.index)), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- session-confined path
	if err != nil {
		return fmt.Errorf("open segment %d: %w", s.index, err)
	}
	s.file = f
	s.startDts = startDts
	s.startWall = wall
	return nil
}

func (s *segmentWriter) append(frag []byte) error {
	if s.file == nil {
		return fmt.Errorf("append to closed segment")
	}
	if _, err := s.file.Write(frag); err != nil {
		return fmt.Errorf("write segment %d: %w", s.index, err)
	}
	return nil
}

// rotate closes the current segment file and advances the index. The
// caller records the playlist entry via lastClosedName/lastStartWall.
func (s *segmentWriter) rotate() error {
	if err := s.closeFile(); err != nil {
		return err
	}
	s.closedIndex = s.index
	s.lastStartWall = s.startWall
	s.index++
	return nil
}

func (s *segmentWriter) lastClosedName() string { return segmentName(s.closedIndex) }

func (s *segmentWriter) closeFile() error {
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	if err != nil {
		return fmt.Errorf("close segment %d: %w", s.index, err)
	}
	return nil
}

// --- playlist ---

var segNameRe = regexp.MustCompile(`^seg_(\d+)\.m4s$`)

type playlistWriter struct {
	path             string
	targetDuration   int
	listSize         int
	mediaLines       []string
	nextSegmentIndex int
	mediaSequence    int
	discontSequence  int
	// maxDuration tracks the longest EXTINF ever published. The HLS spec
	// requires EXT-X-TARGETDURATION >= every segment duration (rounded), so
	// an over-long segment (e.g. a startup GOP overshoot) bumps the published
	// target instead of violating the invariant and confusing native players.
	maxDuration float64
	shadow      *store.ShadowPublisher
	streamID    store.StreamID
	ctx         context.Context
}

// newPlaylistWriter continues an existing session playlist when one is on
// disk (worker restart into the same session dir): prior media entries are
// preserved, a DISCONTINUITY separates the new encode, and numbering
// resumes after the highest existing segment.
func newPlaylistWriter(ctx context.Context, dir string, targetDuration int, listSize int, shadow *store.ShadowPublisher, streamID store.StreamID) (*playlistWriter, error) {
	pl := &playlistWriter{
		path:           filepath.Join(dir, "index.m3u8"),
		targetDuration: targetDuration,
		listSize:       listSize,
		shadow:         shadow,
		streamID:       streamID,
		ctx:            ctx,
	}
	raw, err := os.ReadFile(pl.path) // #nosec G304 -- session-confined path
	if err != nil {
		if os.IsNotExist(err) {
			return pl, nil
		}
		return nil, err
	}
	maxIdx := -1
	inMedia := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "#EXT-X-ENDLIST" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")); err == nil {
				pl.mediaSequence = n
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:") {
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-DISCONTINUITY-SEQUENCE:")); err == nil {
				pl.discontSequence = n
			}
			continue
		}
		if m := segNameRe.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxIdx {
				maxIdx = n
			}
			inMedia = true
			pl.mediaLines = append(pl.mediaLines, line)
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") || strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:") || strings.HasPrefix(line, "#EXT-X-DISCONTINUITY") {
			if raw, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				// RFC 8216: #EXTINF:<duration>,[<title>] — strip the optional title.
				if idx := strings.IndexByte(raw, ','); idx >= 0 {
					raw = raw[:idx]
				}
				raw = strings.TrimSpace(raw)
				if d, err := strconv.ParseFloat(raw, 64); err == nil && d > pl.maxDuration {
					pl.maxDuration = d
				}
			}
			inMedia = true
			pl.mediaLines = append(pl.mediaLines, line)
			continue
		}
		_ = inMedia // header lines are regenerated
	}
	if maxIdx >= 0 {
		pl.nextSegmentIndex = maxIdx + 1
		pl.mediaLines = append(pl.mediaLines, "#EXT-X-DISCONTINUITY")
	}
	return pl, nil
}

func (p *playlistWriter) appendSegment(name string, duration float64, start time.Time) error {
	if duration > p.maxDuration {
		p.maxDuration = duration
	}
	p.mediaLines = append(p.mediaLines,
		fmt.Sprintf("#EXTINF:%.6f,", duration),
		"#EXT-X-PROGRAM-DATE-TIME:"+start.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		name,
	)
	if p.listSize > 0 {
		segmentsCount := 0
		for _, line := range p.mediaLines {
			if !strings.HasPrefix(line, "#") {
				segmentsCount++
			}
		}
		for segmentsCount > p.listSize {
			for len(p.mediaLines) > 0 {
				line := p.mediaLines[0]
				p.mediaLines = p.mediaLines[1:]

				if line == "#EXT-X-DISCONTINUITY" {
					p.discontSequence++
				}

				if !strings.HasPrefix(line, "#") {
					p.mediaSequence++
					segmentsCount--
					_ = os.Remove(filepath.Join(filepath.Dir(p.path), line))
					if p.shadow != nil {
						p.shadow.Delete(p.ctx, p.streamID, line)
					}
					break
				}
			}
		}
	}
	return p.publish(false)
}

func (p *playlistWriter) finalize() error { return p.publish(true) }

func (p *playlistWriter) publish(end bool) error {
	target := p.targetDuration
	// Per RFC 8216 §4.3.3.1 the target duration must be >= each segment's
	// rounded EXTINF; round-half-up matches the spec's rounding rule.
	if rounded := int(math.Floor(p.maxDuration + 0.5)); rounded > target {
		target = rounded
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", p.mediaSequence)
	if p.discontSequence > 0 {
		fmt.Fprintf(&b, "#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", p.discontSequence)
	}
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	b.WriteString("#EXT-X-MAP:URI=\"init.mp4\"\n")
	for _, line := range p.mediaLines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if end {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	playlistBytes := []byte(b.String())
	if err := atomicWriteFile(p.path, playlistBytes); err != nil {
		return err
	}
	if p.shadow != nil {
		p.shadow.Publish(p.ctx, p.streamID, store.Object{
			Name:        "index.m3u8",
			Kind:        store.ObjectPlaylist,
			ContentType: "application/vnd.apple.mpegurl",
			Data:        playlistBytes,
			PublishedAt: time.Now(),
		})
	}
	return nil
}

func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
