// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"encoding/binary"
	"testing"
)

// --- transport fixtures -----------------------------------------------------
//
// Built here rather than reused from the video tests because these need a
// configurable audio PID, a real AC-3 descriptor and a PMT version that can be
// moved, which is what the generation cases turn on.

const (
	obsPMTPID   = 100
	obsVideoPID = 256
	obsAudioPID = 257
	obsOtherPID = 258
)

func psiPacket(pid uint16, section []byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 | byte((pid>>8)&0x1F) // PUSI
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 // payload only, CC 0
	pkt[4] = 0x00 // pointer_field
	copy(pkt[5:], section)
	for i := 5 + len(section); i < TSPacketSize; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

func obsPAT() []byte {
	s := []byte{
		0x00,
		0xB0, 0x0D,
		0x00, 0x01, // transport stream id
		0xC1,       // version 0, current
		0x00, 0x00, // section 0 of 0
		0x00, 0x01, // program 1
		0xE0 | byte(obsPMTPID>>8), byte(obsPMTPID & 0xFF),
		0, 0, 0, 0,
	}
	binary.BigEndian.PutUint32(s[len(s)-4:], CalculateMPEG2CRC32(s[:len(s)-4]))
	return psiPacket(0, s)
}

// obsTrack is one audio elementary stream for the PMT builder to name.
type obsTrack struct {
	pid         uint16
	streamType  byte
	descriptors []byte
}

// ac3Track is the shorthand a multi-track case needs: an AC-3 stream declaring a
// channel class and a language.
func ac3Track(pid uint16, lang string, componentType byte) obsTrack {
	d := append(ac3Descriptor(componentType), langDescriptor(lang)...)
	return obsTrack{pid: pid, streamType: 0x06, descriptors: d}
}

// obsPMT names one video stream and the given audio streams. streamType and the
// descriptors decide which codec each audio track is taken to be.
func obsPMT(version uint8, tracks ...obsTrack) []byte {
	es := []byte{
		0x1B, // H.264
		0xE0 | byte(obsVideoPID>>8), byte(obsVideoPID & 0xFF),
		0xF0, 0x00,
	}
	for _, tr := range tracks {
		es = append(es,
			tr.streamType,
			0xE0|byte(tr.pid>>8), byte(tr.pid&0xFF),
			0xF0|byte(len(tr.descriptors)>>8), byte(len(tr.descriptors)&0xFF),
		)
		es = append(es, tr.descriptors...)
	}

	sectionLen := 9 + len(es) + 4
	s := []byte{
		0x02,
		0xB0 | byte(sectionLen>>8), byte(sectionLen & 0xFF),
		0x00, 0x01, // program 1
		0xC0 | ((version & 0x1F) << 1) | 0x01,
		0x00, 0x00,
		0xE0 | byte(obsVideoPID>>8), byte(obsVideoPID & 0xFF), // PCR PID
		0xF0, 0x00, // program_info_length
	}
	s = append(s, es...)
	s = append(s, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(s[len(s)-4:], CalculateMPEG2CRC32(s[:len(s)-4]))
	return psiPacket(obsPMTPID, s)
}

// ac3Descriptor is descriptor tag 0x6A with the component type flag set, which is
// what makes the PMT declare a channel class.
func ac3Descriptor(componentType byte) []byte {
	return []byte{0x6A, 0x02, 0x80, componentType}
}

// langDescriptor is the ISO 639 language descriptor, tag 0x0A.
func langDescriptor(lang string) []byte {
	return []byte{0x0A, 0x04, lang[0], lang[1], lang[2], 0x00}
}

// audioPackets frames elementary stream bytes into TS packets, the first of them
// carrying a private_stream_1 PES header the way DVB carries AC-3.
func audioPackets(pid uint16, es []byte, scrambled bool) [][]byte {
	var out [][]byte
	first := true

	for len(es) > 0 {
		pkt := make([]byte, TSPacketSize)
		pkt[0] = SyncByte
		pkt[2] = byte(pid & 0xFF)
		pkt[1] = byte((pid >> 8) & 0x1F)
		pkt[3] = 0x10
		if scrambled {
			pkt[3] |= 0x80
		}

		body := 4
		if first {
			pkt[1] |= 0x40 // PUSI
			copy(pkt[4:], []byte{
				0x00, 0x00, 0x01, 0xBD, // private_stream_1
				0x00, 0x00, // PES packet length, unbounded
				0x80, 0x00, 0x00, // flags, flags, header_data_length = 0
			})
			body = 13
			first = false
		}

		n := copy(pkt[body:], es)
		es = es[n:]
		for i := body + n; i < TSPacketSize; i++ {
			pkt[i] = 0xFF
		}
		out = append(out, pkt)
	}
	return out
}

// --- elementary stream fixtures ---------------------------------------------
//
// The header bytes are the same spec-derived values the esaudio tests use.
const (
	obsByte6Surround51 = 0xEB
	obsByte6Stereo     = 0x40
)

func obsAC3Frame(byte6 byte) []byte {
	f := make([]byte, 128)
	f[0], f[1] = 0x0B, 0x77
	f[4] = 0x00 // 48 kHz, smallest frame
	f[5] = 8 << 3
	f[6] = byte6
	return f
}

func obsAC3Run(byte6 byte, n int) []byte {
	var out []byte
	for i := 0; i < n; i++ {
		out = append(out, obsAC3Frame(byte6)...)
	}
	return out
}

func pushAll(t *testing.T, r *MasterRing, packets ...[]byte) {
	t.Helper()
	for _, pkt := range packets {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push: %v", err)
		}
	}
}

func pushAudio(t *testing.T, r *MasterRing, pid uint16, es []byte) {
	t.Helper()
	pushAll(t, r, audioPackets(pid, es, false)...)
}

func observedTrack(t *testing.T, r *MasterRing, pid uint16) AudioTrackInfo {
	t.Helper()
	for _, track := range r.ReadinessFacts().AudioTracks {
		if track.PID == pid {
			return track
		}
	}
	t.Fatalf("no track for PID %d in %+v", pid, r.ReadinessFacts().AudioTracks)
	return AudioTrackInfo{}
}

// --- tests ------------------------------------------------------------------

// The descriptor declares a class and the audio carries the count. Both reach the
// facts, side by side, neither standing in for the other.
func TestMasterRing_ObservesAC3ChannelsFromTheStream(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	// Component type 0x85: multichannel, no count declared.
	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))

	track := observedTrack(t, r, obsAudioPID)

	if track.Codec != "ac3" {
		t.Fatalf("Codec = %q, want ac3", track.Codec)
	}
	if !track.Declared.Multichannel || track.Declared.Channels != 0 {
		t.Errorf("Declared = %+v, want the class without a count", track.Declared)
	}
	if track.Observed.Channels != 6 || !track.Observed.LFE {
		t.Errorf("Observed = %+v, want 6 channels with LFE", track.Observed)
	}
}

// Stereo AC-3 must not be read as anything else, or a downmix decision would be
// made on an invented surround layout.
func TestMasterRing_ObservesAC3Stereo(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x82)}))
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Stereo, 4))

	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 2 || got.LFE {
		t.Errorf("Observed = %+v, want 2 channels without LFE", got)
	}
}

// The case a startup probe cannot answer: the same PID moves from stereo to 5.1
// and back between programmes, with no PMT change to announce either move.
func TestMasterRing_FollowsALayoutChangeWithoutAPMTChange(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))

	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Stereo, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 2 {
		t.Fatalf("Observed.Channels = %d, want 2 to start", got.Channels)
	}

	generation := r.ReadinessFacts().Generation

	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 6 {
		t.Errorf("Observed.Channels = %d, want 6 after the stream moved to 5.1", got.Channels)
	}

	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Stereo, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 2 {
		t.Errorf("Observed.Channels = %d, want 2 after the stream moved back", got.Channels)
	}

	if now := r.ReadinessFacts().Generation; now != generation {
		t.Errorf("Generation moved from %d to %d - an audio layout change is not a new stream", generation, now)
	}
}

// A new PMT can put a different elementary stream on a PID. Carrying the old
// observation across would describe audio that is no longer there.
func TestMasterRing_PMTChangeDropsTheObservation(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 6 {
		t.Fatalf("Observed.Channels = %d, want 6 before the change", got.Channels)
	}

	// Same PID, new table.
	pushAll(t, r, obsPMT(1, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))

	if got := observedTrack(t, r, obsAudioPID).Observed; got.Known() {
		t.Errorf("Observed = %+v, want nothing carried across the PMT change", got)
	}

	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Stereo, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 2 {
		t.Errorf("Observed.Channels = %d, want the new stream's own layout", got.Channels)
	}
}

// The sharp edge of the same rule. A new table can keep the PID and change the
// codec to one this path does not read, so no new observer is built for it - and
// then nothing but the reset stops the previous stream's 5.1 from being attached
// to a track that is now MP2.
func TestMasterRing_PMTChangeToAnUnreadCodecDropsTheObservation(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))
	if got := observedTrack(t, r, obsAudioPID).Observed; got.Channels != 6 {
		t.Fatalf("Observed.Channels = %d, want 6 before the change", got.Channels)
	}

	// Same PID, now MPEG-1 layer II.
	pushAll(t, r, obsPMT(1, obsTrack{obsAudioPID, 0x03, nil}))

	track := observedTrack(t, r, obsAudioPID)
	if track.Codec != "mp2" {
		t.Fatalf("Codec = %q, want mp2", track.Codec)
	}
	if track.Observed.Known() {
		t.Errorf("Observed = %+v, want nothing - this is the previous stream's layout", track.Observed)
	}
}

// The audio moving to a different PID is the same problem seen from the other
// side: nothing may survive on either PID.
func TestMasterRing_AudioPIDChangeDropsTheObservation(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))

	pushAll(t, r, obsPMT(1, obsTrack{obsOtherPID, 0x06, ac3Descriptor(0x85)}))

	facts := r.ReadinessFacts()
	for _, track := range facts.AudioTracks {
		if track.PID == obsAudioPID {
			t.Errorf("PID %d still present after it left the PMT", obsAudioPID)
		}
	}
	if got := observedTrack(t, r, obsOtherPID).Observed; got.Known() {
		t.Errorf("Observed = %+v on the new PID, want nothing before it has carried audio", got)
	}

	// Payload still arriving on the PID the table no longer names changes nothing.
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))
	if got := observedTrack(t, r, obsOtherPID).Observed; got.Known() {
		t.Errorf("Observed = %+v, want a stream the PMT dropped to reach no track", got)
	}
}

// MP2 carries its channel mode in the audio frame header too, but this path does
// not read that syntax. It must produce no observation rather than a wrong one.
func TestMasterRing_UnreadCodecProducesNoObservation(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x03, nil})) // MPEG-1 layer II
	pushAudio(t, r, obsAudioPID, obsAC3Run(obsByte6Surround51, 4))

	track := observedTrack(t, r, obsAudioPID)
	if track.Codec != "mp2" {
		t.Fatalf("Codec = %q, want mp2", track.Codec)
	}
	if track.Observed.Known() {
		t.Errorf("Observed = %+v, want nothing for a codec this path does not read", track.Observed)
	}
}

// Scrambled payload is ciphertext. Parsing it would index random bytes as frame
// headers, which is the same mistake the video path already refuses to make.
func TestMasterRing_ScrambledAudioIsNotObserved(t *testing.T) {
	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushAll(t, r, obsPAT(), obsPMT(0, obsTrack{obsAudioPID, 0x06, ac3Descriptor(0x85)}))
	pushAll(t, r, audioPackets(obsAudioPID, obsAC3Run(obsByte6Surround51, 4), true)...)

	if got := observedTrack(t, r, obsAudioPID).Observed; got.Known() {
		t.Errorf("Observed = %+v, want nothing read out of scrambled payload", got)
	}
}
