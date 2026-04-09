package recordings

import (
	"strings"
	"testing"
)

func TestCanonicalResumeKeyFromRecordingIDMatchesServiceRefEncoding(t *testing.T) {
	const serviceRef = "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"
	recordingID := EncodeRecordingID(serviceRef)

	fromRef, ok := CanonicalResumeKeyFromServiceRef(serviceRef)
	if !ok {
		t.Fatal("expected valid serviceRef to yield canonical resume key")
	}
	if fromRef != recordingID {
		t.Fatalf("expected serviceRef canonical key %q, got %q", recordingID, fromRef)
	}

	fromID, ok := CanonicalResumeKeyFromRecordingID(strings.ToUpper(recordingID))
	if !ok {
		t.Fatal("expected valid recordingID to yield canonical resume key")
	}
	if fromID != recordingID {
		t.Fatalf("expected recordingID canonical key %q, got %q", recordingID, fromID)
	}
}

func TestCanonicalResumeKeyFromRecordingIDRejectsInvalidInput(t *testing.T) {
	if _, ok := CanonicalResumeKeyFromRecordingID("not-hex"); ok {
		t.Fatal("expected invalid recordingID to be rejected")
	}
}
