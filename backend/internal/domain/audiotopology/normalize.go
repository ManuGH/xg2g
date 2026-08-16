package audiotopology

import (
	"strings"
)

// NormalizeCodec parses raw codec strings or descriptors into canonical AudioCodec enum.
func NormalizeCodec(raw string) AudioCodec {
	val := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(val, "mp2") || strings.Contains(val, "mpeg") || val == "mp1":
		return CodecMP2
	case strings.Contains(val, "eac3") || strings.Contains(val, "ec-3") || strings.Contains(val, "dd+") || strings.Contains(val, "dolby digital plus"):
		return CodecEAC3
	case strings.Contains(val, "ac3") || strings.Contains(val, "ac-3") || strings.Contains(val, "dolby"):
		return CodecAC3
	case strings.Contains(val, "aac") || strings.Contains(val, "mp4a"):
		return CodecAAC
	case strings.Contains(val, "dts"):
		return CodecDTS
	case strings.Contains(val, "pcm") || strings.Contains(val, "wav"):
		return CodecPCM
	case val == "":
		return CodecUnknown
	default:
		return AudioCodec(val)
	}
}

// CodecFromDVBStreamType maps standard MPEG-2/DVB TS stream_type identifiers to AudioCodec.
// Note: 0x06 is generic Private PES data (can be AC-3, E-AC-3, DTS, Teletext, DVB Subtitles).
// It returns CodecUnknown unless confirmed by specific registration descriptors or active probing.
func CodecFromDVBStreamType(streamType uint8) AudioCodec {
	switch streamType {
	case 0x03, 0x04: // ISO/IEC 11172-3 / 13818-3 Audio (MP2)
		return CodecMP2
	case 0x0F: // ISO/IEC 13818-7 Audio with ADTS (AAC)
		return CodecAAC
	case 0x11: // ISO/IEC 14496-3 Audio with LATM (AAC)
		return CodecAAC
	case 0x81: // ATSC A/52 AC-3
		return CodecAC3
	case 0x87: // ATSC A/52b E-AC-3
		return CodecEAC3
	case 0x06: // Generic Private PES data - requires descriptor confirmation
		return CodecUnknown
	default:
		return CodecUnknown
	}
}

// NormalizeLanguage parses ISO-639 codes and language descriptors.
func NormalizeLanguage(raw string) LanguageInfo {
	val := strings.ToLower(strings.TrimSpace(raw))
	if idx := strings.IndexAny(val, ",;/ -"); idx != -1 {
		val = val[:idx]
	}

	info := LanguageInfo{
		Code: val,
	}

	switch val {
	case "deu", "ger", "de":
		info.ISO639_2 = "deu"
		info.ISO639_1 = "de"
	case "fra", "fre", "fr":
		info.ISO639_2 = "fra"
		info.ISO639_1 = "fr"
	case "eng", "en":
		info.ISO639_2 = "eng"
		info.ISO639_1 = "en"
	case "ita", "it":
		info.ISO639_2 = "ita"
		info.ISO639_1 = "it"
	case "spa", "es":
		info.ISO639_2 = "spa"
		info.ISO639_1 = "es"
	case "nld", "dut", "nl":
		info.ISO639_2 = "nld"
		info.ISO639_1 = "nl"
	case "pol", "pl":
		info.ISO639_2 = "pol"
		info.ISO639_1 = "pl"
	case "rus", "ru":
		info.ISO639_2 = "rus"
		info.ISO639_1 = "ru"
	case "mis": // Miscellaneous languages
		info.ISO639_2 = "mis"
		info.ISO639_1 = "und"
		info.IsUndefined = true
	case "mul": // Multiple languages (DVB standard for Original audio / Two-channel sound)
		info.ISO639_2 = "mul"
		info.ISO639_1 = "mul"
		info.IsOriginal = true
		info.IsUndefined = false
	case "und", "nar", "":
		info.ISO639_2 = "und"
		info.ISO639_1 = "und"
		info.IsUndefined = true
	default:
		// ISO 639-2 qaa..qtz are reserved for local/private use, NOT guaranteed Originalton.
		if len(val) == 3 && val[0] == 'q' && val[1] >= 'a' && val[1] <= 't' {
			info.ISO639_2 = val
			info.ISO639_1 = "und"
			info.IsUndefined = true
		} else {
			info.ISO639_2 = val
			info.ISO639_1 = val
		}
	}

	if info.Code == "" {
		info.Code = "und"
	}
	return info
}
