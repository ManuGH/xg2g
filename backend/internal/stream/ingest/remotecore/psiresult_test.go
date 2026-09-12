// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The result envelopes, byte for byte, as media-core/src/ipc.rs builds them.
//
// The same literals appear in that file's tests. Two implementations of one
// format drift, and they drift silently: each side keeps passing its own tests
// while agreeing on something the other no longer sends. Pinning both to one
// literal is what turns an edit on either side into a red test rather than a
// protocol nobody can read.

// goldenEmptyResult is a core that has read nothing: no tables, no events.
var goldenEmptyResult = []byte{
	0x00,                                           // status ok
	0x01,                                           // coverage: PSI only
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x2A, // through = 1066
	0x00, 0x00, 0x00, 0x00, // no events
	0x00,       // no PAT, no PMT
	0x00,       // PMT version 0
	0x00, 0x00, // programme 0
	0x00, 0x00, // PMT PID 0
	0x00, 0x00, // video PID 0
	0x00,                   // video codec unknown
	0x00, 0x00, 0x00, 0x00, // no audio PIDs
	0x00, 0x00, 0x00, 0x00, // no audio tracks
	0x00, 0x00, // no PAT sections
	0x00, 0x00, // no PMT sections
}

// goldenFullResult carries one of everything: an event, both tables, a video
// stream, and an audio track with a complete declaration.
var goldenFullResult = []byte{
	0x00,                                           // status ok
	0x01,                                           // coverage: PSI only
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // through = 188
	0x00, 0x00, 0x00, 0x01, // one event
	0x01,                                           // programme identity changed
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // offset 0
	0x00,       // not joinable
	0x03,       // has PAT and PMT
	0x05,       // PMT version 5
	0x00, 0x01, // programme 1
	0x01, 0x00, // PMT PID 256
	0x01, 0x01, // video PID 257
	0x01,                   // H.264
	0x00, 0x00, 0x00, 0x01, // one audio PID
	0x01, 0x02, // PID 258
	0x00, 0x00, 0x00, 0x01, // one audio track
	0x01, 0x02, // PID 258
	0x06,          // stream type 0x06
	0x03,          // ac3
	'd', 'e', 'u', // language
	0x02,       // two channels
	0x02,       // has a component type, not multichannel
	0x04,       // component type 4
	0x00, 0x01, // one PAT section
	0x00, 0x04, // four bytes
	0x00, 0xB0, 0x0D, 0x99,
	0x00, 0x01, // one PMT section
	0x00, 0x03, // three bytes
	0x02, 0xB0, 0x21,
}

func TestPSIResult_TheEmptyEnvelopeIsReadExactlyAsAgreed(t *testing.T) {
	got, err := decodePSIResult(goldenEmptyResult)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Covers(mediafacts.ParseCoveragePSIOnly) {
		t.Errorf("coverage %s, want psi-only", got.Coverage)
	}
	if got.ProcessedThroughOffset != 1066 {
		t.Errorf("through %d, want 1066", got.ProcessedThroughOffset)
	}
	if len(got.Events) != 0 {
		t.Errorf("%d events, want none", len(got.Events))
	}
	if got.Facts.HasPAT || got.Facts.HasPMT {
		t.Errorf("facts claim a table: %+v", got.Facts)
	}
	if len(got.PSI.PATSections) != 0 || len(got.PSI.PMTSections) != 0 {
		t.Errorf("sections present in an empty result")
	}
}

func TestPSIResult_TheFullEnvelopeIsReadExactlyAsAgreed(t *testing.T) {
	got, err := decodePSIResult(goldenFullResult)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.ProcessedThroughOffset != 188 {
		t.Errorf("through %d, want 188", got.ProcessedThroughOffset)
	}
	if len(got.Events) != 1 || got.Events[0].Kind != mediafacts.EventProgramIdentityChanged {
		t.Fatalf("events %+v", got.Events)
	}
	if got.Events[0].Offset != 0 || got.Events[0].Joinable {
		t.Errorf("event %+v, want offset 0 and not joinable", got.Events[0])
	}

	f := got.Facts
	if !f.HasPAT || !f.HasPMT {
		t.Errorf("facts %+v, want both tables", f)
	}
	if f.PMTVersion != 5 || f.ProgramNumber != 1 || f.PMTPID != 256 {
		t.Errorf("facts %+v", f)
	}
	if f.VideoPID != 257 || f.VideoCodec != mediafacts.CodecH264 {
		t.Errorf("video %d/%s", f.VideoPID, f.VideoCodec)
	}
	if len(f.AudioPIDs) != 1 || f.AudioPIDs[0] != 258 {
		t.Errorf("audio PIDs %v", f.AudioPIDs)
	}
	if len(f.AudioTracks) != 1 {
		t.Fatalf("%d audio tracks", len(f.AudioTracks))
	}
	tr := f.AudioTracks[0]
	if tr.PID != 258 || tr.StreamType != 0x06 || tr.Codec != "ac3" || tr.Language != "deu" {
		t.Errorf("track %+v", tr)
	}
	if tr.Declared.Channels != 2 || tr.Declared.Multichannel {
		t.Errorf("declaration %+v", tr.Declared)
	}
	if !tr.Declared.HasComponentType || tr.Declared.ComponentType != 4 {
		t.Errorf("component type %+v", tr.Declared)
	}

	// The section bytes come back exactly, which is the whole point of carrying
	// them rather than re-deriving them on this side.
	if len(got.PSI.PATSections) != 1 || !bytes.Equal(got.PSI.PATSections[0], []byte{0x00, 0xB0, 0x0D, 0x99}) {
		t.Errorf("PAT sections %x", got.PSI.PATSections)
	}
	if len(got.PSI.PMTSections) != 1 || !bytes.Equal(got.PSI.PMTSections[0], []byte{0x02, 0xB0, 0x21}) {
		t.Errorf("PMT sections %x", got.PSI.PMTSections)
	}
}

// TestPSIResult_APeerThatIsFailingIsRefused is the decoder held to the one thing
// it exists for. The peer is this process's own child, which is exactly the
// thing that can be wrong in ways nothing else is: a crash mid-write, a build
// that does not match, a version that slipped through.
func TestPSIResult_APeerThatIsFailingIsRefused(t *testing.T) {
	// A body that decodes cleanly, so each case below differs from it in one way.
	good := append([]byte(nil), goldenFullResult...)

	// The offsets below are counted from the layout, not guessed: status 0,
	// coverage 1, through 2, event count 10, the event 14, facts flags 24,
	// version 25, programme 26, PMT PID 28, video PID 30, video codec 32, audio
	// PID count 33, track count 39, the track 43. A wrong one here would make a
	// case pass for a reason that has nothing to do with its name.
	replace := func(at int, with ...byte) []byte {
		out := append([]byte(nil), good...)
		copy(out[at:], with)
		return out
	}

	sections := func(count int, length int) []byte {
		out := append([]byte(nil), goldenEmptyResult[:len(goldenEmptyResult)-4]...)
		out = append(out, byte(count>>8), byte(count))
		for i := 0; i < count; i++ {
			out = append(out, byte(length>>8), byte(length))
			out = append(out, make([]byte, length)...)
		}
		out = append(out, 0x00, 0x00) // no PMT sections
		return out
	}

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"empty body", nil},
		{"status only", good[:1]},
		{"coverage unknown", replace(1, 0x00)},
		{"coverage complete", replace(1, 0x02)},
		{"coverage nobody defined", replace(1, 0x7F)},
		{"an offset past what an offset can be", replace(2, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"an event kind this build does not know", replace(14, 0x02)},
		{"event flags nobody defined", replace(23, 0x80)},
		{"facts flags nobody defined", replace(24, 0x80)},
		{"a video codec this build does not know", replace(32, 0x7F)},
		{"an audio codec this build does not know", replace(46, 0x7F)},
		{"track flags nobody defined", replace(51, 0x80)},
		{"truncated before the sections", good[:len(good)-8]},
		{"truncated mid-section", good[:len(good)-2]},
		{"trailing bytes after the result", append(append([]byte(nil), good...), 0x00)},
		{"more events than the frame can hold", replace(10, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio PIDs than the frame can hold", replace(33, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio tracks than the frame can hold", replace(39, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more sections than a table may have", sections(mediafacts.MaxSectionsPerTable+1, 3)},
		{"a section longer than a section may be", sections(1, mediafacts.MaxSectionBytes+1)},
		{"a section shorter than a section may be", sections(1, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodePSIResult(tc.body)
			if err == nil {
				t.Fatal("a peer that is failing was accepted")
			}
			if !errors.Is(err, mediafacts.ErrCoreInvalidResponse) {
				t.Errorf("error %v, want ErrCoreInvalidResponse", err)
			}
		})
	}
}

// TestPSIResult_ASectionListAtItsBoundIsRead is the other half of the bound: a
// check that refused everything would pass the table above and break the
// product.
func TestPSIResult_ASectionListAtItsBoundIsRead(t *testing.T) {
	body := append([]byte(nil), goldenEmptyResult[:len(goldenEmptyResult)-4]...)
	count := mediafacts.MaxSectionsPerTable
	length := mediafacts.MaxSectionBytes
	body = append(body, byte(count>>8), byte(count))
	for i := 0; i < count; i++ {
		body = append(body, byte(length>>8), byte(length))
		body = append(body, make([]byte, length)...)
	}
	body = append(body, 0x00, 0x00)

	got, err := decodePSIResult(body)
	if err != nil {
		t.Fatalf("the largest table a syntax-conforming core can send was refused: %v", err)
	}
	if len(got.PSI.PATSections) != count {
		t.Errorf("%d sections, want %d", len(got.PSI.PATSections), count)
	}
}
