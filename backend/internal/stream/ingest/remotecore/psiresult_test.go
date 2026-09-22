// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"encoding/binary"
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

// goldenEmptyResult is a core that has read nothing: no tables, no events, no timing records.
var goldenEmptyResult = []byte{
	0x00,                                           // status ok
	0x02,                                           // coverage: Complete (wireCoverageComplete)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x2A, // through = 1066
	0x00, 0x04, // 4 sections
	// Section 1: EVENTS (12 bytes)
	0x01,       // SECTION_EVENTS
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x04, // length: 4
	0x00, 0x00, 0x00, 0x00, // no events
	// Section 2: FACTS (130 bytes)
	0x02,       // SECTION_FACTS
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x7A, // length: 122 (0x7A)
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
	// Section 3: ACTIVE_PSI (12 bytes)
	0x03,       // SECTION_ACTIVE_PSI
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x04, // length: 4
	0x00, 0x00, // no PAT sections
	0x00, 0x00, // no PMT sections
	// Section 4: TIMING (21 bytes)
	0x04,       // SECTION_TIMING
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x0D, // length: 13 (0x0D)
	0x00,                                           // has_active_epoch: false
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // active_epoch: 0
	0x00, 0x00, 0x00, 0x00, // record_count: 0
}

// goldenFullResult carries one of everything: events of each kind, both tables, a video
// stream with facts, an audio track with a complete declaration, and timing records.
var goldenFullResult = []byte{
	0x00,                                           // status ok
	0x02,                                           // coverage: Complete
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // through = 188
	0x00, 0x04, // 4 sections
	// Section 1: EVENTS (42 bytes)
	0x01,       // SECTION_EVENTS
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x22, // length = 34 (4 + 3 * 10)
	0x00, 0x00, 0x00, 0x03, // 3 events
	0x01,                                           // event 1: program identity changed
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // offset 0
	0x00,                                           // flags 0
	0x02,                                           // event 2: random access point
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // offset 188
	0x01,                                           // flags 1 (joinable)
	0x03,                                           // event 3: random access point invalidated
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // offset 188
	0x00, // flags 0
	// Section 2: FACTS (153 bytes)
	0x02,       // SECTION_FACTS
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x91, // length = 145 (0x91)
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
	// Audio observation for track 258 (11 bytes):
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
	// Section 3: ACTIVE_PSI (23 bytes)
	0x03,       // SECTION_ACTIVE_PSI
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x0F, // length = 15 (0x0F)
	0x00, 0x01, // one PAT section
	0x00, 0x04, // four bytes
	0x00, 0xB0, 0x0D, 0x99, //
	0x00, 0x01, // one PMT section
	0x00, 0x03, // three bytes
	0x02, 0xB0, 0x21, //
	// Section 4: TIMING (122 bytes)
	0x04,       // SECTION_TIMING
	0x01,       // SECTION_VERSION_V1
	0x00, 0x01, // flags: CRITICAL
	0x00, 0x00, 0x00, 0x72, // length = 114 (0x72)
	0x01,                                           // has_active_epoch = true
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // active_epoch = 1
	0x00, 0x00, 0x00, 0x03, // 3 records
	// Record 1: PES_TIMING (44B)
	0x01,                                           // RECORD_PES_TIMING
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // epoch = 1
	0x01, 0x01, // pid = 257
	0x03,                                           // flags: HAS_PTS | HAS_DTS
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // observed_at = 188
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // subject_at = 188
	0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x5F, 0x90, // pts_90k = 90000
	0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x51, 0x80, // dts_90k = 86400
	// Record 2: PCR (27B)
	0x02,                                           // RECORD_PCR
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // epoch = 1
	0x01, 0x00, // pcr_pid = 256
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // observed_at = 188
	0x00, 0x00, 0x00, 0x00, 0x01, 0x9B, 0xFC, 0xC0, // extended_pcr_27m = 27000000
	// Record 3: DISCONTINUITY (30B)
	0x03,       // RECORD_DISCONTINUITY
	0x01,       // scope = Program (1)
	0x00, 0x00, // track_pid = 0
	0x03,                                           // reason = PcrDiscontinuityIndicator (3)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xBC, // observed_at = 188
	0x03,                                           // flags: HAS_EPOCH_BEFORE | HAS_EPOCH_AFTER
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // epoch_before = 1
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // epoch_after = 2
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

	// Timing verification
	if got.Timing.Authority != mediafacts.TimingAuthorityCanonical {
		t.Errorf("timing authority %s, want canonical", got.Timing.Authority)
	}
	if got.Timing.HasActiveEpoch {
		t.Errorf("timing has active epoch in empty result")
	}
	if got.Timing.ActiveEpoch != 0 {
		t.Errorf("timing active epoch %d, want 0", got.Timing.ActiveEpoch)
	}
	if len(got.Timing.Records) != 0 {
		t.Errorf("%d timing records, want none", len(got.Timing.Records))
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

	// Timing verification
	tm := got.Timing
	if tm.Authority != mediafacts.TimingAuthorityCanonical {
		t.Errorf("timing authority %s, want canonical", tm.Authority)
	}
	if !tm.HasActiveEpoch || tm.ActiveEpoch != 1 {
		t.Errorf("active epoch has=%v, epoch=%d, want true/1", tm.HasActiveEpoch, tm.ActiveEpoch)
	}
	if len(tm.Records) != 3 {
		t.Fatalf("timing record count %d, want 3", len(tm.Records))
	}

	// Record 0: PES timing
	r0 := tm.Records[0]
	if r0.Type != mediafacts.TimingRecordTypePES {
		t.Errorf("record 0 type %s, want pes", r0.Type)
	}
	if r0.PES.Epoch != 1 || r0.PES.PID != 257 || !r0.PES.HasPTS || r0.PES.PTS90k != 90000 || !r0.PES.HasDTS || r0.PES.DTS90k != 86400 || r0.PES.ObservedAt != 188 || r0.PES.SubjectAt != 188 {
		t.Errorf("record 0 PES timing mismatch: %+v", r0.PES)
	}

	// Record 1: PCR
	r1 := tm.Records[1]
	if r1.Type != mediafacts.TimingRecordTypePCR {
		t.Errorf("record 1 type %s, want pcr", r1.Type)
	}
	if r1.PCR.Epoch != 1 || r1.PCR.PCRPID != 256 || r1.PCR.ObservedAt != 188 || r1.PCR.ExtendedPCR27m != 27000000 {
		t.Errorf("record 1 PCR mismatch: %+v", r1.PCR)
	}

	// Record 2: Discontinuity
	r2 := tm.Records[2]
	if r2.Type != mediafacts.TimingRecordTypeDiscontinuity {
		t.Errorf("record 2 type %s, want discontinuity", r2.Type)
	}
	if r2.Discontinuity.Scope != mediafacts.DiscontinuityScopeProgram || r2.Discontinuity.TrackPID != 0 || r2.Discontinuity.Reason != mediafacts.DiscontinuityReasonPCRDiscontinuityIndicator || r2.Discontinuity.ObservedAt != 188 || !r2.Discontinuity.HasEpochBefore || r2.Discontinuity.EpochBefore != 1 || !r2.Discontinuity.HasEpochAfter || r2.Discontinuity.EpochAfter != 2 {
		t.Errorf("record 2 Discontinuity mismatch: %+v", r2.Discontinuity)
	}
}

// TestPSIResult_APeerThatIsFailingIsRefused is the decoder held to the one thing
// it exists for. The peer is this process's own child, which is exactly the
// thing that can be wrong in ways nothing else is: a crash mid-write, a build
// that does not match, a version that slipped through.
func TestPSIResult_APeerThatIsFailingIsRefused(t *testing.T) {
	// A body that decodes cleanly, so each case below differs from it in one way.
	good := append([]byte(nil), goldenFullResult...)

	replace := func(at int, with ...byte) []byte {
		out := append([]byte(nil), good...)
		copy(out[at:], with)
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
		{"negative processed-through offset", replace(2, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"an event kind this build does not know", replace(24, 0x04)},
		{"event 1 (identity) with non-zero offset", replace(25, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01)},
		{"event 1 (identity) with non-zero flags", replace(33, 0x01)},
		{"event 2 (rap) with invalid flag bits", replace(43, 0x02)},
		{"event 3 (rap invalidated) with non-zero flags", replace(53, 0x01)},
		{"facts flags nobody defined", replace(62, 0x80)},
		{"a video codec this build does not know", replace(70, 0x7F)},
		{"an audio codec this build does not know", replace(84, 0x7F)},
		{"track flags nobody defined", replace(89, 0x80)},
		{"track observed flags nobody defined", replace(92, 0x80)},
		{"video facts flags nobody defined", replace(102, 0x80)},
		{"timing PES flags nobody defined", replace(262, 0x80)},
		{"timing PES negative observed_at", replace(263, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"timing PES negative subject_at", replace(271, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"timing PES pts non-zero when has_pts is false", replace(262, 0x02)},               // only DTS
		{"timing PES dts non-zero when has_dts is false", replace(262, 0x01)},               // only PTS
		{"timing active_epoch non-zero when has_active_epoch is false", replace(238, 0x00)}, // active_epoch is 1
		{"timing PCR negative observed_at", replace(306, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"timing discontinuity track PID non-zero for Program scope", replace(324, 0x00, 0x01)},
		{"timing discontinuity negative observed_at", replace(327, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"timing discontinuity epoch_before non-zero when has_epoch_before is false", replace(335, 0x02)}, // only after
		{"timing discontinuity epoch_after non-zero when has_epoch_after is false", replace(335, 0x01)},   // only before
		{"timing record type unknown", replace(251, 0x7F)},
		{"truncated in video facts block", good[:120]},
		{"truncated in audio scrambling block", good[:190]},
		{"truncated before section 4", good[:230]},
		{"truncated mid-section", good[:len(good)-2]},
		{"trailing bytes after the result", append(append([]byte(nil), good...), 0x00)},
		{"more events than the frame can hold", replace(20, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio PIDs than the frame can hold", replace(71, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more audio tracks than the frame can hold", replace(77, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"more timing records than the frame can hold", replace(247, 0xFF, 0xFF, 0xFF, 0xFF)},
		{"unknown critical section", replace(12, 0x7F)},
		{"duplicate section events", replace(54, 0x01)},
		{"known section events missing critical flag", replace(14, 0x00, 0x00)},
		{"known section events with reserved flag bit", replace(14, 0x00, 0x02)},
		{"known section events critical plus reserved bit", replace(14, 0x00, 0x03)},
		{"known section events critical plus high reserved bit", replace(14, 0x80, 0x01)},
		{"known section timing missing critical flag", replace(232, 0x00, 0x00)},
		{"known section timing with reserved bit", replace(232, 0x00, 0x02)},
		{"known section timing critical plus reserved bit", replace(232, 0x80, 0x01)},
		{"timing PES PID exceeds 13-bit limit", replace(260, 0x20, 0x00)},
		{"timing PCR PID exceeds 13-bit limit", replace(304, 0x20, 0x00)},
		{"timing discontinuity track scope PID exceeds 13-bit limit", replace(323, 0x02, 0x20, 0x00)},
		{"timing discontinuity track scope null PID 0x1FFF", replace(323, 0x02, 0x1F, 0xFF)},
		{"timing discontinuity track scope zero PID", replace(323, 0x02, 0x00, 0x00)},
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

// TestPSIResult_ASectionListAtItsBoundIsRead tests that the maximum allowed table sections
// can be decoded successfully in a v6 envelope.
func TestPSIResult_ASectionListAtItsBoundIsRead(t *testing.T) {
	count := mediafacts.MaxSectionsPerTable
	length := mediafacts.MaxSectionBytes

	// Build Section 3 (ACTIVE_PSI) payload
	var psiPayload []byte
	patCountBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(patCountBuf, uint16(count))
	psiPayload = append(psiPayload, patCountBuf...)
	for i := 0; i < count; i++ {
		secLenBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(secLenBuf, uint16(length))
		psiPayload = append(psiPayload, secLenBuf...)
		psiPayload = append(psiPayload, make([]byte, length)...)
	}
	psiPayload = append(psiPayload, 0x00, 0x00) // no PMT sections

	// Build envelope with sections 1, 2, 3, 4
	var body []byte
	body = append(body, 0x00) // status ok
	body = append(body, 0x02) // coverage complete
	throughBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(throughBuf, 1066)
	body = append(body, throughBuf...)
	secCountBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(secCountBuf, 4)
	body = append(body, secCountBuf...)

	// Section 1: EVENTS (from goldenEmptyResult)
	body = append(body, goldenEmptyResult[12:24]...)
	// Section 2: FACTS (from goldenEmptyResult)
	body = append(body, goldenEmptyResult[24:154]...)

	// Section 3: ACTIVE_PSI
	body = append(body, SectionActivePSI)
	body = append(body, SectionVersionV1)
	flagsBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(flagsBuf, SectionFlagCritical)
	body = append(body, flagsBuf...)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(psiPayload)))
	body = append(body, lenBuf...)
	body = append(body, psiPayload...)

	// Section 4: TIMING (from goldenEmptyResult)
	body = append(body, goldenEmptyResult[166:]...)

	got, err := decodePSIResult(body)
	if err != nil {
		t.Fatalf("the largest table a syntax-conforming core can send was refused: %v", err)
	}
	if len(got.PSI.PATSections) != count {
		t.Errorf("%d sections, want %d", len(got.PSI.PATSections), count)
	}
}

// TestPSIResult_UnknownNonCriticalSectionIsSkipped tests that an unknown non-critical section
// is safely skipped while critical sections are processed.
func TestPSIResult_UnknownNonCriticalSectionIsSkipped(t *testing.T) {
	// Build an envelope with 5 sections: 1, 2, 3, 4 and an unknown non-critical section (type 0xFE, flags 0, length 10)
	var body []byte
	body = append(body, goldenFullResult[:10]...) // status, coverage, through
	// section_count = 5
	body = append(body, 0x00, 0x05)

	// Section 1: EVENTS
	body = append(body, goldenFullResult[12:54]...)
	// Section 2: FACTS
	body = append(body, goldenFullResult[54:207]...)
	// Section 3: ACTIVE_PSI
	body = append(body, goldenFullResult[207:230]...)

	// Unknown non-critical section: type 0xFE, version 1, flags 0 (not critical), length 10
	body = append(body, 0xFE, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0A)
	body = append(body, []byte("0123456789")...)

	// Section 4: TIMING
	body = append(body, goldenFullResult[230:]...)

	got, err := decodePSIResult(body)
	if err != nil {
		t.Fatalf("unknown non-critical section was not skipped: %v", err)
	}
	if got.ProcessedThroughOffset != 188 {
		t.Errorf("through = %d, want 188", got.ProcessedThroughOffset)
	}
	if len(got.Timing.Records) != 3 {
		t.Errorf("timing records = %d, want 3", len(got.Timing.Records))
	}
}
