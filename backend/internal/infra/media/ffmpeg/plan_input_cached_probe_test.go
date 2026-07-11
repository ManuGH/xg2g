package ffmpeg

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func TestPlanInput_UsesOneShotFastProbeAfterSuccessfulChannel(t *testing.T) {
	adapter := &LocalAdapter{
		StreamRelayAnalyzeDuration: "5000000",
		StreamRelayProbeSize:       "20M",
		CachedRelayAnalyzeDuration: "2500000",
		CachedRelayProbeSize:       "10M",
		fastProbeReady:             make(map[string]time.Time),
		Logger:                     zerolog.Nop(),
	}
	spec := ports.StreamSpec{
		SessionID: "cached-probe",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source: ports.StreamSource{
			ID:   "1:0:19:132F:3EF:1:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
		Profile: ports.ProfileSpec{TranscodeVideo: false},
	}
	inputURL := "http://receiver.example:17999/1:0:19:132F:3EF:1:C00000:0:0:0"
	adapter.markFastProbeEligible(fpsCacheKey(spec.Source, inputURL))

	first, err := adapter.planInput(spec, inputURL)
	if err != nil {
		t.Fatalf("first planInput: %v", err)
	}
	if got, _ := valueAfter(first.args, "-analyzeduration"); got != "2500000" {
		t.Fatalf("cached analyzeduration = %q, want 2500000", got)
	}
	if got, _ := valueAfter(first.args, "-probesize"); got != "10M" {
		t.Fatalf("cached probesize = %q, want 10M", got)
	}

	second, err := adapter.planInput(spec, inputURL)
	if err != nil {
		t.Fatalf("second planInput: %v", err)
	}
	if got, _ := valueAfter(second.args, "-analyzeduration"); got != "5000000" {
		t.Fatalf("one-shot fallback analyzeduration = %q, want 5000000", got)
	}
	if got, _ := valueAfter(second.args, "-probesize"); got != "20M" {
		t.Fatalf("one-shot fallback probesize = %q, want 20M", got)
	}
}

func TestPlanInput_UsesPersistedMediaTruthOnceThenFallsBackDeep(t *testing.T) {
	adapter := &LocalAdapter{
		StreamRelayAnalyzeDuration: "5000000",
		StreamRelayProbeSize:       "20M",
		CachedRelayAnalyzeDuration: "2500000",
		CachedRelayProbeSize:       "10M",
		fastProbeReady:             make(map[string]time.Time),
		fastProbeUsed:              make(map[string]time.Time),
		Logger:                     zerolog.Nop(),
	}
	spec := ports.StreamSpec{
		SessionID: "persisted-truth",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Source:    ports.StreamSource{ID: "1:0:19:132F:3EF:1:C00000:0:0:0", Type: ports.SourceTuner},
		Profile: ports.ProfileSpec{
			TranscodeVideo:      false,
			SourceTruthVerified: true,
			SourceVideoCodec:    "h264",
			SourceAudioCodec:    "eac3",
		},
	}
	inputURL := "http://receiver.example:17999/1:0:19:132F:3EF:1:C00000:0:0:0"

	first, err := adapter.planInput(spec, inputURL)
	if err != nil {
		t.Fatalf("first planInput: %v", err)
	}
	if got, _ := valueAfter(first.args, "-analyzeduration"); got != "2500000" {
		t.Fatalf("persisted-truth analyzeduration = %q, want 2500000", got)
	}

	second, err := adapter.planInput(spec, inputURL)
	if err != nil {
		t.Fatalf("second planInput: %v", err)
	}
	if got, _ := valueAfter(second.args, "-analyzeduration"); got != "5000000" {
		t.Fatalf("retry analyzeduration = %q, want deep 5000000", got)
	}
}
