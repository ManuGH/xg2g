// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"errors"
	"reflect"
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
	0x02,                                           // coverage: Complete (wireCoverageComplete)
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
	// Video facts (81 bytes)
	0x00,                                           // video facts flags (no ps, no scrconf)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // clean_rap_count = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // clean_access_units = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // irap_points = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // intra_points = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // recovery_point_seis = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // predicted_rejected = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // unreadable_slices = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // video_scrambled = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // video_clear = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // video_clear_run = 0
	// Audio scrambling facts (24 bytes)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // audio_scrambled = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // audio_clear = 0
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // audio_clear_run = 0
	0x00, 0x00, // no PAT sections
	0x00, 0x00, // no PMT sections
}

// goldenFullResult carries one of everything: events of each kind, both tables, a video
// stream with facts, and an audio track with a complete declaration.
var goldenFullResult = []byte{
	0x00,                                           // status ok
	0x02,                                           // coverage: Complete
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // through = 188
	0x00, 0x00, 0x00, 0x03, // 3 events
	0x01,                                           // event 1: programme identity changed
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // offset 0
	0x00,                                           // flags 0
	0x02,                                           // event 2: random access point
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // offset 188
	0x01,                                           // flags 1 (joinable)
	0x03,                                           // event 3: random access point invalidated
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // offset 188
	0x00,       // flags 0
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
	0x02, // two channels
	0x02, // has a component type, not multichannel
	0x04, // component type 4
	// Audio observation for track 258 (11 bytes)
	0x06,                                           // channels: 6
	0x03,                                           // flags: OBS_FLAG_LFE | OBS_FLAG_HAS_ACMOD
	0x07,                                           // acmod: 7
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, 0xB4, // frames: 1460
	// Video facts (81 bytes)
	0x03,                                           // video facts flags: parameter_sets_seen | scrambled_confirmed
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // clean_rap_count = 1
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // clean_access_units = 2
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, // irap_points = 3
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, // intra_points = 4
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05, // recovery_point_seis = 5
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x06, // predicted_rejected = 6
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x07, // unreadable_slices = 7
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, // video_scrambled = 8
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09, // video_clear = 9
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A, // video_clear_run = 10
	// Audio scrambling facts (24 bytes)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0B, // audio_scrambled = 11
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, // audio_clear = 12
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, // audio_clear_run = 13
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
	if !got.Covers(mediafacts.ParseCoverageComplete) {
		t.Errorf("coverage %s, want complete", got.Coverage)
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
	if got.Facts.ParameterSetsSeen || got.Facts.ScrambledVideoConfirmed {
		t.Errorf("video facts non-zero in empty result: %+v", got.Facts)
	}
	if got.Facts.Scrambling.AudioScrambled != 0 || got.Facts.Scrambling.AudioClear != 0 || got.Facts.Scrambling.AudioClearRun != 0 {
		t.Errorf("audio scrambling non-zero in empty result: %+v", got.Facts.Scrambling)
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

	if !got.Covers(mediafacts.ParseCoverageComplete) {
		t.Errorf("coverage %s, want complete", got.Coverage)
	}
	if got.ProcessedThroughOffset != 188 {
		t.Errorf("through %d, want 188", got.ProcessedThroughOffset)
	}
	if len(got.Events) != 3 {
		t.Fatalf("events count = %d, want 3: %+v", len(got.Events), got.Events)
	}
	if got.Events[0].Kind != mediafacts.EventProgramIdentityChanged || got.Events[0].Offset != 0 || got.Events[0].Joinable {
		t.Errorf("event 0: %+v", got.Events[0])
	}
	if got.Events[1].Kind != mediafacts.EventRandomAccessPoint || got.Events[1].Offset != 188 || !got.Events[1].Joinable {
		t.Errorf("event 1: %+v", got.Events[1])
	}
	if got.Events[2].Kind != mediafacts.EventRandomAccessPointInvalidated || got.Events[2].Offset != 188 || got.Events[2].Joinable {
		t.Errorf("event 2: %+v", got.Events[2])
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
	if !reflect.DeepEqual(f.Scrambling.AudioPIDs, f.AudioPIDs) {
		t.Errorf("scrambling audio PIDs %v, want %v", f.Scrambling.AudioPIDs, f.AudioPIDs)
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
	if tr.Observed.Channels != 6 || !tr.Observed.LFE || !tr.Observed.HasAcmod || tr.Observed.Acmod != 7 || tr.Observed.Frames != 1460 {
		t.Errorf("observed %+v", tr.Observed)
	}

	// Video Facts verification
	if !f.ParameterSetsSeen {
		t.Error("ParameterSetsSeen = false, want true")
	}
	if !f.ScrambledVideoConfirmed {
		t.Error("ScrambledVideoConfirmed = false, want true")
	}
	if f.CleanEntryPoints != 1 {
		t.Errorf("CleanEntryPoints = %d, want 1", f.CleanEntryPoints)
	}
	if f.CleanAccessUnits != 2 {
		t.Errorf("CleanAccessUnits = %d, want 2", f.CleanAccessUnits)
	}
	if f.RandomAccess.IRAPPoints != 3 {
		t.Errorf("IRAPPoints = %d, want 3", f.RandomAccess.IRAPPoints)
	}
	if f.RandomAccess.IntraPoints != 4 {
		t.Errorf("IntraPoints = %d, want 4", f.RandomAccess.IntraPoints)
	}
	if f.RandomAccess.RecoveryPointSEIs != 5 {
		t.Errorf("RecoveryPointSEIs = %d, want 5", f.RandomAccess.RecoveryPointSEIs)
	}
	if f.RandomAccess.PredictedRejected != 6 {
		t.Errorf("PredictedRejected = %d, want 6", f.RandomAccess.PredictedRejected)
	}
	if f.RandomAccess.UnreadableSlices != 7 {
		t.Errorf("UnreadableSlices = %d, want 7", f.RandomAccess.UnreadableSlices)
	}
	if f.Scrambling.VideoScrambled != 8 {
		t.Errorf("VideoScrambled = %d, want 8", f.Scrambling.VideoScrambled)
	}
	if f.Scrambling.VideoClear != 9 {
		t.Errorf("VideoClear = %d, want 9", f.Scrambling.VideoClear)
	}
	if f.Scrambling.VideoClearRun != 10 {
		t.Errorf("VideoClearRun = %d, want 10", f.Scrambling.VideoClearRun)
	}

	// Audio Scrambling verification
	if f.Scrambling.AudioScrambled != 11 {
		t.Errorf("AudioScrambled = %d, want 11", f.Scrambling.AudioScrambled)
	}
	if f.Scrambling.AudioClear != 12 {
		t.Errorf("AudioClear = %d, want 12", f.Scrambling.AudioClear)
	}
	if f.Scrambling.AudioClearRun != 13 {
		t.Errorf("AudioClearRun = %d, want 13", f.Scrambling.AudioClearRun)
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

	// The offsets below are counted from the layout, not guessed:
	// status 0, coverage 1, through 2..9, event count 10..13,
	// event 1 (14..23: 14 kind, 15..22 offset, 23 flags),
	// event 2 (24..33: 24 kind, 25..32 offset, 33 flags),
	// event 3 (34..43: 34 kind, 35..42 offset, 43 flags),
	// facts flags 44, version 45, programme 46..47, PMT PID 48..49, video PID 50..51,
	// video codec 52, audio PID count 53..56, audio PID 57..58,
	// track count 59..62,
	// track 63..83 (63..64 PID, 65 stream type, 66 codec, 67..69 lang, 70 channels, 71 flags, 72 comp, 73 obs_channels, 74 obs_flags, 75 obs_acmod, 76..83 obs_frames),
	// video facts 84..164 (84 flags, 85..92 clean_rap, 93..100 clean_au, 101..108 irap, 109..116 intra, 117..124 rpsei, 125..132 predrej, 133..140 unread, 141..148 vscr, 149..156 vclr, 157..164 vrun),
	// audio scrambling 165..188 (165..172 ascr, 173..180 aclr, 181..188 arun),
	// PAT sections 189..
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
		{"coverage psi-only", replace(1, 0x01)},
		{"coverage psi-video", replace(1, 0x03)},
		{"coverage nobody defined", replace(1, 0x7F)},
		{"an offset past what an offset can be", replace(2, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"an event kind this build does not know", replace(14, 0x04)},
		{"event 1 (identity) with non-zero offset", replace(15, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01)},
		{"event 1 (identity) with non-zero flags", replace(23, 0x01)},
		{"event 2 (rap) with invalid flag bits", replace(33, 0x02)},
		{"event 3 (rap invalidated) with non-zero flags", replace(43, 0x01)},
		{"facts flags nobody defined", replace(44, 0x80)},
		{"a video codec this build does not know", replace(52, 0x7F)},
		{"an audio codec this build does not know", replace(66, 0x7F)},
		{"track flags nobody defined", replace(71, 0x80)},
		{"track observed flags nobody defined", replace(74, 0x80)},
		{"video facts flags nobody defined", replace(84, 0x80)},
		{"truncated in video facts block", good[:100]},
		{"truncated in audio scrambling block", good[:170]},
		{"truncated before the sections", good[:len(good)-8]},
		{"truncated mid-section", good[:len(good)-2]},
		{"trailing bytes after the result", append(append([]byte(nil), good...), 0x00)},
		{"more events than the frame can hold", replace(10, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio PIDs than the frame can hold", replace(53, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio tracks than the frame can hold", replace(59, 0xFF, 0xFF, 0xFF, 0xFF)},
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
