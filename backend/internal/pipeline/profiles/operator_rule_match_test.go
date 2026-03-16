package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestMatchOperatorRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rule       config.PlaybackOperatorRuleConfig
		mode       string
		serviceRef string
		want       bool
	}{
		{
			name: "exact live match",
			rule: config.PlaybackOperatorRuleConfig{Name: "one", Mode: "live", ServiceRef: "1:0:1:abc"},
			mode: "live", serviceRef: "1:0:1:abc", want: true,
		},
		{
			name: "exact mismatch",
			rule: config.PlaybackOperatorRuleConfig{Name: "one", Mode: "live", ServiceRef: "1:0:1:abc"},
			mode: "live", serviceRef: "1:0:1:def", want: false,
		},
		{
			name: "prefix recording match",
			rule: config.PlaybackOperatorRuleConfig{Name: "two", Mode: "recording", ServiceRefPrefix: "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/"},
			mode: "recording", serviceRef: "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/Monk.ts", want: true,
		},
		{
			name: "any mode matches live",
			rule: config.PlaybackOperatorRuleConfig{Name: "any", Mode: "any", ServiceRef: "1:0:1:abc"},
			mode: "live", serviceRef: "1:0:1:abc", want: true,
		},
		{
			name: "invalid mode never matches",
			rule: config.PlaybackOperatorRuleConfig{Name: "bad", Mode: "broken", ServiceRef: "1:0:1:abc"},
			mode: "live", serviceRef: "1:0:1:abc", want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := MatchOperatorRule(tc.rule, tc.mode, tc.serviceRef); got != tc.want {
				t.Fatalf("MatchOperatorRule() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveMatchingOperatorRule_FirstMatchWins(t *testing.T) {
	t.Parallel()

	rules := []config.PlaybackOperatorRuleConfig{
		{Name: "prefix", Mode: "live", ServiceRefPrefix: "1:0:1:"},
		{Name: "exact", Mode: "live", ServiceRef: "1:0:1:abc"},
	}

	got, ok := ResolveMatchingOperatorRule(rules, "live", "1:0:1:abc")
	if !ok {
		t.Fatal("expected rule match")
	}
	if got.Name != "prefix" {
		t.Fatalf("expected first rule to win, got %q", got.Name)
	}
}
