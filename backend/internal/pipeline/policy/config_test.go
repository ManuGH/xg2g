package policy

import (
	"errors"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name      string
		envVal    string
		wantMode  PreemptionMode
		wantErrIs error
	}{
		{"EmptyString_DefaultsToDisabled", "", PreemptionModeDisabled, nil},
		{"Whitespace_DefaultsToDisabled", "   ", PreemptionModeDisabled, nil},
		{"DisabledMode", "disabled", PreemptionModeDisabled, nil},
		{"AuditOnlyMode", "audit-only", PreemptionModeAuditOnly, nil},
		{"EnforceMode_ForbiddenInStepE2", "enforce", "", ErrInvalidPreemptionMode},
		{"UnknownMode_Rejected", "invalid-mode-string", "", ErrInvalidPreemptionMode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(tt.envVal)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("expected error %v, got %v", tt.wantErrIs, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.Mode != tt.wantMode {
					t.Errorf("expected mode %s, got %s", tt.wantMode, cfg.Mode)
				}
			}
		})
	}
}
