// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The archived Step-5d transport, replayed through the reference's own audio
// path.
//
// This writes what the reference fed and writes nothing else. The comparison
// with the Rust ingress is a diff of two files produced independently - see
// media-core/scripts/audio-hardware-differential.sh - rather than one
// implementation grading itself against a number the other one handed it.
//
// The feeds come from the AudioShadow seam, which is the reference stating what
// its own observer was given. Re-deriving them from the packets here would be
// writing a second parser and comparing it with itself.
//
// Skipped unless XG2G_AUDIO_HARDWARE_DIR names the archive: the captures are
// tens of megabytes of real broadcast and are not in the repository.

type audioHWCapture struct {
	size   int64
	sha256 string
}

type audioHWStepKind int

const (
	audioHWFeed audioHWStepKind = iota
	audioHWSetTarget
	audioHWNewCore
)

type audioHWStep struct {
	kind    audioHWStepKind
	capture string
	target  uint16
}

type audioHWCase struct {
	name   string
	target uint16
	chunk  int
	steps  []audioHWStep
}

// audioHWCaptures reads the capture identities from the Step-5d manifest and
// from nowhere else. Two copies of a hash are two things that can disagree
// about which bytes a result came from.
func audioHWCaptures(t *testing.T) map[string]audioHWCapture {
	t.Helper()
	f, err := os.Open("../../../../../testdata/psi-hardware/manifest.txt")
	if err != nil {
		t.Fatalf("open the Step-5d manifest: %v", err)
	}
	defer func() { _ = f.Close() }()

	out := map[string]audioHWCapture{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 4 || fields[0] != "capture" {
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			t.Fatalf("capture %s: size: %v", fields[1], err)
		}
		out[fields[1]] = audioHWCapture{size: size, sha256: fields[3]}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read the Step-5d manifest: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("the Step-5d manifest names no captures")
	}
	return out
}

func audioHWCases(t *testing.T) []audioHWCase {
	t.Helper()
	f, err := os.Open("../../../../../testdata/audio-ts-hardware/manifest.txt")
	if err != nil {
		t.Fatalf("open the Step-6c manifest: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []audioHWCase
	var current *audioHWCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "version":
			if fields[1] != "1" {
				t.Fatalf("unknown manifest version %s", fields[1])
			}
		case "case":
			current = &audioHWCase{name: fields[1]}
		case "end":
			if current == nil {
				t.Fatal("end outside a case")
			}
			if current.chunk <= 0 || current.chunk%TSPacketSize != 0 {
				t.Fatalf("case %s: chunk %d is not whole packets", current.name, current.chunk)
			}
			out = append(out, *current)
			current = nil
		case "target":
			current.target = audioHWUint16(t, fields[1])
		case "chunk":
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatalf("chunk: %v", err)
			}
			current.chunk = n
		case "feed":
			current.steps = append(current.steps, audioHWStep{kind: audioHWFeed, capture: fields[1]})
		case "settarget":
			current.steps = append(current.steps, audioHWStep{kind: audioHWSetTarget, target: audioHWUint16(t, fields[1])})
		case "newcore":
			current.steps = append(current.steps, audioHWStep{kind: audioHWNewCore})
		default:
			t.Fatalf("unknown manifest line %q", fields[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read the Step-6c manifest: %v", err)
	}
	if current != nil {
		t.Fatal("a case was never ended")
	}
	if len(out) == 0 {
		t.Fatal("the manifest names no cases")
	}
	return out
}

func audioHWUint16(t *testing.T, s string) uint16 {
	t.Helper()
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		t.Fatalf("not a programme number: %q", s)
	}
	return uint16(n)
}

// audioHWLoad reads a capture and refuses to return bytes that are not the ones
// the manifest names.
func audioHWLoad(t *testing.T, dir, name string, want audioHWCapture) []byte {
	t.Helper()
	path := filepath.Join(dir, name+".ts")
	data, err := os.ReadFile(path) // #nosec G304 -- an operator-named archive directory
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if int64(len(data)) != want.size {
		t.Fatalf("%s is %d bytes, the manifest says %d", name, len(data), want.size)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want.sha256 {
		t.Fatalf("%s hashes to %s, the manifest says %s", name, got, want.sha256)
	}
	return data
}

func TestAudioTSHardware_TheArchivedTransportIsReplayedThroughTheReference(t *testing.T) {
	dir := os.Getenv("XG2G_AUDIO_HARDWARE_DIR")
	if dir == "" {
		t.Skip("XG2G_AUDIO_HARDWARE_DIR is unset; the archive is not here")
	}
	outDir := os.Getenv("XG2G_AUDIO_HARDWARE_OUT")
	if outDir == "" {
		t.Fatal("XG2G_AUDIO_HARDWARE_OUT must say where the traces go")
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("create the output directory: %v", err)
	}

	identities := audioHWCaptures(t)
	loaded := map[string][]byte{}
	ctx := t.Context()

	for _, c := range audioHWCases(t) {
		t.Run(c.name, func(t *testing.T) {
			// One shadow per core, and not one shadow reused.
			//
			// A shadow keys its observers by PID and epoch, and a new core
			// starts its epochs again - so a reused shadow hands the new core's
			// first feed to an observer that already holds the old core's
			// stream, and then reports the two disagreeing. That is the harness
			// describing itself. The traces are joined afterwards instead,
			// where the core they came from is still known.
			shadows := []*replayShadow{newReplayShadow()}
			core := NewGoCore(c.target)
			core.SetAudioShadow(shadows[0])
			defer func() { core.CloseAudioShadow() }()

			var facts Facts
			offset := int64(0)

			for _, step := range c.steps {
				switch step.kind {
				case audioHWSetTarget:
					res, err := core.SetTargetProgram(ctx, step.target)
					if err != nil {
						t.Fatalf("set target: %v", err)
					}
					facts = res.Facts
				case audioHWNewCore:
					audioHWDrain(t, core, c.name)
					core.CloseAudioShadow()
					core = NewGoCore(c.target)
					next := newReplayShadow()
					shadows = append(shadows, next)
					core.SetAudioShadow(next)
					offset = 0
				case audioHWFeed:
					data, ok := loaded[step.capture]
					if !ok {
						want, named := identities[step.capture]
						if !named {
							t.Fatalf("no capture named %s in the Step-5d manifest", step.capture)
						}
						data = audioHWLoad(t, dir, step.capture, want)
						loaded[step.capture] = data
					}
					for len(data) > 0 {
						n := c.chunk
						if n > len(data) {
							n = len(data)
						}
						res, err := core.Ingest(ctx, offset, data[:n])
						if err != nil {
							t.Fatalf("ingest: %v", err)
						}
						offset += int64(n)
						facts = res.Facts
						data = data[n:]
						// The comparison is asynchronous and its queue is four
						// chunks deep. A capture handed over faster than the
						// worker takes it fills the queue and retires the
						// shadow, and a retired shadow is no trace at all.
						audioHWDrain(t, core, c.name)
					}
				}
			}
			audioHWDrain(t, core, c.name)

			trace, esBytes := audioHWTrace(shadows)
			// Every audio track the table declares, so that a capture with no
			// observable audio says which codecs it carried instead of saying
			// nothing at all.
			for _, tr := range facts.AudioTracks {
				trace.WriteString(fmt.Sprintf("track pid=%04x codec=%s lang=%s observable=%d\n",
					tr.PID, tr.Codec, tr.Language, b2i(observableAudioCodec(tr.Codec))))
			}
			last := shadows[len(shadows)-1]
			for _, tr := range facts.AudioTracks {
				if !observableAudioCodec(tr.Codec) {
					continue
				}
				o := tr.Observed
				trace.WriteString(fmt.Sprintf(
					"stream pid=%04x codec=%s feeds=%d channels=%d lfe=%d acmod=%d hasAcmod=%d dependent=%d frames=%d\n",
					tr.PID, tr.Codec, audioHWFeedsOfCurrent(last, core.shadowEpoch, tr.PID),
					o.Channels, b2i(o.LFE), o.Acmod, b2i(o.HasAcmod), b2i(o.DependentSubstream), o.Frames))
			}

			if err := os.WriteFile(filepath.Join(outDir, c.name+".go.trace"), []byte(trace.String()), 0o644); err != nil { // #nosec G306 -- evidence
				t.Fatalf("write the trace: %v", err)
			}
			if err := os.WriteFile(filepath.Join(outDir, c.name+".go.es"), esBytes, 0o644); err != nil { // #nosec G306 -- evidence
				t.Fatalf("write the bytes: %v", err)
			}
			t.Logf("%s: %d feeds, %d elementary stream bytes", c.name, strings.Count(trace.String(), "feed inc="), len(esBytes))
		})
	}
}

func audioHWDrain(t *testing.T, core *GoCore, name string) {
	t.Helper()
	report := awaitShadow(t, core, "the shadow to catch up", func(r AudioShadowReport) bool {
		return r.Compared == r.Batches || r.Disabled
	})
	if report.Disabled {
		t.Fatalf("case %s: the shadow was retired mid-run, so the trace is incomplete: %+v", name, report)
	}
	if report.Mismatches != 0 {
		t.Fatalf("case %s: the replay disagreed with the core: %+v", name, report)
	}
}

// audioHWTrace renders the captured feeds in the one order that survives how
// they were batched: streams in the order their first feed appeared, and each
// stream's feeds in the order they were given.
//
// The reference groups a call's feeds per stream, so two streams interleaved
// packet by packet come out as one run each from a single call and as an
// alternation from one call per packet. The Rust side writes packet order. The
// same feeds to the same observers either way - and a positional comparison of
// the two would report a mismatch the moment a capture carried two observable
// tracks. The corpus canonicalises for exactly this reason; so does this.
func audioHWTrace(shadows []*replayShadow) (*strings.Builder, []byte) {
	type key struct {
		core  int
		epoch uint64
	}
	index := map[key]int{}
	var feeds []audioTSFeed
	for coreIndex, shadow := range shadows {
		shadow.mu.Lock()
		batches := append([]AudioShadowBatch(nil), shadow.seen...)
		shadow.mu.Unlock()
		for _, batch := range batches {
			k := key{core: coreIndex, epoch: batch.Epoch}
			inc, ok := index[k]
			if !ok {
				inc = len(index)
				index[k] = inc
			}
			for _, f := range batch.Feeds {
				feeds = append(feeds, audioTSFeed{incarnation: inc, pid: batch.PID, es: f})
			}
		}
	}
	var trace strings.Builder
	var es []byte
	for _, f := range audioTSCanonical(feeds) {
		trace.WriteString(fmt.Sprintf("feed inc=%d pid=%04x len=%d\n", f.incarnation, f.pid, len(f.es)))
		es = append(es, f.es...)
	}
	return &trace, es
}

// The trace may not know how the reference batched. Two shadows holding the
// same four feeds - once as the alternation one packet per call produces, once
// as the two runs a single call produces - have to render identically.
func TestAudioTSHardware_TheTraceDoesNotDependOnHowTheReferenceBatches(t *testing.T) {
	f := func(b byte) []byte { return []byte{b, b, b} }
	interleaved := newReplayShadow()
	interleaved.seen = []AudioShadowBatch{
		{PID: 0x100, Epoch: 2, Feeds: [][]byte{f(1)}},
		{PID: 0x101, Epoch: 2, Feeds: [][]byte{f(2)}},
		{PID: 0x100, Epoch: 2, Feeds: [][]byte{f(3)}},
		{PID: 0x101, Epoch: 2, Feeds: [][]byte{f(4)}},
	}
	grouped := newReplayShadow()
	grouped.seen = []AudioShadowBatch{
		{PID: 0x100, Epoch: 2, Feeds: [][]byte{f(1), f(3)}},
		{PID: 0x101, Epoch: 2, Feeds: [][]byte{f(2), f(4)}},
	}
	a, aBytes := audioHWTrace([]*replayShadow{interleaved})
	b, bBytes := audioHWTrace([]*replayShadow{grouped})
	if a.String() != b.String() || string(aBytes) != string(bBytes) {
		t.Fatalf("the trace depends on batching:\n%s\nversus\n%s", a.String(), b.String())
	}
	want := "feed inc=0 pid=0100 len=3\nfeed inc=0 pid=0100 len=3\nfeed inc=0 pid=0101 len=3\nfeed inc=0 pid=0101 len=3\n"
	if a.String() != want {
		t.Fatalf("trace\n%s\nwant\n%s", a.String(), want)
	}
	if string(aBytes) != string(append(append(append(f(1), f(3)...), f(2)...), f(4)...)) {
		t.Fatalf("the bytes are not in the order the trace names them: %x", aBytes)
	}
}

// And the order it settles on is the corpus's. Two observable streams, handed
// over one packet per call - the case that batches least like a single call -
// render exactly as the authored feeds do. The Rust side is held to the same
// rendering of the same authored feeds, which is what makes the two writers
// agree on a multi-track capture before one has ever been archived.
func TestAudioTSHardware_TheTraceOfTwoStreamsIsTheCorpusOrder(t *testing.T) {
	var c audioTSCase
	for _, candidate := range audioTSCorpusCases() {
		if candidate.name == "two_observable_streams_at_once" {
			c = candidate
		}
	}
	if c.name == "" {
		t.Fatal("the corpus has no two-stream case")
	}
	ctx := t.Context()
	shadow := newReplayShadow()
	core := NewGoCore(c.initial)
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()
	offset := int64(0)
	for _, step := range c.steps {
		if step.kind != audioTSStepChunk {
			t.Fatalf("case %s has a step this test does not drive", c.name)
		}
		for _, part := range audioTSCut(step.chunk, TSPacketSize) {
			if _, err := core.Ingest(ctx, offset, part); err != nil {
				t.Fatalf("ingest: %v", err)
			}
			offset += int64(len(part))
			audioHWDrain(t, core, c.name)
		}
	}
	got, gotBytes := audioHWTrace([]*replayShadow{shadow})

	var want strings.Builder
	var wantBytes []byte
	for _, f := range audioTSCanonical(c.want) {
		want.WriteString(fmt.Sprintf("feed inc=%d pid=%04x len=%d\n", f.incarnation, f.pid, len(f.es)))
		wantBytes = append(wantBytes, f.es...)
	}
	if got.String() != want.String() {
		t.Fatalf("trace\n%s\nwant\n%s", got.String(), want.String())
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatal("the bytes differ from the authored feeds")
	}
}

func audioHWFeedsOfCurrent(shadow *replayShadow, epoch uint64, pid uint16) int {
	shadow.mu.Lock()
	defer shadow.mu.Unlock()
	n := 0
	for _, b := range shadow.seen {
		if b.Epoch == epoch && b.PID == pid {
			n += len(b.Feeds)
		}
	}
	return n
}
