package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func getHistogramCount(t *testing.T, hist prometheus.Observer) uint64 {
	t.Helper()
	h, ok := hist.(prometheus.Histogram)
	if !ok {
		t.Fatalf("observer is not a prometheus.Histogram")
	}
	metric := &dto.Metric{}
	if err := h.Write(metric); err != nil {
		t.Fatalf("write histogram metric: %v", err)
	}
	return metric.GetHistogram().GetSampleCount()
}

func TestPlaybackSLO_StartAndErrorNormalization(t *testing.T) {
	startInitial := getCounterVecValue(t, playbackStartTotal, "unknown", "unknown")
	IncPlaybackStart("custom_schema", "custom_mode")
	startAfter := getCounterVecValue(t, playbackStartTotal, "unknown", "unknown")
	if startAfter != startInitial+1 {
		t.Fatalf("expected playback start unknown/unknown +1, got initial=%v after=%v", startInitial, startAfter)
	}

	errInitial := getCounterVecValue(t, playbackErrorTotal, "recording", "playback_info", "UNKNOWN")
	IncPlaybackError("recording", "playback_info", "custom_code")
	errAfter := getCounterVecValue(t, playbackErrorTotal, "recording", "playback_info", "UNKNOWN")
	if errAfter != errInitial+1 {
		t.Fatalf("expected playback error UNKNOWN +1, got initial=%v after=%v", errInitial, errAfter)
	}
}

func TestPlaybackSLO_TTFFAndRebufferNormalization(t *testing.T) {
	hist := playbackTTFFSeconds.WithLabelValues("live", "hlsjs", "ok")
	before := getHistogramCount(t, hist)
	ObservePlaybackTTFF("live", "hlsjs", "ok", 1.25)
	after := getHistogramCount(t, hist)
	if after != before+1 {
		t.Fatalf("expected TTFF sample count +1, got before=%d after=%d", before, after)
	}

	rebufferInitial := getCounterVecValue(t, playbackRebufferTotal, "unknown", "unknown", "unknown")
	IncPlaybackRebuffer("invalid_schema", "invalid_mode", "invalid_severity")
	rebufferAfter := getCounterVecValue(t, playbackRebufferTotal, "unknown", "unknown", "unknown")
	if rebufferAfter != rebufferInitial+1 {
		t.Fatalf("expected rebuffer unknown labels +1, got initial=%v after=%v", rebufferInitial, rebufferAfter)
	}
}

func TestPlaybackSLO_ErrorCodeAllowlist(t *testing.T) {
	knownInitial := getCounterVecValue(t, playbackErrorTotal, "live", "intent", "ADMISSION_NO_TUNERS")
	IncPlaybackError("live", "intent", "admission_no_tuners")
	knownAfter := getCounterVecValue(t, playbackErrorTotal, "live", "intent", "ADMISSION_NO_TUNERS")
	if knownAfter != knownInitial+1 {
		t.Fatalf("expected known code counter +1, got initial=%v after=%v", knownInitial, knownAfter)
	}

	unknownInitial := getCounterVecValue(t, playbackErrorTotal, "live", "intent", "UNKNOWN")
	IncPlaybackError("live", "intent", "totally_new_code")
	unknownAfter := getCounterVecValue(t, playbackErrorTotal, "live", "intent", "UNKNOWN")
	if unknownAfter != unknownInitial+1 {
		t.Fatalf("expected unknown code counter +1, got initial=%v after=%v", unknownInitial, unknownAfter)
	}
}

func TestPlaybackSLO_NoHighCardinalityLabels(t *testing.T) {
	forbidden := []string{"request_id", "recording_id", "service_ref", "session_id"}
	collectors := []prometheus.Collector{
		playbackTTFFSeconds,
		playbackRebufferTotal,
		playbackErrorTotal,
		playbackStartTotal,
	}

	for _, c := range collectors {
		descCh := make(chan *prometheus.Desc, 10)
		c.Describe(descCh)
		close(descCh)
		for desc := range descCh {
			s := strings.ToLower(desc.String())
			for _, bad := range forbidden {
				if strings.Contains(s, bad) {
					t.Fatalf("collector descriptor contains forbidden high-cardinality label %q: %s", bad, desc.String())
				}
			}
		}
	}
}
