// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
)

// counterVecTotal sums a counter vector across every label combination it
// currently carries. The evidence counter's labels are not known up front - a
// wrongly counted track would appear under whichever codec it declared - so a
// test that only read the labels it expects could not see the increment it is
// meant to rule out.
func counterVecTotal(t *testing.T, vec *prometheus.CounterVec) float64 {
	t.Helper()

	ch := make(chan prometheus.Metric, 256)
	vec.Collect(ch)
	close(ch)

	var sum float64
	for m := range ch {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		sum += pb.GetCounter().GetValue()
	}
	return sum
}

func probeResultCount(t *testing.T, result string) float64 {
	t.Helper()

	var pb dto.Metric
	if err := metrics.AudioProbeTotal.WithLabelValues(result).Write(&pb); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return pb.GetCounter().GetValue()
}

func evidenceCount(t *testing.T, codec, result string) float64 {
	t.Helper()

	var pb dto.Metric
	if err := metrics.AudioTopologyEvidenceTotal.WithLabelValues(codec, result).Write(&pb); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return pb.GetCounter().GetValue()
}

func probeMetricsAdapter(t *testing.T) *LocalAdapter {
	t.Helper()

	return NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
}

// probeMetricsSpec reaches the startup probe: live HLS, video transcoded into
// fmp4, and not the native Android TV client that skips the probe by design.
func probeMetricsSpec() ports.StreamSpec {
	return ports.StreamSpec{
		SessionID: "probe-metrics",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:             "av1_hw",
			Container:        "fmp4",
			VideoCodec:       "av1",
			VideoSourceCodec: "h264",
			TranscodeVideo:   true,
			AudioBitrateK:    192,
		},
		Source: ports.StreamSource{
			ID:   "http://10.10.55.64:17999/1:0:19:11:6:85:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}
}

// A broken ffprobe run and a run that saw no audio are different observations,
// and B2 draws different conclusions from them. Neither may be reported as
// topology evidence: no source of channel information was compared, so counting
// one there would put a case in the evidence denominator that was never
// classifiable.
func TestLiveAudioProbeMetrics_FailedAndEmptyAreDistinct(t *testing.T) {
	tests := []struct {
		name    string
		streams []liveAudioStream
		err     error
		want    string
		notWant string
	}{
		{
			name:    "the probe did not complete",
			err:     errors.New("ffprobe exit status 1"),
			want:    metrics.AudioProbeFailed,
			notWant: metrics.AudioProbeEmpty,
		},
		{
			name:    "the probe completed and saw no audio",
			streams: []liveAudioStream{{Index: 0, CodecType: "video", CodecName: "h264"}},
			want:    metrics.AudioProbeEmpty,
			notWant: metrics.AudioProbeFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := probeMetricsAdapter(t)
			adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
				return tc.streams, tc.err
			}

			// The tables declare a track even though the probe supplies nothing.
			// This is the shape that must not be mistaken for evidence.
			pmt := []ports.LiveAudioTrack{{PID: 101, Codec: "ac3", Channels: 2}}

			spec := probeMetricsSpec()
			beforeWant := probeResultCount(t, tc.want)
			beforeOther := probeResultCount(t, tc.notWant)
			beforeAvailable := probeResultCount(t, metrics.AudioProbeAvailable)
			beforeEvidence := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal)

			adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)

			if got := probeResultCount(t, tc.want) - beforeWant; got != 1 {
				t.Errorf("%s delta = %v, want 1", tc.want, got)
			}
			if got := probeResultCount(t, tc.notWant) - beforeOther; got != 0 {
				t.Errorf("%s delta = %v, want 0 - the two outcomes must stay distinguishable", tc.notWant, got)
			}
			if got := probeResultCount(t, metrics.AudioProbeAvailable) - beforeAvailable; got != 0 {
				t.Errorf("available delta = %v, want 0", got)
			}
			if got := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal) - beforeEvidence; got != 0 {
				t.Errorf("topology evidence delta = %v, want 0 - nothing was classified", got)
			}
		})
	}
}

// The successful path records both dimensions exactly once per plan and per
// track: one statement that the probe was there, one statement about where the
// track's channel information came from.
func TestLiveAudioProbeMetrics_AvailableAccompaniesTheClassification(t *testing.T) {
	adapter := probeMetricsAdapter(t)
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return []liveAudioStream{
			{Index: 1, ID: "101", CodecType: "audio", CodecName: "ac3", Channels: 2, ChannelLayout: "stereo", Tags: map[string]string{"language": "deu"}},
		}, nil
	}

	pmt := []ports.LiveAudioTrack{{PID: 101, Codec: "ac3", Language: "deu", Channels: 2}}

	spec := probeMetricsSpec()
	beforeAvailable := probeResultCount(t, metrics.AudioProbeAvailable)
	beforeFailed := probeResultCount(t, metrics.AudioProbeFailed)
	beforeEmpty := probeResultCount(t, metrics.AudioProbeEmpty)
	beforeEvidence := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal)
	beforeExact := evidenceCount(t, "ac3", metrics.AudioEvidenceDeclaredExact)

	adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)

	if got := probeResultCount(t, metrics.AudioProbeAvailable) - beforeAvailable; got != 1 {
		t.Errorf("available delta = %v, want 1", got)
	}
	if got := probeResultCount(t, metrics.AudioProbeFailed) - beforeFailed; got != 0 {
		t.Errorf("failed delta = %v, want 0", got)
	}
	if got := probeResultCount(t, metrics.AudioProbeEmpty) - beforeEmpty; got != 0 {
		t.Errorf("empty delta = %v, want 0", got)
	}
	if got := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal) - beforeEvidence; got != 1 {
		t.Errorf("topology evidence delta = %v, want exactly one outcome for the one track", got)
	}
	if got := evidenceCount(t, "ac3", metrics.AudioEvidenceDeclaredExact) - beforeExact; got != 1 {
		t.Errorf("declared_exact{ac3} delta = %v, want 1", got)
	}
}

// Native Android TV never runs the probe. Counting it would report an absence
// that was never asked for and would drag the availability ratio down with
// sessions the question does not concern.
func TestLiveAudioProbeMetrics_AndroidTVNativeIsNotCounted(t *testing.T) {
	adapter := probeMetricsAdapter(t)
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		t.Fatal("the probe must not run for native Android TV")
		return nil, nil
	}

	spec := probeMetricsSpec()
	spec.ClientFamily = "android_tv_native"

	before := counterVecTotal(t, metrics.AudioProbeTotal)
	beforeEvidence := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal)

	adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, []ports.LiveAudioTrack{{PID: 101, Codec: "ac3", Channels: 2}})

	if got := counterVecTotal(t, metrics.AudioProbeTotal) - before; got != 0 {
		t.Errorf("probe counter delta = %v, want 0", got)
	}
	if got := counterVecTotal(t, metrics.AudioTopologyEvidenceTotal) - beforeEvidence; got != 0 {
		t.Errorf("topology evidence delta = %v, want 0", got)
	}
}

// The facts layer now reports an unstated channel count as unknown instead of as
// stereo, and the multi-track argument builder no longer carries a fallback for a
// zero. Neither may change what reaches ffmpeg: the policy decides stereo, and
// the command line says 2. A regression here would read as "-ac:a:0 0".
func TestPlanLiveAudio_UnknownChannelsStillEncodeAsStereo(t *testing.T) {
	adapter := probeMetricsAdapter(t)
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		// Channels 0 throughout: nothing anywhere names a count.
		return []liveAudioStream{
			{Index: 1, ID: "101", CodecType: "audio", CodecName: "ac3", Tags: map[string]string{"language": "deu"}},
			{Index: 2, ID: "102", CodecType: "audio", CodecName: "ac3", Tags: map[string]string{"language": "eng"}},
		}, nil
	}

	pmt := []ports.LiveAudioTrack{
		{PID: 101, Codec: "ac3", Language: "deu"},
		{PID: 102, Codec: "ac3", Language: "eng"},
	}

	spec := probeMetricsSpec()
	sel := adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)

	if len(sel.Maps) < 2 {
		t.Fatalf("Maps = %v, want both tracks planned", sel.Maps)
	}
	for i := range sel.Maps {
		flag := fmt.Sprintf("-ac:a:%d", i)
		got, ok := valueAfter(sel.AudioArgs, flag)
		if !ok {
			t.Fatalf("%s missing from %v", flag, sel.AudioArgs)
		}
		if got != "2" {
			t.Errorf("%s = %q, want 2 - an unknown layout is still encoded as stereo", flag, got)
		}
	}
}
