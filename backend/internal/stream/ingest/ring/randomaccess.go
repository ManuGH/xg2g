// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

// Random access classification.
//
// A subscriber that joins mid-broadcast has no reference pictures, so it can only
// be attached at an access unit the decoder can start on. Deciding which access
// units qualify used to be done by asking whether a parameter set had been seen
// in the same PES:
//
//	case 1, 2: if pesHasSPS && pesHasPPS { isKeyframe = true }
//
// That is not what a random access point is. Broadcast encoders repeat SPS and PPS
// on a schedule of their own, and nothing stops them from repeating them ahead of a
// predicted picture. Such a picture references frames that were never delivered, and
// a decoder started on it produces exactly the blocky start this project measured.
//
// What actually makes an access unit joinable is a property of its slices, not of
// its neighbours: either the stream declares it an instantaneous refresh (H.264 IDR,
// HEVC IRAP), or every coded slice in it is intra-coded, which is how DVB encoders
// signal random access when they never emit an IDR at all - the case on all eight
// channels measured against the reference receiver.
//
// The recovery_point SEI is read alongside, because it is the stream's own statement
// that a picture is a random access point. It is recorded as corroboration rather
// than required: an encoder that omits it still produces joinable intra pictures, and
// gating on it would black out those channels.

// H.264 NAL unit types relevant to random access.
const (
	h264NALSliceNonIDR = 1
	h264NALSlicePartA  = 2
	h264NALSliceIDR    = 5
	h264NALSEI         = 6
	h264NALSPS         = 7
	h264NALPPS         = 8
)

// HEVC NAL unit types relevant to random access. 16..21 is the IRAP range:
// BLA_W_LP, BLA_W_RADL, BLA_N_LP, IDR_W_RADL, IDR_N_LP, CRA_NUT. Any of them can
// start a decoder, which is why the whole range qualifies rather than only IDR.
const (
	hevcNALIRAPFirst = 16
	hevcNALIRAPLast  = 21
	hevcNALVPS       = 32
	hevcNALSPS       = 33
	hevcNALPPS       = 34
	hevcNALPrefixSEI = 39
)

// seiPayloadRecoveryPoint is the SEI payload type that marks a random access point.
const seiPayloadRecoveryPoint = 6

// nalCaptureKind selects how the bytes captured after a NAL header are read.
type nalCaptureKind uint8

const (
	captureNone nalCaptureKind = iota
	captureH264SliceHeader
	captureSEI
	captureMPEG2PictureHeader
)

// Capture budgets. A slice header needs only first_mb_in_slice and slice_type, both
// Exp-Golomb coded at the very start. An SEI needs enough to walk past payloads that
// precede a recovery_point; broadcast SEI NALs put pic_timing first and stay small.
const (
	sliceHeaderCaptureBytes        = 12
	seiCaptureBytes                = 48
	mpeg2PictureHeaderCaptureBytes = 3
)

// mpeg2PictureIsIntra inspects the first 2-3 bytes after an MPEG-2 picture_start_code (0x00).
// In ISO/IEC 13818-2 Section 6.2.2.6:
// - temporal_reference: 10 bits (byte 0 and top 2 bits of byte 1)
// - picture_coding_type: 3 bits (bits 5..3 of byte 1, i.e. (byte[1] >> 3) & 0x07)
// Values: 1 = I-Frame (Intra), 2 = P-Frame (Predictive), 3 = B-Frame (Bidirectional).
func mpeg2PictureIsIntra(data []byte) (isIntra bool, ok bool) {
	if len(data) < 2 {
		return false, false
	}
	codingType := (data[1] >> 3) & 0x07
	if codingType < 1 || codingType > 3 {
		return false, false
	}
	return codingType == 1, true
}

// bitReader reads Exp-Golomb and fixed-width fields from an RBSP byte slice.
type bitReader struct {
	data []byte
	pos  int // bit position
}

func (b *bitReader) bitsLeft() int { return len(b.data)*8 - b.pos }

func (b *bitReader) readBit() (uint32, bool) {
	if b.pos >= len(b.data)*8 {
		return 0, false
	}
	byteIdx := b.pos >> 3
	bitIdx := 7 - uint(b.pos&7)
	b.pos++
	return uint32((b.data[byteIdx] >> bitIdx) & 1), true
}

// readUE reads an unsigned Exp-Golomb value. Broadcast slice headers keep these
// small; a run of leading zeros longer than 32 means the bytes are not a slice
// header at all, so it is rejected rather than read.
func (b *bitReader) readUE() (uint32, bool) {
	leadingZeros := 0
	for {
		bit, ok := b.readBit()
		if !ok {
			return 0, false
		}
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros > 32 {
			return 0, false
		}
	}
	if leadingZeros == 0 {
		return 0, true
	}
	if b.bitsLeft() < leadingZeros {
		return 0, false
	}
	var suffix uint32
	for i := 0; i < leadingZeros; i++ {
		bit, ok := b.readBit()
		if !ok {
			return 0, false
		}
		suffix = (suffix << 1) | bit
	}
	return (1 << uint(leadingZeros)) - 1 + suffix, true
}

// removeEmulationPrevention strips the 0x03 bytes an encoder inserts to keep
// payload data from imitating a start code. Reading Exp-Golomb over the raw bytes
// without this shifts every field that follows an inserted byte.
func removeEmulationPrevention(src []byte) []byte {
	out := make([]byte, 0, len(src))
	zeros := 0
	for _, c := range src {
		if zeros >= 2 && c == 0x03 {
			zeros = 0
			continue
		}
		if c == 0x00 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

// h264SliceIsIntra reports whether a slice header names an intra-coded slice type.
//
// The second return value distinguishes "read it, and it is predicted" from "could
// not read it". An unreadable header must not be counted as intra: that would let a
// truncated capture open the attach gate on a predicted picture, which is the exact
// failure this classification exists to prevent.
func h264SliceIsIntra(captured []byte) (isIntra bool, ok bool) {
	rbsp := removeEmulationPrevention(captured)
	r := &bitReader{data: rbsp}
	if _, ok := r.readUE(); !ok { // first_mb_in_slice
		return false, false
	}
	sliceType, ok := r.readUE()
	if !ok {
		return false, false
	}
	// 5..9 repeat 0..4 with the added promise that every slice in the picture
	// carries that type; the family is what matters here.
	switch sliceType % 5 {
	case 2, 4: // I, SI
		return true, true
	default:
		return false, true
	}
}

// seiHasRecoveryPoint reports whether an SEI NAL payload contains a recovery_point
// message. The payload is a sequence of (type, size, body) triples, both type and
// size coded as 0xFF-extended byte runs.
func seiHasRecoveryPoint(captured []byte) bool {
	rbsp := removeEmulationPrevention(captured)
	pos := 0
	for pos < len(rbsp) {
		if rbsp[pos] == 0x80 { // rbsp_trailing_bits
			return false
		}
		payloadType := 0
		for pos < len(rbsp) && rbsp[pos] == 0xFF {
			payloadType += 255
			pos++
		}
		if pos >= len(rbsp) {
			return false
		}
		payloadType += int(rbsp[pos])
		pos++

		payloadSize := 0
		for pos < len(rbsp) && rbsp[pos] == 0xFF {
			payloadSize += 255
			pos++
		}
		if pos >= len(rbsp) {
			return false
		}
		payloadSize += int(rbsp[pos])
		pos++

		if payloadType == seiPayloadRecoveryPoint {
			return true
		}
		// A payload that runs past the captured bytes ends the walk: the messages
		// after it cannot be located without the bytes that were not captured.
		if payloadSize > len(rbsp)-pos {
			return false
		}
		pos += payloadSize
	}
	return false
}

// RandomAccessObservation reports how the ring has been classifying access units.
//
// Exposed for the readiness evaluator and for diagnosing a channel that never yields
// an attach point: "no IDR, no intra access unit either" and "IDR present but the
// payload is scrambled" call for different answers.
type RandomAccessObservation struct {
	// IRAPPoints counts access units that declared an instantaneous refresh.
	IRAPPoints uint64
	// IntraPoints counts access units admitted because every coded slice was intra.
	IntraPoints uint64
	// RecoveryPointSEIs counts admitted access units that also carried the SEI.
	RecoveryPointSEIs uint64
	// PredictedRejected counts access units that carried parameter sets but were
	// rejected because at least one slice was predicted. Under the previous rule
	// every one of these would have been offered as an attach point.
	PredictedRejected uint64
	// UnreadableSlices counts slice headers that could not be parsed.
	UnreadableSlices uint64
}

// isAudioStreamType reports whether a PMT elementary stream entry carries audio.
//
// The unambiguous stream types are listed directly. Type 0x06 is "PES carrying
// private data" and is what European broadcasters use for AC-3, E-AC-3 and DTS, so
// it only counts as audio when a descriptor says so - otherwise subtitles and
// teletext, which share that stream type, would be watched for descrambling as if
// they were the programme audio.
func isAudioStreamType(streamType byte, descriptors []byte) bool {
	switch streamType {
	case 0x03, 0x04: // MPEG-1 / MPEG-2 audio
		return true
	case 0x0F: // AAC in ADTS
		return true
	case 0x11: // AAC in LATM
		return true
	case 0x1C: // MPEG-4 raw audio
		return true
	case 0x81, 0x87: // AC-3, E-AC-3 (ATSC registration)
		return true
	case 0x06:
		return hasAudioDescriptor(descriptors)
	default:
		return false
	}
}

// Descriptor tags that identify an audio elementary stream within stream type 0x06.
const (
	descriptorAC3      = 0x6A
	descriptorEnhAC3   = 0x7A
	descriptorDTS      = 0x7B
	descriptorAAC      = 0x7C
	descriptorDTSHD    = 0x7D
	descriptorAudioReg = 0x05 // registration_descriptor, checked for AC-3 format ids
)

func hasAudioDescriptor(descriptors []byte) bool {
	for i := 0; i+2 <= len(descriptors); {
		tag := descriptors[i]
		length := int(descriptors[i+1])
		if i+2+length > len(descriptors) {
			return false
		}
		switch tag {
		case descriptorAC3, descriptorEnhAC3, descriptorDTS, descriptorAAC, descriptorDTSHD:
			return true
		case descriptorAudioReg:
			switch string(descriptors[i+2 : i+2+min(length, 4)]) {
			case "AC-3", "EAC3", "DTS1", "DTS2", "DTS3":
				return true
			}
		}
		i += 2 + length
	}
	return false
}

func appendPID(list []uint16, pid uint16) []uint16 {
	if pid == 0 || pid == 0x1FFF {
		return list
	}
	for _, existing := range list {
		if existing == pid {
			return list
		}
	}
	return append(list, pid)
}

// AudioTrackInfo describes an audio elementary stream discovered from PMT.
type AudioTrackInfo struct {
	PID        uint16 `json:"pid"`
	StreamType byte   `json:"streamType"`
	Codec      string `json:"codec"`    // "mp2", "aac", "ac3", "eac3", "dts", "unknown"
	Language   string `json:"language"` // e.g. "deu", "eng", "und"
}

// AudioCodecFromStreamType identifies the audio codec normalized from stream type and descriptors.
func AudioCodecFromStreamType(streamType byte, descriptors []byte) string {
	switch streamType {
	case 0x03, 0x04:
		return "mp2"
	case 0x0F, 0x11, 0x1C:
		return "aac"
	case 0x81, 0x87:
		return "ac3"
	case 0x06:
		for i := 0; i+2 <= len(descriptors); {
			tag := descriptors[i]
			length := int(descriptors[i+1])
			if i+2+length > len(descriptors) {
				break
			}
			switch tag {
			case descriptorAC3:
				return "ac3"
			case descriptorEnhAC3:
				return "eac3"
			case descriptorAAC:
				return "aac"
			case descriptorDTS, descriptorDTSHD:
				return "dts"
			case descriptorAudioReg:
				if length >= 4 {
					fmtID := string(descriptors[i+2 : i+6])
					if fmtID == "AC-3" {
						return "ac3"
					}
					if fmtID == "EAC3" {
						return "eac3"
					}
				}
			}
			i += 2 + length
		}
		return "unknown"
	default:
		return "unknown"
	}
}

// LanguageFromDescriptors extracts the 3-letter ISO-639-2 language code if present (descriptor tag 0x0A).
func LanguageFromDescriptors(descriptors []byte) string {
	for i := 0; i+2 <= len(descriptors); {
		tag := descriptors[i]
		length := int(descriptors[i+1])
		if i+2+length > len(descriptors) {
			break
		}
		if tag == 0x0A && length >= 3 {
			lang := string(descriptors[i+2 : i+5])
			if len(lang) == 3 {
				return lang
			}
		}
		i += 2 + length
	}
	return "und"
}
