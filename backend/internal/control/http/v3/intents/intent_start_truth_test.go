package intents

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func TestCapabilityTrustedForFastStart(t *testing.T) {
	now := time.Now()
	capability := &scan.Capability{
		State:       scan.CapabilityStateOK,
		Container:   "mpegts",
		VideoCodec:  "h264",
		AudioCodec:  "eac3",
		LastSuccess: now.Add(-time.Hour),
		LastAttempt: now.Add(-time.Hour),
	}
	if !capabilityTrustedForFastStart(capability, now) {
		t.Fatal("fresh complete persisted media truth should enable fast start")
	}

	stale := *capability
	stale.LastSuccess = now.Add(-31 * 24 * time.Hour)
	stale.LastAttempt = stale.LastSuccess
	stale.NextRetryAt = time.Time{}
	if capabilityTrustedForFastStart(&stale, now) {
		t.Fatal("stale media truth must not enable fast start")
	}

	partial := *capability
	partial.State = scan.CapabilityStatePartial
	if capabilityTrustedForFastStart(&partial, now) {
		t.Fatal("partial media truth must not enable fast start")
	}
}
