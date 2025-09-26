package openwebif

import (
	"os"
	"strings"
	"testing"
)

func TestStreamURLUsesEnvPort(t *testing.T) {
	t.Setenv("XG2G_STREAM_PORT", "17999")
	got := StreamURL("http://127.0.0.1", "REF")
	if !strings.Contains(got, ":17999/") {
		t.Fatalf("expected port :17999 in %q", got)
	}
}
