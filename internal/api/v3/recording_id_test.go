package v3

import (
	"encoding/base64"
	"testing"
)

func TestRecordingIDEncodeDecode(t *testing.T) {
	s := &Server{}
	serviceRef := "1:0:1:445D:453:1:C00000:0:0:0:/media/hdd/movie/foo.ts"

	id := EncodeRecordingID(serviceRef)
	if id == "" {
		t.Fatal("expected non-empty recording_id")
	}

	got := s.DecodeRecordingID(id)
	if got != serviceRef {
		t.Fatalf("expected %q, got %q", serviceRef, got)
	}
}

func TestRecordingIDDecodeRejectsInvalid(t *testing.T) {
	s := &Server{}
	cases := []string{
		"",
		" ",
		"abc",
		"abcd+efghijklmnop",
		"abcd/efghijklmnop",
		"YWJjZGVmZ2hpag==",
		"ABCDEFGHIJKLMNOPQ",
		"%2F",
		"////",
		"abcd1234567890", // too short
	}

	for _, input := range cases {
		if got := s.DecodeRecordingID(input); got != "" {
			t.Fatalf("expected decode to reject %q, got %q", input, got)
		}
	}
}

func TestRecordingIDDecodeRejectsWhitespacePayload(t *testing.T) {
	s := &Server{}
	id := base64.RawURLEncoding.EncodeToString([]byte("   "))
	if got := s.DecodeRecordingID(id); got != "" {
		t.Fatalf("expected whitespace payload to be rejected, got %q", got)
	}
}
