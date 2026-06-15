package recordings

import (
	"encoding/base64"
	"testing"

	"github.com/ManuGH/xg2g/internal/household"
)

// M1: the household DVR access gate decoded recording IDs with the strict hex-only
// DecodeRecordingID and FAILED OPEN when that returned false. The artifact resolver,
// however, also accepts base64 encodings — so a restricted profile could reach a forbidden
// recording by base64-encoding its ID. The shared DecodeRecordingRef closes that gap: the
// gate now resolves the same serviceRef the resolver will serve and enforces the profile.
func TestRecordingAccessGate_Base64ForbiddenRefIsDenied(t *testing.T) {
	const allowedRef = "1:0:1:100:200:300:0:0:0:0:"
	const forbiddenRef = "1:0:1:999:888:777:0:0:0:0:"

	// Restricted profile: allowed only the one service.
	profile := household.NormalizeProfile(household.Profile{
		AllowedServiceRefs: []string{allowedRef},
	})

	// Attacker base64-encodes the FORBIDDEN recording's ID to dodge the hex-only gate.
	b64ID := base64.StdEncoding.EncodeToString([]byte(forbiddenRef))

	// Old gate path: the strict hex decoder rejects the base64 form, so the gate took the
	// "!decoded → return profile, true" branch = ALLOW. This precondition pins that gap.
	if _, ok := DecodeRecordingID(b64ID); ok {
		t.Fatalf("precondition: strict DecodeRecordingID must reject the base64 form")
	}

	// New gate path: the shared decoder resolves the forbidden ref, and the profile check
	// must deny it.
	ref, ok := DecodeRecordingRef(b64ID)
	if !ok {
		t.Fatalf("shared DecodeRecordingRef must resolve the base64 ID")
	}
	if household.IsServiceAllowedNormalized(profile, ref, "") {
		t.Fatalf("SECURITY: base64-encoded forbidden ref %q must be denied for a restricted profile", ref)
	}

	// Sanity: the allowed recording (canonical hex ID) stays accessible.
	allowedID := EncodeRecordingID(allowedRef)
	aref, ok := DecodeRecordingRef(allowedID)
	if !ok || !household.IsServiceAllowedNormalized(profile, aref, "") {
		t.Fatalf("allowed recording must remain accessible (ref=%q ok=%v)", aref, ok)
	}
}
