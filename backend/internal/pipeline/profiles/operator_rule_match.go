package profiles

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

const (
	OperatorRuleModeAny       = "any"
	OperatorRuleModeLive      = "live"
	OperatorRuleModeRecording = "recording"
)

func NormalizeOperatorRuleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", OperatorRuleModeAny:
		return OperatorRuleModeAny
	case OperatorRuleModeLive:
		return OperatorRuleModeLive
	case OperatorRuleModeRecording:
		return OperatorRuleModeRecording
	default:
		return ""
	}
}

func MatchOperatorRule(rule config.PlaybackOperatorRuleConfig, mode, serviceRef string) bool {
	normMode := NormalizeOperatorRuleMode(mode)
	if normMode == "" {
		return false
	}

	ruleMode := NormalizeOperatorRuleMode(rule.Mode)
	if ruleMode == "" {
		return false
	}
	if ruleMode != OperatorRuleModeAny && ruleMode != normMode {
		return false
	}

	target := strings.TrimSpace(serviceRef)
	if target == "" {
		return false
	}

	exact := strings.TrimSpace(rule.ServiceRef)
	prefix := strings.TrimSpace(rule.ServiceRefPrefix)
	switch {
	case exact != "" && prefix == "":
		return target == exact
	case prefix != "" && exact == "":
		return strings.HasPrefix(target, prefix)
	default:
		return false
	}
}

func ResolveMatchingOperatorRule(rules []config.PlaybackOperatorRuleConfig, mode, serviceRef string) (*config.PlaybackOperatorRuleConfig, bool) {
	for i := range rules {
		if MatchOperatorRule(rules[i], mode, serviceRef) {
			rule := rules[i]
			return &rule, true
		}
	}
	return nil, false
}
