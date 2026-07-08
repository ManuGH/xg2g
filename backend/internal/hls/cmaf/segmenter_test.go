// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package cmaf

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- synthetic box builders ---

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[:4], uint32(8+len(payload)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func fullbox(typ string, version byte, flags uint32, body []byte) []byte {
	payload := make([]byte, 4+len(body))
	payload[0] = version
	payload[1] = byte(flags >> 16)
	payload[2] = byte(flags >> 8)
	payload[3] = byte(flags)
	copy(payload[4:], body)
	return box(typ, payload)
}

func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func u64(v uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, v); return b }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func testMoov(trackID, timescale uint32) []byte {
	tkhd := fullbox("tkhd", 0, 0, cat(u32(0), u32(0), u32(trackID)))
	mdhd := fullbox("mdhd", 0, 0, cat(u32(0), u32(0), u32(timescale), u32(0)))
	hdlr := fullbox("hdlr", 0, 0, cat(u32(0), []byte("vide"), make([]byte, 12)))
	mdia := box("mdia", cat(mdhd, hdlr))
	trak := box("trak", cat(tkhd, mdia))
	return box("moov", cat(box("mvhd", make([]byte, 20)), trak))
}

// testFragment builds styp+moof+mdat with a trun carrying first-sample-flags
// (sync ⇒ independent per the llhls scanner) and a version-1 tfdt.
func testFragment(sync bool, dts uint64, trackID uint32, mdatSize int) []byte {
	styp := box("styp", make([]byte, 8))
	mfhd := fullbox("mfhd", 0, 0, u32(1))
	tfhd := fullbox("tfhd", 0, 0, u32(trackID))
	tfdt := fullbox("tfdt", 1, 0, u64(dts))
	var firstFlags uint32
	if !sync {
		firstFlags = 0x00010000 // sample_is_non_sync_sample
	}
	// flags: data-offset present (0x1) + first-sample-flags present (0x4)
	trun := fullbox("trun", 0, 0x000005, cat(u32(1), u32(0), u32(firstFlags)))
	traf := box("traf", cat(tfhd, tfdt, trun))
	moof := box("moof", cat(mfhd, traf))
	mdat := box("mdat", make([]byte, mdatSize))
	return cat(styp, moof, mdat)
}

func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return st.Size()
}

// TestSegmenterEndToEnd feeds a synthetic CMAF stream and verifies the
// contract the llhls packager depends on: init published, the open segment
// grows fragment by fragment on disk, rotation happens at the first
// independent fragment past the target duration, and the playlist is
// published atomically with real durations.
func TestSegmenterEndToEnd(t *testing.T) {
	dir := t.TempDir()
	const trackID, timescale = 1, 1000

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), pr, Config{
			Dir:               dir,
			TargetDurationSec: 2,
			Now:               func() time.Time { return time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC) },
		})
	}()
	write := func(data []byte) {
		t.Helper()
		if _, err := pw.Write(data); err != nil {
			t.Fatal(err)
		}
	}

	ftyp := box("ftyp", make([]byte, 8))
	moov := testMoov(trackID, timescale)
	write(cat(ftyp, moov))
	// Init is only published once the first fragment box arrives.
	frag0 := testFragment(true, 0, trackID, 100)
	write(frag0)
	waitForCond(t, "init.mp4", func() bool { return fileSize(filepath.Join(dir, "init.mp4")) == int64(len(ftyp)+len(moov)) })

	seg0 := filepath.Join(dir, "seg_000000.m4s")
	waitForCond(t, "seg0 first fragment", func() bool { return fileSize(seg0) == int64(len(frag0)) })

	// Three dependent 500ms fragments: file must grow, no rotation yet.
	grown := int64(len(frag0))
	for _, dts := range []uint64{500, 1000, 1500} {
		f := testFragment(false, dts, trackID, 80)
		write(f)
		grown += int64(len(f))
	}
	waitForCond(t, "seg0 grown to 4 fragments", func() bool { return fileSize(seg0) == grown })
	if _, err := os.Stat(filepath.Join(dir, "index.m3u8")); !os.IsNotExist(err) {
		t.Fatalf("playlist must not exist before first rotation, stat err=%v", err)
	}

	// Independent fragment at exactly 2s: rotation.
	frag4 := testFragment(true, 2000, trackID, 90)
	write(frag4)
	seg1 := filepath.Join(dir, "seg_000001.m4s")
	waitForCond(t, "rotation to seg1", func() bool { return fileSize(seg1) == int64(len(frag4)) })

	raw, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	pl := string(raw)
	for _, want := range []string{
		"#EXT-X-TARGETDURATION:2",
		`#EXT-X-MAP:URI="init.mp4"`,
		"#EXTINF:2.000000,",
		"#EXT-X-PROGRAM-DATE-TIME:2026-07-05T12:00:00.000Z",
		"seg_000000.m4s",
	} {
		if !strings.Contains(pl, want) {
			t.Errorf("playlist missing %q:\n%s", want, pl)
		}
	}
	if strings.Contains(pl, "seg_000001.m4s") {
		t.Errorf("open segment must not be listed:\n%s", pl)
	}
	if strings.Contains(pl, "#EXT-X-ENDLIST") {
		t.Errorf("live playlist must not carry ENDLIST:\n%s", pl)
	}

	// EOF: final segment flushed with measured duration, ENDLIST appended.
	write(testFragment(false, 2500, trackID, 70))
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatalf("segmenter returned error: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	pl = string(raw)
	if !strings.Contains(pl, "seg_000001.m4s") || !strings.Contains(pl, "#EXT-X-ENDLIST") {
		t.Errorf("final playlist incomplete:\n%s", pl)
	}
}

// TestSegmenterContinuation: a restart into an existing session dir must
// resume numbering after the highest segment and separate the encodes with
// a DISCONTINUITY, preserving prior media entries.
func TestSegmenterContinuation(t *testing.T) {
	dir := t.TempDir()
	prior := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:7",
		"#EXT-X-TARGETDURATION:2",
		"#EXT-X-MEDIA-SEQUENCE:0",
		"#EXT-X-INDEPENDENT-SEGMENTS",
		`#EXT-X-MAP:URI="init.mp4"`,
		"#EXTINF:3.840000,orphan head",
		"seg_000000.m4s",
		"#EXT-X-ENDLIST",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}

	const trackID, timescale = 1, 1000
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), pr, Config{Dir: dir, TargetDurationSec: 2})
	}()

	if _, err := pw.Write(cat(box("ftyp", make([]byte, 8)), testMoov(trackID, timescale))); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(testFragment(true, 0, trackID, 60)); err != nil {
		t.Fatal(err)
	}
	waitForCond(t, "continued segment file", func() bool {
		return fileSize(filepath.Join(dir, "seg_000001.m4s")) > 0
	})
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatalf("segmenter returned error: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	pl := string(raw)
	idxOld := strings.Index(pl, "seg_000000.m4s")
	idxDisc := strings.Index(pl, "#EXT-X-DISCONTINUITY")
	idxNew := strings.Index(pl, "seg_000001.m4s")
	if idxOld < 0 || idxDisc < 0 || idxNew < 0 || !(idxOld < idxDisc && idxDisc < idxNew) {
		t.Fatalf("expected old entry, discontinuity, then new entry:\n%s", pl)
	}
	if strings.Count(pl, "#EXT-X-ENDLIST") != 1 || strings.Index(pl, "#EXT-X-ENDLIST") < idxNew {
		t.Fatalf("ENDLIST must appear once, after the new entry:\n%s", pl)
	}
	// The prior playlist carries a titled 3.84s EXTINF: the parser must read
	// the duration past the title and bump the published target duration.
	if !strings.Contains(pl, "#EXT-X-TARGETDURATION:4") {
		t.Fatalf("expected target duration bumped to 4 from titled 3.84s EXTINF:\n%s", pl)
	}
}
