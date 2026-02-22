package system

import (
	"context"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestValidateSetupBaseURL_DisabledPolicyRejected(t *testing.T) {
	cfg := config.AppConfig{}
	_, err := validateSetupBaseURL(context.Background(), "http://192.0.2.10", cfg)
	if err == nil {
		t.Fatal("expected error when outbound policy is disabled")
	}
	if !strings.Contains(err.Error(), "outbound policy disabled") {
		t.Fatalf("expected outbound disabled error, got: %v", err)
	}
}

func TestValidateSetupBaseURL_AllowlistEnforced(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Network.Outbound.Enabled = true
	cfg.Network.Outbound.Allow = config.OutboundAllowlist{
		Hosts:   []string{"192.0.2.10"},
		Ports:   []int{80},
		Schemes: []string{"http"},
	}

	got, err := validateSetupBaseURL(context.Background(), "http://192.0.2.10/web/about", cfg)
	if err != nil {
		t.Fatalf("expected allowed URL, got error: %v", err)
	}
	if got != "http://192.0.2.10/web/about" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}

	_, err = validateSetupBaseURL(context.Background(), "http://192.0.2.11/web/about", cfg)
	if err == nil {
		t.Fatal("expected allowlist rejection for disallowed host")
	}
}

func TestSafeURLForLog_RedactsUserInfoAndQuery(t *testing.T) {
	got := safeURLForLog("http://user:secret@192.0.2.10:8443/web/about?probe=abc")
	if got != "http://192.0.2.10:8443" {
		t.Fatalf("unexpected log-safe URL: %q", got)
	}
}
