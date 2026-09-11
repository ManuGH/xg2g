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

// audioHWBoundary says which core the batches before it came from. A new core
// starts its epochs again, and two streams from different cores sharing a
// number would otherwise be reported as one.
type audioHWBoundary struct {
	coreIndex int
	batches   int
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
			shadow := newReplayShadow()
			core := NewGoCore(c.target)
			core.SetAudioShadow(shadow)
			defer core.CloseAudioShadow()

			var boundaries []audioHWBoundary
			coreIndex := 0
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
					shadow.mu.Lock()
					boundaries = append(boundaries, audioHWBoundary{coreIndex: coreIndex, batches: len(shadow.seen)})
					shadow.mu.Unlock()
					coreIndex++
					core = NewGoCore(c.target)
					core.SetAudioShadow(shadow)
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
			shadow.mu.Lock()
			boundaries = append(boundaries, audioHWBoundary{coreIndex: coreIndex, batches: len(shadow.seen)})
			shadow.mu.Unlock()

			trace, esBytes := audioHWTrace(shadow, boundaries)
			for _, tr := range facts.AudioTracks {
				if !observableAudioCodec(tr.Codec) {
					continue
				}
				o := tr.Observed
				trace.WriteString(fmt.Sprintf(
					"stream pid=%04x codec=%s feeds=%d channels=%d lfe=%d acmod=%d hasAcmod=%d dependent=%d frames=%d\n",
					tr.PID, tr.Codec, audioHWFeedsOfCurrent(shadow, core.shadowEpoch, tr.PID),
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

// audioHWTrace renders the captured feeds, with the incarnations numbered by
// the order their first feed appeared.
func audioHWTrace(shadow *replayShadow, boundaries []audioHWBoundary) (*strings.Builder, []byte) {
	shadow.mu.Lock()
	defer shadow.mu.Unlock()

	coreOf := func(i int) int {
		for _, b := range boundaries {
			if i < b.batches {
				return b.coreIndex
			}
		}
		return boundaries[len(boundaries)-1].coreIndex
	}

	type key struct {
		core  int
		epoch uint64
	}
	var order []key
	var trace strings.Builder
	var es []byte
	for i, batch := range shadow.seen {
		k := key{core: coreOf(i), epoch: batch.Epoch}
		inc := -1
		for j, seen := range order {
			if seen == k {
				inc = j
				break
			}
		}
		if inc < 0 {
			inc = len(order)
			order = append(order, k)
		}
		for _, f := range batch.Feeds {
			trace.WriteString(fmt.Sprintf("feed inc=%d pid=%04x len=%d\n", inc, batch.PID, len(f)))
			es = append(es, f...)
		}
	}
	return &trace, es
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
