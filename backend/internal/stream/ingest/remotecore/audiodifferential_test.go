// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The shared raw-TS audio corpus, read and enforced.
//
// Step 7d-B enforces full audio facts and observation over IPC Protocol v5:
// - Coverage: ParseCoverageComplete (wireCoverageComplete = 2)
// - AudioTrack: 21 bytes (10B declared + 11B observed per PMT audio track)
// - Facts: 105 bytes (81B video + 24B audio scrambling block)
//
// Differential test compares:
// 1. Rust RemoteCore against authored truth (facts) for all 47 cases (100% authored truth)
// 2. Go GoCore against authored truth (facts) or ref-facts for diverging cases
// 3. Exactly 2 classified divergences (divergence, defect), zero unclassified

const audioCorpusPath = "../../../../../testdata/audio-ts-corpus/corpus.txt"
const audioCorpusFormatVersion = "1"
const audioCorpusMinimumCases = 47

type audioTSDifferentialTrackFacts struct {
	pid       uint16
	channels  int
	lfe       bool
	acmod     uint8
	hasAcmod  bool
	dependent bool
	frames    uint64
}

type audioTSDifferentialFacts struct {
	audioScrambled uint64
	audioClear     uint64
	audioClearRun  uint64
	audioPIDs      []uint16
	tracks         []audioTSDifferentialTrackFacts
}

type audioTSDifferentialStep struct {
	isChunk bool
	chunk   []byte
	target  uint16

	authoredFacts *audioTSDifferentialFacts
	refFacts      *audioTSDifferentialFacts
}

func (s audioTSDifferentialStep) expectedRefFacts(divergent bool) *audioTSDifferentialFacts {
	if divergent && s.refFacts != nil {
		return s.refFacts
	}
	return s.authoredFacts
}

func (s audioTSDifferentialStep) String() string {
	if s.isChunk {
		return fmt.Sprintf("chunk of %d packets", len(s.chunk)/188)
	}
	return fmt.Sprintf("target %d", s.target)
}

type audioTSDifferentialCase struct {
	name       string
	desc       string
	initial    uint16
	divergence string
	class      string
	steps      []audioTSDifferentialStep
}

func parseAudioDiffFacts(line string) (audioTSDifferentialFacts, error) {
	var f audioTSDifferentialFacts
	parts := strings.Fields(line)
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "ascr":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("ascr: %w", err)
			}
			f.audioScrambled = n
		case "aclr":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("aclr: %w", err)
			}
			f.audioClear = n
		case "arun":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("arun: %w", err)
			}
			f.audioClearRun = n
		case "apids":
			if v != "none" {
				for _, pStr := range strings.Split(v, ",") {
					n, err := strconv.ParseUint(pStr, 16, 16)
					if err != nil {
						return f, fmt.Errorf("apids %s: %w", pStr, err)
					}
					f.audioPIDs = append(f.audioPIDs, uint16(n))
				}
			}
		case "tracks":
			if v != "none" {
				for _, trStr := range strings.Split(v, ";") {
					pidStr, obsStr, ok := strings.Cut(trStr, ":")
					if !ok {
						return f, fmt.Errorf("invalid track %s", trStr)
					}
					pid, err := strconv.ParseUint(pidStr, 16, 16)
					if err != nil {
						return f, fmt.Errorf("track pid %s: %w", pidStr, err)
					}
					tr := audioTSDifferentialTrackFacts{pid: uint16(pid)}
					for _, kv := range strings.Split(obsStr, ",") {
						okey, oval, ok := strings.Cut(kv, "=")
						if !ok {
							continue
						}
						onum, err := strconv.ParseUint(oval, 10, 64)
						if err != nil {
							return f, fmt.Errorf("track field %s: %w", kv, err)
						}
						switch okey {
						case "ch":
							tr.channels = int(onum)
						case "lfe":
							tr.lfe = (onum == 1)
						case "acmod":
							tr.acmod = uint8(onum)
						case "hasAcmod":
							tr.hasAcmod = (onum == 1)
						case "dep":
							tr.dependent = (onum == 1)
						case "frames":
							tr.frames = onum
						}
					}
					f.tracks = append(f.tracks, tr)
				}
			}
		}
	}
	return f, nil
}

func loadAudioCorpus(t *testing.T) []audioTSDifferentialCase {
	t.Helper()

	file, err := os.Open(audioCorpusPath)
	if err != nil {
		t.Fatalf("open %s: %v", audioCorpusPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	var cases []audioTSDifferentialCase
	var current *audioTSDifferentialCase
	var currentStep *audioTSDifferentialStep
	sawHeader := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if !sawHeader {
			prefix := "# xg2g raw transport to audio elementary stream corpus, format version " + audioCorpusFormatVersion
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("corpus file does not begin with header %q: got %q", prefix, line)
			}
			sawHeader = true
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		keyword, rest, _ := strings.Cut(line, " ")
		switch keyword {
		case "case":
			current = &audioTSDifferentialCase{
				name: rest,
			}

		case "desc":
			if current == nil {
				t.Fatalf("desc before case: %q", line)
			}
			current.desc = rest

		case "program":
			if current == nil {
				t.Fatalf("program before case: %q", line)
			}
			initVal, err := strconv.ParseUint(rest, 10, 16)
			if err != nil {
				t.Fatalf("parse program %q: %v", rest, err)
			}
			current.initial = uint16(initVal)

		case "diverges":
			if current == nil {
				t.Fatalf("divergence before case: %q", line)
			}
			class, reason, ok := strings.Cut(rest, ": ")
			if !ok {
				class = rest
			}
			current.class = class
			current.divergence = reason

		case "chunk":
			if current == nil {
				t.Fatalf("chunk before case: %q", line)
			}
			if currentStep != nil {
				current.steps = append(current.steps, *currentStep)
			}
			data, err := hex.DecodeString(rest)
			if err != nil {
				t.Fatalf("decode chunk hex: %v", err)
			}
			currentStep = &audioTSDifferentialStep{
				isChunk: true,
				chunk:   data,
			}

		case "target":
			if current == nil {
				t.Fatalf("target before case: %q", line)
			}
			if currentStep != nil {
				current.steps = append(current.steps, *currentStep)
			}
			targetVal, err := strconv.ParseUint(rest, 10, 16)
			if err != nil {
				t.Fatalf("parse target %q: %v", rest, err)
			}
			currentStep = &audioTSDifferentialStep{
				isChunk: false,
				target:  uint16(targetVal),
			}

		case "facts":
			if currentStep == nil {
				t.Fatalf("facts before step: %q", line)
			}
			facts, err := parseAudioDiffFacts(rest)
			if err != nil {
				t.Fatalf("parse facts %q: %v", rest, err)
			}
			currentStep.authoredFacts = &facts

		case "ref-facts":
			if currentStep == nil {
				t.Fatalf("ref-facts before step: %q", line)
			}
			facts, err := parseAudioDiffFacts(rest)
			if err != nil {
				t.Fatalf("parse ref-facts %q: %v", rest, err)
			}
			currentStep.refFacts = &facts

		case "end":
			if current != nil {
				if currentStep != nil {
					current.steps = append(current.steps, *currentStep)
					currentStep = nil
				}
				cases = append(cases, *current)
				current = nil
			}

		case "feed", "ref-feed", "stream", "ref-stream":
			// Processed by mediafacts reference tests; differential tests compare facts
		}
	}

	if current != nil {
		if currentStep != nil {
			current.steps = append(current.steps, *currentStep)
		}
		cases = append(cases, *current)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}

	if len(cases) < audioCorpusMinimumCases {
		t.Fatalf("corpus had %d cases, want at least %d", len(cases), audioCorpusMinimumCases)
	}

	return cases
}

func audioTSDifferentialFactsOf(f mediafacts.Facts) audioTSDifferentialFacts {
	out := audioTSDifferentialFacts{
		audioScrambled: f.Scrambling.AudioScrambled,
		audioClear:     f.Scrambling.AudioClear,
		audioClearRun:  f.Scrambling.AudioClearRun,
		audioPIDs:      append([]uint16(nil), f.AudioPIDs...),
	}
	for _, tr := range f.AudioTracks {
		out.tracks = append(out.tracks, audioTSDifferentialTrackFacts{
			pid:       tr.PID,
			channels:  tr.Observed.Channels,
			lfe:       tr.Observed.LFE,
			acmod:     tr.Observed.Acmod,
			hasAcmod:  tr.Observed.HasAcmod,
			dependent: tr.Observed.DependentSubstream,
			frames:    tr.Observed.Frames,
		})
	}
	return out
}

func audioTSDiffFactsLine(label string, f audioTSDifferentialFacts) string {
	apids := "none"
	if len(f.audioPIDs) > 0 {
		strs := make([]string, len(f.audioPIDs))
		for i, p := range f.audioPIDs {
			strs[i] = fmt.Sprintf("%04x", p)
		}
		apids = strings.Join(strs, ",")
	}
	tracks := "none"
	if len(f.tracks) > 0 {
		strs := make([]string, len(f.tracks))
		for i, tr := range f.tracks {
			b2i := func(b bool) int {
				if b {
					return 1
				}
				return 0
			}
			strs[i] = fmt.Sprintf("%04x:ch=%d,lfe=%d,acmod=%d,hasAcmod=%d,dep=%d,frames=%d",
				tr.pid, tr.channels, b2i(tr.lfe), tr.acmod, b2i(tr.hasAcmod), b2i(tr.dependent), tr.frames)
		}
		tracks = strings.Join(strs, ";")
	}
	return fmt.Sprintf("%s: ascr=%d aclr=%d arun=%d apids=%s tracks=%s",
		label, f.audioScrambled, f.audioClear, f.audioClearRun, apids, tracks)
}

func diffAudioFacts(got, want audioTSDifferentialFacts, stepIdx int, peer string) []string {
	if got.audioScrambled != want.audioScrambled ||
		got.audioClear != want.audioClear ||
		got.audioClearRun != want.audioClearRun ||
		len(got.audioPIDs) != len(want.audioPIDs) ||
		len(got.tracks) != len(want.tracks) {
		return []string{fmt.Sprintf("  step %d facts mismatch:\n    got  %s\n    want %s",
			stepIdx, audioTSDiffFactsLine(peer, got), audioTSDiffFactsLine("want", want))}
	}

	for i := range got.audioPIDs {
		if got.audioPIDs[i] != want.audioPIDs[i] {
			return []string{fmt.Sprintf("  step %d audio PIDs mismatch:\n    got  %s\n    want %s",
				stepIdx, audioTSDiffFactsLine(peer, got), audioTSDiffFactsLine("want", want))}
		}
	}

	for i := range got.tracks {
		g, w := got.tracks[i], want.tracks[i]
		if g.pid != w.pid ||
			g.channels != w.channels ||
			g.lfe != w.lfe ||
			g.acmod != w.acmod ||
			g.hasAcmod != w.hasAcmod ||
			g.dependent != w.dependent ||
			g.frames != w.frames {
			return []string{fmt.Sprintf("  step %d audio track %d mismatch:\n    got  %s\n    want %s",
				stepIdx, i, audioTSDiffFactsLine(peer, got), audioTSDiffFactsLine("want", want))}
		}
	}

	return nil
}

// TestAudioDifferential_OnlyTheClassifiedDivergencesExist asserts that only the 2
// reviewed divergences exist in the audio corpus.
func TestAudioDifferential_OnlyTheClassifiedDivergencesExist(t *testing.T) {
	cases := loadAudioCorpus(t)
	want := map[string]string{
		"a_pes_header_reaching_past_its_packet": "divergence",
		"scrambled_packet_while_in_header":      "defect",
	}
	got := map[string]string{}
	for _, c := range cases {
		if c.divergence != "" {
			got[c.name] = c.class
		}
	}
	for name, class := range want {
		if got[name] != class {
			t.Errorf("case %s: want class %q, got %q", name, class, got[name])
		}
	}
	for name, class := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("case %s diverges (%s) but is not in the reviewed list", name, class)
		}
	}
}

// TestAudioDifferential_TheRealRustCoreAgreesCallByCall is the 3-way differential
// proof across all 47 audio corpus cases over real Unix domain socket IPC.
func TestAudioDifferential_TheRealRustCoreAgreesCallByCall(t *testing.T) {
	bin := requireVideoRealCore(t)
	cases := loadAudioCorpus(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	compared := 0

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			remote, err := Start(ctx, bin, c.initial)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() {
				if err := remote.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			local := mediafacts.NewGoCore(c.initial)
			offset := int64(0)
			isDivergent := (c.divergence != "")

			for i, step := range c.steps {
				var goRes, rustRes mediafacts.ParseResult
				var goErr, rustErr error

				if step.isChunk {
					goRes, goErr = local.Ingest(ctx, offset, step.chunk)
					rustRes, rustErr = remote.Ingest(ctx, offset, step.chunk)
					offset += int64(len(step.chunk))
				} else {
					goRes, goErr = local.SetTargetProgram(ctx, step.target)
					rustRes, rustErr = remote.SetTargetProgram(ctx, step.target)
				}

				if goErr != nil {
					t.Fatalf("step %d (%s): the reference failed: %v", i+1, step, goErr)
				}
				if rustErr != nil {
					t.Fatalf("step %d (%s): the real core failed: %v", i+1, step, rustErr)
				}

				// Coverage assertion: Reference and Rust are both ParseCoverageComplete
				if !goRes.Covers(mediafacts.ParseCoverageComplete) {
					t.Fatalf("step %d: the reference reported coverage %s, want complete", i+1, goRes.Coverage)
				}
				if !rustRes.Covers(mediafacts.ParseCoverageComplete) {
					t.Fatalf("step %d: the real core reported coverage %s, want complete", i+1, rustRes.Coverage)
				}

				// Offset assertion
				wantOffset := offset
				if !step.isChunk {
					wantOffset = 0
				}
				if goRes.ProcessedThroughOffset != wantOffset {
					t.Fatalf("step %d: reference processed through %d, want %d", i+1, goRes.ProcessedThroughOffset, wantOffset)
				}
				if rustRes.ProcessedThroughOffset != wantOffset {
					t.Fatalf("step %d: rust processed through %d, want %d", i+1, rustRes.ProcessedThroughOffset, wantOffset)
				}

				// 1. Rust must match authored expectations 100% (normative truth)
				if step.authoredFacts == nil {
					t.Fatalf("step %d of %d in %s has no authored facts", i+1, len(c.steps), c.name)
				}
				rustFacts := audioTSDifferentialFactsOf(rustRes.Facts)
				if bad := diffAudioFacts(rustFacts, *step.authoredFacts, i+1, "rust"); len(bad) > 0 {
					t.Fatalf("step %d of %d in %s:\n%s", i+1, len(c.steps), c.name, strings.Join(bad, "\n"))
				}

				// 2. Go reference matches authored (non-divergent) or ref-facts (divergent)
				if wantGoFacts := step.expectedRefFacts(isDivergent); wantGoFacts != nil {
					goFacts := audioTSDifferentialFactsOf(goRes.Facts)
					if bad := diffAudioFacts(goFacts, *wantGoFacts, i+1, "go"); len(bad) > 0 {
						t.Fatalf("step %d of %d in %s:\n%s", i+1, len(c.steps), c.name, strings.Join(bad, "\n"))
					}
				}

				compared++
			}
		})
	}

	wantSteps := 0
	for _, c := range cases {
		wantSteps += len(c.steps)
	}
	if compared != wantSteps {
		t.Fatalf("compared %d steps, the corpus has %d", compared, wantSteps)
	}

	t.Logf("real-process audio differential: %d cases, %d steps, all exact (0 unclassified divergences)",
		len(cases), compared)
}
