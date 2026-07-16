package ffmpeg

import (
	"encoding/binary"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
)

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

		boxSize := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])

		headerSize := 8
		if boxSize == 1 {
			if offset+16 > len(data) {
				return false // truncated 64-bit size
			}
			boxSize = binary.BigEndian.Uint64(data[offset+8 : offset+16])
			headerSize = 16
		} else if boxSize == 0 {
			// Box extends to end of file
			boxSize = uint64(len(data) - offset)
		}

		if boxSize < uint64(headerSize) || uint64(offset)+boxSize > uint64(len(data)) {
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

		offset += int(boxSize)
	}

	if offset != len(data) {
		return false // trailing garbage or misalignment
	}

	if kind == store.ObjectSegment {
		return hasMoof && hasMdat
	} else if kind == store.ObjectInit {
		return hasFtyp && hasMoov
	}

	return true
}
