package ffmpeg

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

type fakeDiagnosticLookup struct{ meta ports.DiagnosticMetadata }

func (f fakeDiagnosticLookup) GetDiagnosticMetadata(context.Context, string) (ports.DiagnosticMetadata, bool) {
	return f.meta, true
}

func TestIngestReleaseReason(t *testing.T) {
	tests := []struct {
		name string
		dc   DiagnosticContext
		want string
	}{
		{"explicit client stop is passed on", DiagnosticContext{Reason: "R_CLIENT_STOP"}, "R_CLIENT_STOP"},
		{"internal restart suppresses the explicit stop", DiagnosticContext{Reason: "R_CLIENT_STOP", RestartPending: true}, ""},
		{"no reason stays a non-explicit release", DiagnosticContext{Reason: "none"}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ingestReleaseReason(tt.dc); got != tt.want {
				t.Fatalf("ingestReleaseReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// End of the transcode must hand the real production reason to the shared
// ingest, except for the first half of an internal restart.
func TestReleaseIngestAfterProcess_UsesRestartAwareReason(t *testing.T) {
	cases := []struct {
		name string
		meta ports.DiagnosticMetadata
		want string
	}{
		{"user stop releases explicitly", ports.DiagnosticMetadata{Reason: "R_CLIENT_STOP"}, "R_CLIENT_STOP"},
		{"fallback/policy restart keeps the warm hold", ports.DiagnosticMetadata{Reason: "R_CLIENT_STOP", RestartPending: true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubLiveSource{preamble: []byte("P"), body: io.NopCloser(strings.NewReader("x"))}
			adapter := &LocalAdapter{Logger: zerolog.Nop(), DiagnosticLookup: fakeDiagnosticLookup{meta: tc.meta}}
			adapter.LiveSources = &stubLiveSources{src: stub}

			in, err := adapter.acquireSharedIngestInput(context.Background(), tunerSpec())
			if err != nil {
				t.Fatalf("acquireSharedIngestInput: %v", err)
			}
			adapter.releaseIngestAfterProcess(in, "sess-1")

			if stub.released != 1 {
				t.Fatalf("expected exactly one release, got %d", stub.released)
			}
			if stub.reason != tc.want {
				t.Fatalf("reason handed to the shared ingest = %q, want %q", stub.reason, tc.want)
			}
		})
	}
}
