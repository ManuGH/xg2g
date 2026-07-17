package ffmpeg

import (
	"encoding/binary"

	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

func checkedFMP4BoxSize(raw uint64, remaining int) (int, bool) {
	if remaining < 0 {
		return 0, false
	}
	size := int(raw) // #nosec G115 -- the round-trip and remaining-length checks below reject overflow.
	if size < 0 {
		return 0, false
	}
	if uint64(size) != raw || size > remaining { // #nosec G115 -- size is non-negative before conversion.
		return 0, false
	}
	return size, true
}

// validCompleteFMP4 structurally parses an fMP4 byte slice to ensure it is fully complete and not truncated.
// It checks that box sizes align exactly with the file size and that the required boxes for the given kind are present.
func validCompleteFMP4(data []byte, kind store.ObjectKind) bool {
	if len(data) < 8 {
		return false
	}

	var hasMoof, hasMdat, hasFtyp, hasMoov bool
	offset := 0

	for offset < len(data) {
		if offset+8 > len(data) {
			return false // truncated box header
		}

		rawBoxSize := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])

		headerSize := 8
		var boxSize int
		switch rawBoxSize {
		case 1:
			if offset+16 > len(data) {
				return false // truncated 64-bit size
			}
			headerSize = 16
			var ok bool
			boxSize, ok = checkedFMP4BoxSize(binary.BigEndian.Uint64(data[offset+8:offset+16]), len(data)-offset)
			if !ok {
				return false
			}
		case 0:
			// Box extends to end of file
			boxSize = len(data) - offset
		default:
			var ok bool
			boxSize, ok = checkedFMP4BoxSize(rawBoxSize, len(data)-offset)
			if !ok {
				return false
			}
		}

		if boxSize < headerSize || boxSize > len(data)-offset {
			return false // box is truncated or claims size larger than file
		}

		switch boxType {
		case "moof":
			hasMoof = true
		case "mdat":
			hasMdat = true
		case "ftyp":
			hasFtyp = true
		case "moov":
			hasMoov = true
		}

		offset += boxSize
	}

	if offset != len(data) {
		return false // trailing garbage or misalignment
	}

	switch kind {
	case store.ObjectSegment:
		return hasMoof && hasMdat
	case store.ObjectInit:
		return hasFtyp && hasMoov
	}

	return true
}
