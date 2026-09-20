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

// The shared raw-TS video corpus, read and enforced.
//
// Step 7d-A enforces full video facts and RAP events over IPC Protocol v4:
// - Coverage: ParseCoveragePSIVideo (wireCoveragePSIVideo = 3)
// - Events: ProgramIdentityChanged, RandomAccessPoint, RandomAccessPointInvalidated
// - Facts: 81-byte video facts block decoding into mediafacts.Facts
//
// Differential test compares:
// 1. Rust RemoteCore against authored truth (event, facts) for all 94 cases (100% authored truth)
// 2. Go GoCore against authored truth (event, facts) or ref-event/ref-facts for diverging cases
// 3. Exactly 5 classified divergences (quirk, limitation, divergence), zero unclassified

const videoCorpusPath = "../../../../../testdata/video-ts-corpus/corpus.txt"
const videoCorpusFormatVersion = "1"
const videoCorpusMinimumCases = 94

type videoTSEvent struct {
	kind     string // "rap", "rap_invalidated", "identity"
	offset   int64
	joinable bool
}

type videoTSFacts struct {
	paramSets          bool
	irapPoints         uint64
	intraPoints        uint64
	recoverySEIs       uint64
	predictedRejected  uint64
	unreadable         uint64
	videoScrambled     uint64
	videoClear         uint64
	videoClearRun      uint64
	cleanEntry         uint64
	cleanAUs           uint64
	scrambledConfirmed bool
	videoPID           uint16
	codec              mediafacts.VideoCodec
}

type videoCorpusStep struct {
	isChunk bool
	chunk   []byte
	target  uint16

	authoredEvents []videoTSEvent
	authoredFacts  *videoTSFacts

	refEvents     []videoTSEvent
	refEventsSeen bool
	refFacts      *videoTSFacts
}

func (s videoCorpusStep) expectedRefEvents(divergent bool) []videoTSEvent {
	if divergent {
		return s.refEvents
	}
	return s.authoredEvents
}

func (s videoCorpusStep) expectedRefFacts(divergent bool) *videoTSFacts {
	if divergent {
		return s.refFacts
	}
	return s.authoredFacts
}

func (s videoCorpusStep) String() string {
	if s.isChunk {
		return fmt.Sprintf("chunk of %d packets", len(s.chunk)/188)
	}
	return fmt.Sprintf("target %d", s.target)
}

type videoCorpusCase struct {
	name       string
	desc       string
	initial    uint16
	divergence string
	class      string
	steps      []videoCorpusStep
}

func parseVideoEvent(line string) (videoTSEvent, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return videoTSEvent{}, fmt.Errorf("empty event line")
	}
	switch parts[0] {
	case "identity":
		return videoTSEvent{kind: "identity"}, nil
	case "rap":
		var offset int64
		var joinable bool
		for _, p := range parts[1:] {
			if k, v, ok := strings.Cut(p, "="); ok {
				switch k {
				case "offset":
					n, err := strconv.ParseInt(v, 10, 64)
					if err != nil {
						return videoTSEvent{}, fmt.Errorf("rap offset: %w", err)
					}
					offset = n
				case "joinable":
					joinable = (v != "0")
				}
			}
		}
		return videoTSEvent{kind: "rap", offset: offset, joinable: joinable}, nil
	case "rap_invalidated":
		var offset int64
		for _, p := range parts[1:] {
			if k, v, ok := strings.Cut(p, "="); ok {
				if k == "offset" {
					n, err := strconv.ParseInt(v, 10, 64)
					if err != nil {
						return videoTSEvent{}, fmt.Errorf("rap_invalidated offset: %w", err)
					}
					offset = n
				}
			}
		}
		return videoTSEvent{kind: "rap_invalidated", offset: offset}, nil
	default:
		return videoTSEvent{}, fmt.Errorf("unknown event kind: %s", parts[0])
	}
}

func parseVideoFacts(line string) (videoTSFacts, error) {
	var f videoTSFacts
	parts := strings.Fields(line)
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "ps":
			f.paramSets = (v != "0")
		case "irap":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("irap: %w", err)
			}
			f.irapPoints = n
		case "intra":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("intra: %w", err)
			}
			f.intraPoints = n
		case "rpsei":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("rpsei: %w", err)
			}
			f.recoverySEIs = n
		case "predrej":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("predrej: %w", err)
			}
			f.predictedRejected = n
		case "unread":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("unread: %w", err)
			}
			f.unreadable = n
		case "vscr":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("vscr: %w", err)
			}
			f.videoScrambled = n
		case "vclr":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("vclr: %w", err)
			}
			f.videoClear = n
		case "vrun":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("vrun: %w", err)
			}
			f.videoClearRun = n
		case "cleanrap":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("cleanrap: %w", err)
			}
			f.cleanEntry = n
		case "cleanau":
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return f, fmt.Errorf("cleanau: %w", err)
			}
			f.cleanAUs = n
		case "scrconf":
			f.scrambledConfirmed = (v != "0")
		case "vpid":
			n, err := strconv.ParseUint(v, 16, 16)
			if err != nil {
				return f, fmt.Errorf("vpid: %w", err)
			}
			f.videoPID = uint16(n)
		case "codec":
			f.codec = mediafacts.VideoCodec(v)
		}
	}
	return f, nil
}

func loadVideoCorpus(t *testing.T) []videoCorpusCase {
	t.Helper()

	f, err := os.Open(videoCorpusPath)
	if err != nil {
		t.Fatalf("open video corpus: %v", err)
	}
	defer func() { _ = f.Close() }()

	var (
		cases   []videoCorpusCase
		current *videoCorpusCase
		step    *videoCorpusStep
		version string
	)

	finishStep := func() {
		if current != nil && step != nil {
			current.steps = append(current.steps, *step)
			step = nil
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		keyword, rest, _ := strings.Cut(text, " ")

		switch keyword {
		case "version":
			if version != "" {
				t.Fatalf("line %d: the corpus declares its version twice", line)
			}
			if rest != videoCorpusFormatVersion {
				t.Fatalf("line %d: corpus format version %q, this reader understands %q",
					line, rest, videoCorpusFormatVersion)
			}
			version = rest

		case "case":
			if version == "" {
				t.Fatalf("line %d: a case before the format version", line)
			}
			if current != nil {
				t.Fatalf("line %d: a case inside a case", line)
			}
			current = &videoCorpusCase{name: rest}

		case "desc":
			if current == nil {
				t.Fatalf("line %d: desc outside a case", line)
			}
			current.desc = rest

		case "program":
			if current == nil {
				t.Fatalf("line %d: program outside a case", line)
			}
			n, err := strconv.ParseUint(rest, 10, 16)
			if err != nil {
				t.Fatalf("line %d: program: %v", line, err)
			}
			current.initial = uint16(n)

		case "diverges":
			if current == nil {
				t.Fatalf("line %d: diverges outside a case", line)
			}
			class, why, ok := strings.Cut(rest, ":")
			if !ok {
				t.Fatalf("line %d: invalid diverges syntax: %q", line, rest)
			}
			current.class = strings.TrimSpace(class)
			current.divergence = strings.TrimSpace(why)

		case "chunk":
			if current == nil {
				t.Fatalf("line %d: chunk outside a case", line)
			}
			finishStep()
			raw, err := hex.DecodeString(rest)
			if err != nil {
				t.Fatalf("line %d: chunk hex: %v", line, err)
			}
			step = &videoCorpusStep{isChunk: true, chunk: raw}

		case "target":
			if current == nil {
				t.Fatalf("line %d: target outside a case", line)
			}
			finishStep()
			n, err := strconv.ParseUint(rest, 10, 16)
			if err != nil {
				t.Fatalf("line %d: target: %v", line, err)
			}
			step = &videoCorpusStep{isChunk: false, target: uint16(n)}

		case "event":
			if step == nil {
				t.Fatalf("line %d: event before a step", line)
			}
			ev, err := parseVideoEvent(rest)
			if err != nil {
				t.Fatalf("line %d: event: %v", line, err)
			}
			step.authoredEvents = append(step.authoredEvents, ev)

		case "facts":
			if step == nil {
				t.Fatalf("line %d: facts before a step", line)
			}
			facts, err := parseVideoFacts(rest)
			if err != nil {
				t.Fatalf("line %d: facts: %v", line, err)
			}
			step.authoredFacts = &facts

		case "ref-event":
			if step == nil {
				t.Fatalf("line %d: ref-event before a step", line)
			}
			ev, err := parseVideoEvent(rest)
			if err != nil {
				t.Fatalf("line %d: ref-event: %v", line, err)
			}
			step.refEvents = append(step.refEvents, ev)
			step.refEventsSeen = true

		case "ref-facts":
			if step == nil {
				t.Fatalf("line %d: ref-facts before a step", line)
			}
			facts, err := parseVideoFacts(rest)
			if err != nil {
				t.Fatalf("line %d: ref-facts: %v", line, err)
			}
			step.refFacts = &facts

		case "end":
			if current == nil {
				t.Fatalf("line %d: end outside a case", line)
			}
			finishStep()
			if len(current.steps) == 0 {
				t.Fatalf("line %d: case %q has no steps", line, current.name)
			}
			cases = append(cases, *current)
			current = nil

		default:
			t.Fatalf("line %d: %q is not a keyword this reader knows", line, keyword)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read the corpus: %v", err)
	}
	if current != nil {
		t.Fatal("the corpus ends inside a case")
	}
	if version == "" {
		t.Fatal("the corpus never declared its format version")
	}
	if len(cases) < videoCorpusMinimumCases {
		t.Fatalf("the reader found %d cases and the corpus had at least %d",
			len(cases), videoCorpusMinimumCases)
	}
	return cases
}

func videoTSEventsOf(events []mediafacts.Event) []videoTSEvent {
	out := make([]videoTSEvent, 0, len(events))
	for _, e := range events {
		switch e.Kind {
		case mediafacts.EventRandomAccessPoint:
			out = append(out, videoTSEvent{kind: "rap", offset: e.Offset, joinable: e.Joinable})
		case mediafacts.EventRandomAccessPointInvalidated:
			out = append(out, videoTSEvent{kind: "rap_invalidated", offset: e.Offset})
		case mediafacts.EventProgramIdentityChanged:
			out = append(out, videoTSEvent{kind: "identity"})
		default:
			out = append(out, videoTSEvent{kind: fmt.Sprintf("unknown(%d)", e.Kind), offset: e.Offset})
		}
	}
	return out
}

func videoTSFactsOf(f mediafacts.Facts) videoTSFacts {
	return videoTSFacts{
		paramSets:          f.ParameterSetsSeen,
		irapPoints:         f.RandomAccess.IRAPPoints,
		intraPoints:        f.RandomAccess.IntraPoints,
		recoverySEIs:       f.RandomAccess.RecoveryPointSEIs,
		predictedRejected:  f.RandomAccess.PredictedRejected,
		unreadable:         f.RandomAccess.UnreadableSlices,
		videoScrambled:     f.Scrambling.VideoScrambled,
		videoClear:         f.Scrambling.VideoClear,
		videoClearRun:      f.Scrambling.VideoClearRun,
		cleanEntry:         f.CleanEntryPoints,
		cleanAUs:           f.CleanAccessUnits,
		scrambledConfirmed: f.ScrambledVideoConfirmed,
		videoPID:           f.VideoPID,
		codec:              f.VideoCodec,
	}
}

func videoTSEventLine(kind string, e videoTSEvent) string {
	if e.kind == "rap" {
		return fmt.Sprintf("  %s rap offset=%d joinable=%d", kind, e.offset, b2i(e.joinable))
	}
	if e.kind == "rap_invalidated" {
		return fmt.Sprintf("  %s rap_invalidated offset=%d", kind, e.offset)
	}
	return fmt.Sprintf("  %s %s", kind, e.kind)
}

func videoTSFactsLine(kind string, f videoTSFacts) string {
	return fmt.Sprintf("  %s ps=%d irap=%d intra=%d rpsei=%d predrej=%d unread=%d vscr=%d vclr=%d vrun=%d cleanrap=%d cleanau=%d scrconf=%d vpid=%04x codec=%s",
		kind, b2i(f.paramSets), f.irapPoints, f.intraPoints, f.recoverySEIs, f.predictedRejected, f.unreadable,
		f.videoScrambled, f.videoClear, f.videoClearRun, f.cleanEntry, f.cleanAUs, b2i(f.scrambledConfirmed),
		f.videoPID, f.codec)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func diffVideoEvents(got, want []videoTSEvent, stepIdx int, peer string) []string {
	var bad []string
	if len(got) != len(want) {
		bad = append(bad, fmt.Sprintf("  step %d: %s emitted %d event(s), want %d", stepIdx, peer, len(got), len(want)))
	}
	minLen := len(got)
	if len(want) < minLen {
		minLen = len(want)
	}
	for i := 0; i < minLen; i++ {
		gl := videoTSEventLine("event", got[i])
		wl := videoTSEventLine("event", want[i])
		if gl != wl {
			bad = append(bad, fmt.Sprintf("  step %d event %d:\n    got  %s\n    want %s", stepIdx, i, videoTSEventLine(peer, got[i]), videoTSEventLine("want", want[i])))
		}
	}
	for i := minLen; i < len(got); i++ {
		bad = append(bad, fmt.Sprintf("  step %d: unexpected %s", stepIdx, videoTSEventLine(peer, got[i])))
	}
	for i := minLen; i < len(want); i++ {
		bad = append(bad, fmt.Sprintf("  step %d: missing %s", stepIdx, videoTSEventLine("want", want[i])))
	}
	return bad
}

func diffVideoFacts(got, want videoTSFacts, stepIdx int, peer string) []string {
	gl := videoTSFactsLine("facts", got)
	wl := videoTSFactsLine("facts", want)
	if gl == wl {
		return nil
	}
	return []string{fmt.Sprintf("  step %d facts mismatch:\n    got  %s\n    want %s", stepIdx, videoTSFactsLine(peer, got), videoTSFactsLine("want", want))}
}

// TestVideoDifferential_OnlyTheClassifiedDivergencesExist asserts that only the 5
// reviewed divergences exist in the video corpus.
func TestVideoDifferential_OnlyTheClassifiedDivergencesExist(t *testing.T) {
	cases := loadVideoCorpus(t)
	want := map[string]string{
		"h264_unreadable_slice_header":                                     "quirk",
		"h264_sei_longer_than_the_capture_budget_hides_the_recovery_point": "limitation",
		"hevc_long_sei_hides_the_recovery_point_and_blocks_entry":          "limitation",
		"mpeg2_predicted_pictures_are_not_entry_points":                    "quirk",
		"video_pes_header_reaching_past_its_packet":                        "divergence",
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

func requireVideoRealCore(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("XG2G_MEDIA_CORE_BIN")
	if bin == "" {
		t.Skip("XG2G_MEDIA_CORE_BIN not set; build media-core and point this at it")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("media core binary not usable: %v", err)
	}
	if os.Getenv("XG2G_TEST_ALLOW_DARWIN_CORE") == "1" {
		useIdentity(t, func(pid int) (processIdentity, error) {
			return &darwinTestIdentity{pid: pid}, nil
		})
		return bin
	}
	requireOwnableCore(t)
	return bin
}

// TestVideoDifferential_TheRealRustCoreAgreesCallByCall is the 3-way differential
// proof across all 94 corpus cases over real Unix domain socket IPC.
func TestVideoDifferential_TheRealRustCoreAgreesCallByCall(t *testing.T) {
	bin := requireVideoRealCore(t)
	cases := loadVideoCorpus(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	compared := 0
	divergencesSeen := map[string]string{}

	for _, c := range cases {
		if c.divergence != "" {
			divergencesSeen[c.name] = c.class
		}
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
				rustEvents := videoTSEventsOf(rustRes.Events)
				if bad := diffVideoEvents(rustEvents, step.authoredEvents, i+1, "rust"); len(bad) > 0 {
					t.Fatalf("step %d of %d in %s:\n%s", i+1, len(c.steps), c.name, strings.Join(bad, "\n"))
				}
				if step.authoredFacts != nil {
					rustFacts := videoTSFactsOf(rustRes.Facts)
					if bad := diffVideoFacts(rustFacts, *step.authoredFacts, i+1, "rust"); len(bad) > 0 {
						t.Fatalf("step %d of %d in %s:\n%s", i+1, len(c.steps), c.name, strings.Join(bad, "\n"))
					}
				}

				// 2. Go reference matches authored (non-divergent) or ref-event/ref-facts (divergent)
				wantGoEvents := step.expectedRefEvents(isDivergent)
				goEvents := videoTSEventsOf(goRes.Events)
				if bad := diffVideoEvents(goEvents, wantGoEvents, i+1, "go"); len(bad) > 0 {
					t.Fatalf("step %d of %d in %s:\n%s", i+1, len(c.steps), c.name, strings.Join(bad, "\n"))
				}
				if wantGoFacts := step.expectedRefFacts(isDivergent); wantGoFacts != nil {
					goFacts := videoTSFactsOf(goRes.Facts)
					if bad := diffVideoFacts(goFacts, *wantGoFacts, i+1, "go"); len(bad) > 0 {
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

	t.Logf("real-process video differential: %d cases, %d steps, all exact (0 unclassified divergences)",
		len(cases), compared)
}
