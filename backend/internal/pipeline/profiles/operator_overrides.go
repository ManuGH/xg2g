package profiles

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type OperatorOverrideSnapshot struct {
	ForcedIntent          playbackprofile.PlaybackIntent
	MaxQualityRung        playbackprofile.QualityRung
	DisableClientFallback bool
	RuleName              string
	RuleScope             string
	OverrideApplied       bool
}

func ResolveRequestedProfileWithOperatorOverride(requestedProfileID string, operator config.PlaybackOperatorConfig) (string, OperatorOverrideSnapshot) {
	return ResolveRequestedProfileWithSourceOperatorOverride(requestedProfileID, OperatorRuleModeAny, "", operator)
}

func ResolveRequestedProfileWithSourceOperatorOverride(requestedProfileID, mode, serviceRef string, operator config.PlaybackOperatorConfig) (string, OperatorOverrideSnapshot) {
	effectiveOperator, matchedRule := ResolveEffectivePlaybackOperatorConfig(operator, mode, serviceRef)
	canonical := NormalizeRequestedProfileID(requestedProfileID)
	forcedIntent := playbackprofile.NormalizeRequestedIntent(effectiveOperator.ForceIntent)
	maxQualityRung := playbackprofile.NormalizeQualityRung(effectiveOperator.MaxQualityRung)
	snapshot := OperatorOverrideSnapshot{
		ForcedIntent:          forcedIntent,
		MaxQualityRung:        maxQualityRung,
		DisableClientFallback: effectiveOperator.DisableClientFallback,
	}
	if matchedRule != nil {
		snapshot.RuleName = strings.TrimSpace(matchedRule.Name)
		snapshot.RuleScope = NormalizeOperatorRuleMode(matchedRule.Mode)
	}
	snapshot.OverrideApplied = matchedRule != nil || forcedIntent != playbackprofile.IntentUnknown || maxQualityRung != playbackprofile.RungUnknown || snapshot.DisableClientFallback

	if forcedIntent == playbackprofile.IntentUnknown {
		return canonical, snapshot
	}

	effective := canonical
	switch forcedIntent {
	case playbackprofile.IntentDirect:
		effective = ProfileCopy
	case playbackprofile.IntentCompatible, playbackprofile.IntentQuality:
		effective = ProfileHigh
	case playbackprofile.IntentRepair:
		effective = ProfileRepair
	}
	return effective, snapshot
}

func ResolveEffectivePlaybackOperatorConfig(operator config.PlaybackOperatorConfig, mode, serviceRef string) (config.PlaybackOperatorConfig, *config.PlaybackOperatorRuleConfig) {
	effective := operator
	rule, matched := ResolveMatchingOperatorRule(operator.SourceRules, mode, serviceRef)
	if !matched || rule == nil {
		return effective, nil
	}
	if forceIntent := strings.TrimSpace(rule.ForceIntent); forceIntent != "" {
		effective.ForceIntent = forceIntent
	}
	if maxQualityRung := strings.TrimSpace(rule.MaxQualityRung); maxQualityRung != "" {
		effective.MaxQualityRung = maxQualityRung
	}
	if rule.DisableClientFallback != nil {
		effective.DisableClientFallback = *rule.DisableClientFallback
	}
	return effective, rule
}

func ApplyMaxQualityRung(spec model.ProfileSpec, maxRung playbackprofile.QualityRung) (model.ProfileSpec, bool) {
	maxAudioBitrate := playbackprofile.MaxAudioBitrateForRung(maxRung)
	if maxAudioBitrate <= 0 || spec.AudioBitrateK <= 0 {
		return spec, false
	}
	if spec.AudioBitrateK <= maxAudioBitrate {
		return spec, false
	}
	spec.AudioBitrateK = maxAudioBitrate
	return spec, true
}

func ApplyHostPressureOverride(requestedProfileID string, band playbackprofile.HostPressureBand) (string, bool) {
	if normalizedBand := playbackprofile.NormalizeHostPressureBand(string(band)); normalizedBand != playbackprofile.HostPressureConstrained && normalizedBand != playbackprofile.HostPressureCritical {
		return NormalizeRequestedProfileID(requestedProfileID), false
	}

	switch NormalizeRequestedProfileID(requestedProfileID) {
	case ProfileSafariHEVC, ProfileSafariHEVCHW, ProfileSafariHEVCHWLL, ProfileAV1HW:
		return ProfileHigh, true
	default:
		return NormalizeRequestedProfileID(requestedProfileID), false
	}
}
