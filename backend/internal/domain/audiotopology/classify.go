package audiotopology

import (
	"fmt"
	"strings"
)

// ClassifyPurpose derives the AudioPurpose of a track with confidence tracking.
func ClassifyPurpose(
	lang LanguageInfo,
	e2Desc string,
	cleanEffects bool,
	isPrimaryLang bool,
) (AudioPurpose, Confidence) {
	descLower := strings.ToLower(strings.TrimSpace(e2Desc))

	// 1. Explicit Enigma2 indication
	if strings.Contains(descLower, "originalton") || strings.Contains(descLower, "original") {
		return AudioPurposeAlternate, ConfidenceHigh
	}
	if strings.Contains(descLower, "kommentar") || strings.Contains(descLower, "commentary") {
		return AudioPurposeCommentary, ConfidenceHigh
	}

	// 2. DVB ETSI original audio indicator
	if lang.IsOriginal {
		return AudioPurposeAlternate, ConfidenceExplicit
	}

	// 3. Clean effects / Stadium sound without commentary
	if cleanEffects {
		return AudioPurposeAlternate, ConfidenceMedium
	}

	// 4. Non-primary language on the service
	if !isPrimaryLang && !lang.IsUndefined {
		return AudioPurposeAlternate, ConfidenceMedium
	}

	return AudioPurposeMain, ConfidenceLow
}

// ClassifyAccessibility determines barrier-free features from PMT and Enigma2 descriptions.
func ClassifyAccessibility(
	visualImpaired bool,
	hearingImpaired bool,
	e2Desc string,
) AudioAccessibility {
	acc := AudioAccessibility{
		AudioDescription: visualImpaired,
		HearingImpaired:  hearingImpaired,
	}

	descLower := strings.ToLower(strings.TrimSpace(e2Desc))

	if strings.Contains(descLower, "audiodeskription") ||
		strings.Contains(descLower, "hörfilm") ||
		strings.Contains(descLower, "mit audiodeskription") {
		// Caution: "ohne Audiodeskription" is handled via conflict checking in merge.go
		if !strings.Contains(descLower, "ohne audiodeskription") {
			acc.AudioDescription = true
		}
	}

	if strings.Contains(descLower, "klare sprache") ||
		strings.Contains(descLower, "barrierefrei") ||
		strings.Contains(descLower, "clear voice") {
		acc.ClearDialogue = true
		acc.HearingImpaired = true
	}

	return acc
}

// BuildTrackLabel constructs a clear, user-facing label for the audio track.
func BuildTrackLabel(
	lang LanguageInfo,
	codec AudioCodec,
	channels int,
	purpose AudioPurpose,
	acc AudioAccessibility,
	e2Desc string,
) string {
	var parts []string

	// 1. Language or Main Feature
	baseName := languageDisplayName(lang)
	e2Lower := strings.ToLower(strings.TrimSpace(e2Desc))

	switch {
	case acc.AudioDescription:
		parts = append(parts, "Audiodeskription (Hörfilm)")
	case acc.ClearDialogue:
		if baseName != "" {
			parts = append(parts, fmt.Sprintf("%s (Klare Sprache)", baseName))
		} else {
			parts = append(parts, "Klare Sprache")
		}
	case purpose == AudioPurposeAlternate && (strings.Contains(e2Lower, "original") || lang.IsOriginal):
		parts = append(parts, "Originalton")
	case strings.Contains(e2Lower, "französisch"):
		parts = append(parts, "Französisch")
	case strings.Contains(e2Lower, "englisch"):
		parts = append(parts, "Englisch")
	case baseName != "":
		parts = append(parts, baseName)
	default:
		parts = append(parts, "Audio")
	}

	// 2. Format / Channel Layout
	switch {
	case codec == CodecAC3 || codec == CodecEAC3:
		if channels == 6 {
			parts = append(parts, "Dolby Digital 5.1")
		} else {
			parts = append(parts, "Dolby Digital 2.0")
		}
	case channels == 6:
		parts = append(parts, "5.1 Surround")
	case channels == 2:
		// Stereo is implicit for basic tracks unless needed for disambiguation
		if len(parts) == 1 && !acc.AudioDescription && !acc.ClearDialogue {
			parts = append(parts, "Stereo")
		}
	}

	if len(parts) == 1 {
		return parts[0]
	}
	return fmt.Sprintf("%s – %s", parts[0], parts[1])
}

func languageDisplayName(lang LanguageInfo) string {
	switch lang.ISO639_1 {
	case "de":
		return "Deutsch"
	case "fr":
		return "Französisch"
	case "en":
		return "Englisch"
	case "it":
		return "Italienisch"
	case "es":
		return "Spanisch"
	default:
		if lang.IsOriginal {
			return "Originalton"
		}
		if lang.IsUndefined {
			return ""
		}
		return strings.ToUpper(lang.ISO639_2)
	}
}
