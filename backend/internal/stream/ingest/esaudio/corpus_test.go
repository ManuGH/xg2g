// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared observer corpus.
//
// Every case is a sequence of Feed calls and the observation expected after each
// one of them. Both are written out here by hand: the point of a corpus that a
// second implementation has to satisfy is that it states the semantics, not that
// it records whatever the first implementation happens to do. The Go observer is
// then checked against these expectations, and the same file is checked into the
// repository for the Rust observer to be checked against.
//
// Feed boundaries are part of the case, not an implementation detail. The
// observer carries a partial header across a call and skips frame payload that
// ran past the end of one, so "the same bytes, split differently" is a different
// input - which is why the file records a list of feeds rather than one blob.

var updateCorpus = flag.Bool("update-corpus", false, "rewrite the checked-in observer corpus")

// corpusPath is the shared location both languages read.
const corpusPath = "../../../../../testdata/audio-corpus/corpus.txt"

const corpusFormatVersion = 1

type feedStep struct {
	feed []byte
	want Observation
}

type corpusCase struct {
	name  string
	desc  string
	steps []feedStep
}

// --- frame builders -------------------------------------------------------
//
// byte 6 of an AC-3 frame is written out per acmod rather than computed with the
// same offset arithmetic the parser uses, so a mistake in that arithmetic cannot
// hide behind a corpus that repeats it. lfeon sits behind the mix level fields
// that acmod itself decides the presence of; the bit it lands on is named here.
//
//	acmod 0 (1+1)  no mixlev fields          lfeon at bit 4
//	acmod 1 (1/0)  no mixlev fields          lfeon at bit 4
//	acmod 2 (2/0)  dsurmod                   lfeon at bit 2
//	acmod 3 (3/0)  cmixlev                   lfeon at bit 2
//	acmod 4 (2/1)  surmixlev                 lfeon at bit 2
//	acmod 5 (3/1)  cmixlev + surmixlev       lfeon at bit 0
//	acmod 6 (2/2)  surmixlev                 lfeon at bit 2
//	acmod 7 (3/2)  cmixlev + surmixlev       lfeon at bit 0
var ac3LFEBit = [8]uint{4, 4, 2, 2, 2, 0, 2, 0}

func ac3Byte6(acmod uint8, lfe bool) byte {
	b := acmod << 5
	if lfe {
		b |= 1 << ac3LFEBit[acmod]
	}
	return b
}

// ac3FrameWith builds an AC-3 syncframe with the syncinfo fields named. The
// payload is left zero: nothing in the header depends on it.
func ac3FrameWith(fscod, frmsizecod, bsid uint8, byte6 byte, size int) []byte {
	f := make([]byte, size)
	f[0], f[1] = 0x0B, 0x77
	f[2], f[3] = 0xAA, 0xBB // crc1, not checked
	f[4] = fscod<<6 | frmsizecod
	f[5] = bsid << 3
	f[6] = byte6
	return f
}

// ac3 builds the smallest legal 48 kHz frame for a layout: 128 bytes.
func ac3(acmod uint8, lfe bool) []byte {
	return ac3FrameWith(0x00, 0x00, 8, ac3Byte6(acmod, lfe), 128)
}

// ac3Channels is what a layout is worth, from A/52 Table 5.8 plus LFE.
func ac3Channels(acmod uint8, lfe bool) int {
	n := [8]int{2, 1, 2, 3, 3, 4, 4, 5}[acmod]
	if lfe {
		n++
	}
	return n
}

// eac3 builds an E-AC-3 syncframe of the given size.
func eac3(strmtyp, substreamID, acmod uint8, lfe bool, size int) []byte {
	frmsiz := uint16(size/2 - 1)
	f := make([]byte, size)
	f[0], f[1] = 0x0B, 0x77
	f[2] = strmtyp<<6 | substreamID<<3 | byte(frmsiz>>8)
	f[3] = byte(frmsiz & 0xFF)
	f[4] = acmod<<1 | boolBit(lfe)
	f[5] = 16 << 3 // bsid 16 selects the Annex E syntax
	return f
}

func boolBit(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func join(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// established is the observation after n frames of one layout have settled.
func established(acmod uint8, lfe bool, frames uint64) Observation {
	return Observation{
		Channels: ac3Channels(acmod, lfe),
		LFE:      lfe,
		Acmod:    acmod,
		HasAcmod: true,
		Frames:   frames,
	}
}

// nothing is the observation of a stream that has not said anything yet, with a
// frame count that may already be non-zero.
func nothing(frames uint64) Observation { return Observation{Frames: frames} }

// corpusCases is the corpus itself.
func corpusCases() []corpusCase {
	var cases []corpusCase

	// Every AC-3 layout, with and without LFE. Three agreeing frames in one feed
	// is the shortest run that establishes anything.
	for acmod := uint8(0); acmod < 8; acmod++ {
		for _, lfe := range []bool{false, true} {
			f := ac3(acmod, lfe)
			suffix := "nolfe"
			if lfe {
				suffix = "lfe"
			}
			cases = append(cases, corpusCase{
				name: fmt.Sprintf("ac3_acmod%d_%s", acmod, suffix),
				desc: fmt.Sprintf("A/52 acmod %d, lfeon %v: %d channels", acmod, lfe, ac3Channels(acmod, lfe)),
				steps: []feedStep{
					{feed: join(f, f, f), want: established(acmod, lfe, 3)},
				},
			})
		}
	}

	// The mix level fields sit between acmod and lfeon and say nothing about the
	// layout. A frame that fills them must read as the same layout as one that
	// does not.
	f51WithMixLevels := ac3FrameWith(0x00, 0x00, 8, 0xEB, 128) // acmod 7, cmixlev 01, surmixlev 01, lfeon 1
	cases = append(cases, corpusCase{
		name: "ac3_51_mix_levels_do_not_move_the_answer",
		desc: "acmod 7 with cmixlev and surmixlev set reads as 5.1, same as with them clear",
		steps: []feedStep{
			{feed: join(f51WithMixLevels, f51WithMixLevels, f51WithMixLevels), want: established(7, true, 3)},
		},
	})

	// Anything the syntax does not define is not a frame. A byte pair that looked
	// like a syncword is the likelier explanation, and acting on it would put a
	// layout into the facts that no stream ever carried.
	reserved := []struct {
		name  string
		desc  string
		frame []byte
	}{
		{
			"ac3_reserved_fscod",
			"fscod 3 is reserved: not a frame, not a frame with unknown fields",
			ac3FrameWith(0x03, 0x00, 8, ac3Byte6(7, true), 128),
		},
		{
			"ac3_reserved_frmsizecod",
			"frmsizecod above 37 is reserved",
			ac3FrameWith(0x00, 38, 8, ac3Byte6(7, true), 128),
		},
		{
			"ac3_reserved_bsid",
			"bsid above 16 is a bitstream this parser has never heard of",
			ac3FrameWith(0x00, 0x00, 17, ac3Byte6(7, true), 128),
		},
		{
			"eac3_reserved_strmtyp",
			"strmtyp 3 is reserved",
			eac3(0x03, 0, 7, true, 128),
		},
	}
	for _, r := range reserved {
		cases = append(cases, corpusCase{
			name: r.name,
			desc: r.desc,
			steps: []feedStep{
				{feed: join(r.frame, r.frame, r.frame), want: nothing(0)},
			},
		})
	}

	// E-AC-3.
	eacIndependent := func(acmod uint8, lfe bool) []byte { return eac3(0x00, 0, acmod, lfe, 128) }
	cases = append(cases,
		corpusCase{
			name: "eac3_independent_51",
			desc: "strmtyp 0, substream 0: this track's own layout",
			steps: []feedStep{{
				feed: join(eacIndependent(7, true), eacIndependent(7, true), eacIndependent(7, true)),
				want: established(7, true, 3),
			}},
		},
		corpusCase{
			name: "eac3_independent_stereo",
			desc: "the same, at 2/0",
			steps: []feedStep{{
				feed: join(eacIndependent(2, false), eacIndependent(2, false), eacIndependent(2, false)),
				want: established(2, false, 3),
			}},
		},
		corpusCase{
			name: "eac3_dependent_is_flagged_not_counted",
			desc: "a dependent substream extends the independent one; its acmod is not the programme's total",
			steps: []feedStep{{
				feed: join(eac3(0x01, 0, 7, true, 128), eac3(0x01, 0, 7, true, 128)),
				want: Observation{DependentSubstream: true},
			}},
		},
		corpusCase{
			name: "eac3_foreign_substream_carries_no_count",
			desc: "an independent substream other than 0 is a different programme in the same elementary stream",
			steps: []feedStep{{
				feed: join(eac3(0x00, 1, 7, true, 128), eac3(0x00, 1, 7, true, 128), eac3(0x00, 1, 7, true, 128)),
				want: nothing(0),
			}},
		},
		corpusCase{
			name: "eac3_dependent_between_independents_does_not_break_the_run",
			desc: "a dependent frame is not a disagreeing frame; the run through it still establishes",
			steps: []feedStep{
				{feed: eacIndependent(7, true), want: nothing(1)},
				{feed: eac3(0x01, 0, 2, false, 128), want: Observation{DependentSubstream: true, Frames: 1}},
				{feed: eacIndependent(7, true), want: Observation{DependentSubstream: true, Frames: 2}},
				{feed: eacIndependent(7, true), want: withDependent(established(7, true, 3))},
			},
		},
	)

	return append(cases, streamingCases()...)
}

func withDependent(o Observation) Observation {
	o.DependentSubstream = true
	return o
}

// streamingCases are the ones about where the bytes were cut, and about the
// state that survives a cut.
func streamingCases() []corpusCase {
	stereo := ac3(2, false)
	surround := ac3(7, true)
	garbage := bytes.Repeat([]byte{0xFF}, 5)

	var cases []corpusCase

	// A header cut at every byte a header can be cut at. The first feed cannot
	// establish anything - it does not contain a whole header - and the second
	// has to complete it from what was carried rather than start over.
	for k := 1; k < headerBytes; k++ {
		cases = append(cases, corpusCase{
			name: fmt.Sprintf("ac3_header_split_after_%d_bytes", k),
			desc: fmt.Sprintf("a syncframe header cut after %d of %d header bytes is completed from the carry", k, headerBytes),
			steps: []feedStep{
				{feed: surround[:k], want: nothing(0)},
				{feed: join(surround[k:], surround, surround), want: established(7, true, 3)},
			},
		})
	}

	cases = append(cases,
		corpusCase{
			name: "ac3_frame_payload_crosses_the_feed_boundary",
			desc: "a frame whose payload runs past the end of a feed is stepped over, not rescanned",
			steps: []feedStep{
				{feed: surround[:100], want: nothing(1)},
				{feed: join(surround[100:], surround, surround), want: established(7, true, 3)},
			},
		},
		corpusCase{
			name: "ac3_garbage_before_the_first_sync",
			desc: "bytes that are not a frame are skipped one at a time until one is",
			steps: []feedStep{
				{feed: join(garbage, surround, surround, surround), want: established(7, true, 3)},
			},
		},
		corpusCase{
			name: "ac3_sync_pattern_inside_frame_payload_is_not_a_frame",
			desc: "compressed audio contains the sync pattern by chance; stepping over the frame is what stops it becoming a layout",
			steps: []feedStep{
				{feed: join(withEmbeddedSync(surround), withEmbeddedSync(surround), withEmbeddedSync(surround)),
					want: established(7, true, 3)},
			},
		},
		corpusCase{
			name: "ac3_truncated_trailing_frame",
			desc: "a frame that has only begun when the stream stops changes nothing",
			steps: []feedStep{
				{feed: join(surround, surround, surround, surround[:3]), want: established(7, true, 3)},
			},
		},
		corpusCase{
			name: "ac3_two_agreeing_frames_are_not_enough",
			desc: "two frames are a guess; three are the run this observer requires",
			steps: []feedStep{
				{feed: stereo, want: nothing(1)},
				{feed: stereo, want: nothing(2)},
				{feed: stereo, want: established(2, false, 3)},
			},
		},
		corpusCase{
			name: "ac3_interrupted_run_starts_over",
			desc: "a disagreeing frame restarts the candidate; nothing is established until a whole run agrees",
			steps: []feedStep{
				{feed: stereo, want: nothing(1)},
				{feed: stereo, want: nothing(2)},
				{feed: surround, want: nothing(3)},
				{feed: stereo, want: nothing(4)},
				{feed: stereo, want: nothing(5)},
				{feed: stereo, want: established(2, false, 6)},
			},
		},
		corpusCase{
			name: "ac3_stereo_to_51_without_a_reset",
			desc: "the same PID going from 2.0 to 5.1 between programmes; the old layout stands until the new one is proved",
			steps: []feedStep{
				{feed: join(stereo, stereo, stereo), want: established(2, false, 3)},
				{feed: surround, want: established(2, false, 4)},
				{feed: surround, want: established(2, false, 5)},
				{feed: surround, want: established(7, true, 6)},
			},
		},
		corpusCase{
			name: "ac3_51_to_stereo_without_a_reset",
			desc: "and back again, which is the direction that matters for a downmix decision",
			steps: []feedStep{
				{feed: join(surround, surround, surround), want: established(7, true, 3)},
				{feed: stereo, want: established(7, true, 4)},
				{feed: stereo, want: established(7, true, 5)},
				{feed: stereo, want: established(2, false, 6)},
			},
		},
		corpusCase{
			name: "ac3_resumes_after_garbage_mid_stream",
			desc: "noise between frames costs the run, not the observer",
			steps: []feedStep{
				{feed: join(stereo, stereo), want: nothing(2)},
				{feed: garbage, want: nothing(2)},
				{feed: join(stereo, stereo, stereo), want: established(2, false, 5)},
			},
		},
	)
	return cases
}

// withEmbeddedSync puts a plausible stereo header inside a frame's payload. A
// parser that rescans frame payload finds it; one that steps over the frame does
// not.
func withEmbeddedSync(frame []byte) []byte {
	out := append([]byte(nil), frame...)
	copy(out[64:], ac3(2, false)[:headerBytes])
	return out
}

// --- class A: same segmentation -------------------------------------------
//
// The corpus fixes both the bytes and where they were cut. Every implementation
// fed exactly these slices must answer exactly these observations. This is the
// test the Rust observer has to pass against the same file.

func TestCorpus_TheGoObserverMeetsTheAuthoredExpectations(t *testing.T) {
	for _, c := range corpusCases() {
		t.Run(c.name, func(t *testing.T) {
			o := NewObserver()
			for i, step := range c.steps {
				o.Feed(step.feed)
				if got := o.Current(); got != step.want {
					t.Fatalf("after feed %d of %d (%d bytes):\n got %+v\nwant %+v",
						i+1, len(c.steps), len(step.feed), got, step.want)
				}
			}
		})
	}
}

func TestCorpus_TheCheckedInFileMatchesTheCases(t *testing.T) {
	want := renderCorpus(corpusCases())

	if *updateCorpus {
		if err := os.MkdirAll(filepath.Dir(corpusPath), 0o755); err != nil {
			t.Fatalf("create corpus directory: %v", err)
		}
		if err := os.WriteFile(corpusPath, []byte(want), 0o644); err != nil { // #nosec G306 -- a checked-in fixture
			t.Fatalf("write corpus: %v", err)
		}
		t.Logf("wrote %s", corpusPath)
		return
	}

	got, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("read corpus: %v\nrun: go test ./internal/stream/ingest/esaudio/ -run TestCorpus -update-corpus", err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date; run:\n"+
			"  go test ./internal/stream/ingest/esaudio/ -run TestCorpus -update-corpus\n"+
			"and review the diff - the Rust observer is checked against this file", corpusPath)
	}
}

// renderCorpus writes the corpus in a line format both languages can read
// without a parser library, and a reviewer can read without either.
func renderCorpus(cases []corpusCase) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# xg2g AC-3 / E-AC-3 observer corpus, format version %d\n", corpusFormatVersion)
	b.WriteString("#\n")
	b.WriteString("# Generated. To change it, edit corpusCases() in\n")
	b.WriteString("# backend/internal/stream/ingest/esaudio/corpus_test.go and run\n")
	b.WriteString("#   go test ./internal/stream/ingest/esaudio/ -run TestCorpus -update-corpus\n")
	b.WriteString("#\n")
	b.WriteString("# Each case is a sequence of feeds and the observation expected after each\n")
	b.WriteString("# one. The feed boundaries are part of the case: an observer carries a partial\n")
	b.WriteString("# header across a call and skips frame payload that ran past the end of one,\n")
	b.WriteString("# so the same bytes cut differently are a different input.\n")
	fmt.Fprintf(&b, "version %d\n", corpusFormatVersion)
	for _, c := range cases {
		b.WriteString("\ncase " + c.name + "\n")
		b.WriteString("  desc " + c.desc + "\n")
		for _, s := range c.steps {
			b.WriteString("  feed " + hex.EncodeToString(s.feed) + "\n")
			b.WriteString("  want " + renderObservation(s.want) + "\n")
		}
		b.WriteString("end\n")
	}
	return b.String()
}

func renderObservation(o Observation) string {
	return fmt.Sprintf("channels=%d lfe=%d acmod=%d hasAcmod=%d dependent=%d frames=%d",
		o.Channels, boolBit(o.LFE), o.Acmod, boolBit(o.HasAcmod), boolBit(o.DependentSubstream), o.Frames)
}

// --- class B: segmentation invariance -------------------------------------
//
// A different question, kept apart from the corpus on purpose: the same logical
// elementary stream, cut in every way, has to end at the same observation. This
// is about the observer, not about cross-language agreement - the corpus above
// is what proves that, and mixing the two would let a segmentation bug hide as a
// parity bug or the other way round.

func TestObserver_TheFinalAnswerDoesNotDependOnWhereTheBytesWereCut(t *testing.T) {
	stream := join(
		bytes.Repeat([]byte{0xFF}, 3),
		ac3(2, false), ac3(2, false), ac3(2, false),
		ac3(7, true), ac3(7, true), ac3(7, true),
	)
	want := established(7, true, 6)

	for _, size := range []int{1, 2, 3, 5, 7, 13, 64, 127, 128, 129, 200, 512, len(stream)} {
		t.Run(fmt.Sprintf("chunks_of_%d", size), func(t *testing.T) {
			o := NewObserver()
			for i := 0; i < len(stream); i += size {
				end := i + size
				if end > len(stream) {
					end = len(stream)
				}
				o.Feed(stream[i:end])
			}
			if got := o.Current(); got != want {
				t.Errorf("cut into %d-byte feeds:\n got %+v\nwant %+v", size, got, want)
			}
		})
	}
}
