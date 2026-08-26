// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

import "testing"

// The byte 6 values below are written out from A/52 rather than produced by the
// same offset arithmetic the parser uses, so a mistake in that arithmetic cannot
// hide behind a test that repeats it.
//
//	5.1  acmod=111 cmixlev=01 surmixlev=01 lfeon=1  -> 1110 1011 = 0xEB
//	3/2  acmod=111 cmixlev=01 surmixlev=01 lfeon=0  -> 1110 1010 = 0xEA
//	2/0  acmod=010 dsurmod=00 lfeon=0               -> 0100 0000 = 0x40
//	1/0  acmod=001 lfeon=0                          -> 0010 0000 = 0x20
const (
	ac3Byte6Surround51 = 0xEB
	ac3Byte6Surround50 = 0xEA
	ac3Byte6Stereo     = 0x40
	ac3Byte6Mono       = 0x20
)

// ac3Frame builds a complete AC-3 syncframe. frmsizecod 0 at 48 kHz is 32 kbit/s,
// which no broadcaster would use for 5.1 - it is chosen because it makes the
// smallest legal frame and the header fields under test do not depend on it.
func ac3Frame(byte6 byte) []byte {
	const (
		fscod      = 0x00 // 48 kHz
		frmsizecod = 0x00 // 32 kbit/s -> 64 words -> 128 bytes
		bsid       = 8
	)
	f := make([]byte, 128)
	f[0], f[1] = 0x0B, 0x77
	f[2], f[3] = 0xAA, 0xBB // crc1, not checked
	f[4] = fscod<<6 | frmsizecod
	f[5] = bsid << 3
	f[6] = byte6
	return f
}

// eac3Frame builds an E-AC-3 syncframe. frmsiz counts 16-bit words minus one.
func eac3Frame(strmtyp, substreamID, acmod uint8, lfe bool) []byte {
	const size = 128
	frmsiz := uint16(size/2 - 1)

	f := make([]byte, size)
	f[0], f[1] = 0x0B, 0x77
	f[2] = strmtyp<<6 | substreamID<<3 | byte(frmsiz>>8)
	f[3] = byte(frmsiz & 0xFF)
	f[4] = acmod << 1
	if lfe {
		f[4] |= 0x01
	}
	f[5] = 16 << 3 // bsid 16 selects the Annex E syntax
	return f
}

func TestParseSyncFrame_AC3ChannelLayouts(t *testing.T) {
	tests := []struct {
		name     string
		byte6    byte
		channels int
		lfe      bool
		acmod    uint8
	}{
		{"5.1", ac3Byte6Surround51, 6, true, 7},
		{"3/2 without LFE", ac3Byte6Surround50, 5, false, 7},
		{"stereo", ac3Byte6Stereo, 2, false, 2},
		{"mono", ac3Byte6Mono, 1, false, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, ok := ParseSyncFrame(ac3Frame(tc.byte6))
			if !ok {
				t.Fatal("frame not parsed")
			}
			if h.Channels != tc.channels {
				t.Errorf("Channels = %d, want %d", h.Channels, tc.channels)
			}
			if h.LFE != tc.lfe {
				t.Errorf("LFE = %v, want %v", h.LFE, tc.lfe)
			}
			if h.Acmod != tc.acmod {
				t.Errorf("Acmod = %d, want %d", h.Acmod, tc.acmod)
			}
			if h.SizeBytes != 128 {
				t.Errorf("SizeBytes = %d, want 128", h.SizeBytes)
			}
		})
	}
}

// 32 kHz and 44.1 kHz frames are a different length for the same bitrate, and the
// 44.1 kHz table lists two lengths per bitrate. Stepping by the wrong one would
// land the scan inside frame payload.
func TestParseSyncFrame_AC3FrameSizes(t *testing.T) {
	tests := []struct {
		name       string
		fscod      byte
		frmsizecod byte
		want       int
	}{
		{"48 kHz 32 kbit/s", 0x00, 0, 128},
		{"48 kHz 448 kbit/s", 0x00, 30, 1792},
		{"32 kHz 32 kbit/s", 0x02, 0, 192},
		{"44.1 kHz 64 kbit/s even", 0x01, 8, 278},
		{"44.1 kHz 64 kbit/s odd", 0x01, 9, 280},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := ac3Frame(ac3Byte6Stereo)
			f[4] = tc.fscod<<6 | tc.frmsizecod
			h, ok := ParseSyncFrame(f)
			if !ok {
				t.Fatal("frame not parsed")
			}
			if h.SizeBytes != tc.want {
				t.Errorf("SizeBytes = %d, want %d", h.SizeBytes, tc.want)
			}
		})
	}
}

func TestParseSyncFrame_EAC3(t *testing.T) {
	h, ok := ParseSyncFrame(eac3Frame(0, 0, 7, true))
	if !ok {
		t.Fatal("frame not parsed")
	}
	if h.Channels != 6 {
		t.Errorf("Channels = %d, want 6", h.Channels)
	}
	if !h.LFE || h.Acmod != 7 {
		t.Errorf("LFE = %v, Acmod = %d, want true / 7", h.LFE, h.Acmod)
	}
	if h.SizeBytes != 128 {
		t.Errorf("SizeBytes = %d, want 128", h.SizeBytes)
	}
}

// A dependent substream adds channels to the independent one rather than
// describing the programme, and reconstructing the total needs the channel map
// this parser does not read. Reporting its acmod as a count would be an
// invention, so the frame is recognised and reported without one.
func TestParseSyncFrame_EAC3DependentSubstreamCarriesNoCount(t *testing.T) {
	h, ok := ParseSyncFrame(eac3Frame(1, 0, 7, true))
	if !ok {
		t.Fatal("frame not parsed")
	}
	if !h.Dependent {
		t.Error("Dependent = false, want true")
	}
	if h.Channels != 0 {
		t.Errorf("Channels = %d, want 0 - a dependent substream is not the programme's layout", h.Channels)
	}
}

// A second independent substream is another programme in the same elementary
// stream, not another view of this one.
func TestParseSyncFrame_EAC3ForeignSubstreamCarriesNoCount(t *testing.T) {
	h, ok := ParseSyncFrame(eac3Frame(0, 3, 7, true))
	if !ok {
		t.Fatal("frame not parsed")
	}
	if h.Channels != 0 {
		t.Errorf("Channels = %d, want 0", h.Channels)
	}
	if h.Dependent {
		t.Error("Dependent = true, want false")
	}
}

// Anything that fails a field check is reported as "not a frame". The alternative
// - a frame with fields we did not trust - is how a stray byte pair becomes a
// channel layout.
func TestParseSyncFrame_Rejects(t *testing.T) {
	reserved := ac3Frame(ac3Byte6Stereo)
	reserved[4] = 0x03 << 6 // fscod = 3

	badSize := ac3Frame(ac3Byte6Stereo)
	badSize[4] = 38 // frmsizecod above the table

	badBSID := ac3Frame(ac3Byte6Stereo)
	badBSID[5] = 17 << 3

	noSync := ac3Frame(ac3Byte6Stereo)
	noSync[1] = 0x78

	tests := []struct {
		name  string
		frame []byte
	}{
		{"reserved sample rate", reserved},
		{"frame size code above the table", badSize},
		{"bit stream id we do not know", badBSID},
		{"no syncword", noSync},
		{"too short to hold a header", ac3Frame(ac3Byte6Stereo)[:headerBytes-1]},
		{"empty", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := ParseSyncFrame(tc.frame); ok {
				t.Error("parsed, want rejected")
			}
		})
	}
}
