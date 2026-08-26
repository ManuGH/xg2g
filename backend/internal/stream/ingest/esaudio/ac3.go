// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package esaudio reads audio facts out of elementary stream payload.
//
// It knows nothing about MPEG-TS, PES, or whatever carried the bytes here: the
// input is elementary stream payload for one track, the output is what those
// bytes say about themselves. Nothing in this package decides what to do with the
// answer - no downmix, no rendition, no manifest, no fallback to stereo. That
// separation is the whole point. A PMT descriptor can only declare a class
// ("more than two channels"), and a 5.1 service and a 7.1 service declare the
// same value, so a number can only come from the audio itself.
package esaudio

// Codec names, matching the vocabulary the rest of the ingest path uses.
const (
	CodecAC3  = "ac3"
	CodecEAC3 = "eac3"
)

const (
	syncWordHi = 0x0B
	syncWordLo = 0x77

	// headerBytes is what both syntaxes need to answer the channel question.
	// E-AC-3 has lfeon in byte 4; AC-3 places it after a run of mix level fields
	// whose presence acmod decides, but even the longest of those runs still ends
	// inside byte 6.
	headerBytes = 7
)

// acmodChannels maps audio coding mode to the number of full-bandwidth channels,
// ATSC A/52 Table 5.8. acmod 0 is 1+1 - two independent mono programmes, which is
// two channels of audio however it is used.
var acmodChannels = [8]int{2, 1, 2, 3, 3, 4, 4, 5}

// ac3Bitrates is A/52 Table 5.18 in kbit/s. frmsizecod indexes it in pairs; the
// low bit distinguishes the two 44.1 kHz frame lengths that share a bitrate.
var ac3Bitrates = [19]int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 448, 512, 576, 640}

// FrameHeader is what one syncframe says about its own channel layout.
type FrameHeader struct {
	// Channels counts full-bandwidth channels plus LFE. It is 0 when the frame
	// does not describe the programme's own layout - a dependent substream, or an
	// independent substream belonging to another programme.
	Channels int

	Acmod uint8
	LFE   bool

	// Dependent marks an E-AC-3 dependent substream. Its channels extend the
	// independent substream rather than replacing it, and reconstructing the
	// total needs the channel map this parser does not read - so the frame is
	// reported as a fact about the stream's shape, never as a count.
	Dependent bool

	// SizeBytes is the whole frame, used to step to the next syncword instead of
	// rescanning frame payload that can contain the sync pattern by chance.
	SizeBytes int
}

// ParseSyncFrame reads the header of a syncframe starting at b[0].
//
// AC-3 and E-AC-3 share a syncword and are told apart by bsid, which both
// syntaxes happen to place in the top five bits of byte 5. Everything else about
// the two layouts differs.
func ParseSyncFrame(b []byte) (FrameHeader, bool) {
	if len(b) < headerBytes || b[0] != syncWordHi || b[1] != syncWordLo {
		return FrameHeader{}, false
	}

	switch bsid := b[5] >> 3; {
	case bsid <= 10:
		// bsid 9 and 10 are the alternate bit rate variants; they carry the same
		// bit stream information syntax as 8.
		return parseAC3(b)
	case bsid <= 16:
		return parseEAC3(b)
	default:
		// Reserved. A byte pair that happened to look like a syncword is far more
		// likely than a bitstream this parser has never heard of.
		return FrameHeader{}, false
	}
}

// parseAC3 reads the A/52 syncinfo and bsi fields up to lfeon.
func parseAC3(b []byte) (FrameHeader, bool) {
	fscod := b[4] >> 6
	frmsizecod := b[4] & 0x3F
	if fscod == 0x03 || frmsizecod > 37 {
		// Reserved sample rate or frame size code. Treated as "not a frame" rather
		// than as a frame with unknown fields: a false syncword is the likelier
		// explanation, and acting on one would put a wrong layout into the facts.
		return FrameHeader{}, false
	}

	acmod := b[6] >> 5

	// lfeon sits behind the mix level fields, and which of those are present is
	// decided by acmod itself.
	offset := 3
	if acmod&0x01 != 0 && acmod != 0x01 {
		offset += 2 // cmixlev, present for the modes with a centre channel
	}
	if acmod&0x04 != 0 {
		offset += 2 // surmixlev, present for the modes with surround channels
	}
	if acmod == 0x02 {
		offset += 2 // dsurmod, only 2/0 declares Dolby Surround encoding
	}
	lfe := (b[6]>>(7-offset))&0x01 == 1

	channels := acmodChannels[acmod]
	if lfe {
		channels++
	}
	return FrameHeader{
		Channels:  channels,
		Acmod:     acmod,
		LFE:       lfe,
		SizeBytes: ac3FrameBytes(fscod, frmsizecod),
	}, true
}

// ac3FrameBytes is A/52 Table 5.18. At 48 and 32 kHz the frame is a whole number
// of words per kbit/s; at 44.1 kHz it is not, which is why the table lists two
// lengths per bitrate and frmsizecod's low bit picks between them.
func ac3FrameBytes(fscod, frmsizecod uint8) int {
	bitrate := ac3Bitrates[frmsizecod>>1]

	var words int
	switch fscod {
	case 0x00: // 48 kHz
		words = bitrate * 2
	case 0x01: // 44.1 kHz
		words = (bitrate*1536*1000)/(44100*16) + int(frmsizecod&0x01)
	default: // 32 kHz
		words = bitrate * 3
	}
	return words * 2
}

// parseEAC3 reads the A/52 Annex E bit stream information.
func parseEAC3(b []byte) (FrameHeader, bool) {
	strmtyp := b[2] >> 6
	if strmtyp == 0x03 {
		return FrameHeader{}, false // reserved
	}
	substreamID := (b[2] >> 3) & 0x07
	frmsiz := (uint16(b[2]&0x07) << 8) | uint16(b[3])

	acmod := (b[4] >> 1) & 0x07
	lfe := b[4]&0x01 == 1

	h := FrameHeader{
		Acmod:     acmod,
		LFE:       lfe,
		SizeBytes: (int(frmsiz) + 1) * 2,
	}

	switch {
	case strmtyp == 0x01:
		// Dependent substream: its acmod describes the channels it adds, not the
		// programme's total.
		h.Dependent = true
	case substreamID != 0:
		// An independent substream other than 0 is a different programme in the
		// same elementary stream. Not this track's layout.
	default:
		h.Channels = acmodChannels[acmod]
		if lfe {
			h.Channels++
		}
	}
	return h, true
}
