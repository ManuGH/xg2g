package sqlite

import (
	"strings"
	"testing"
)

func TestBuildDSN_DefaultsToImmediateTxLock(t *testing.T) {
	dsn := buildDSN("/tmp/test.sqlite", DefaultConfig())
	if !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("expected _txlock=immediate in DSN, got %q", dsn)
	}
}

func TestBuildDSN_AllowsExplicitTxLockOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TxLock = "exclusive"

	dsn := buildDSN("/tmp/test.sqlite", cfg)
	if !strings.Contains(dsn, "_txlock=exclusive") {
		t.Fatalf("expected _txlock=exclusive in DSN, got %q", dsn)
	}
}
