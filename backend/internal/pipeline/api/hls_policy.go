// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"bufio"
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type hlsPlaylistMetrics struct {
	TargetDurationSec        int
	RecentAvgSegmentDuration float64
	RecentSegmentCount       int
}

type hlsStartupPolicy struct {
	ClientFamily       string
	StartupHeadroomSec int
	Mode               string
	Reasons            []string
}

func deriveHLSStartupPolicy(rec *model.SessionRecord, content []byte) hlsStartupPolicy {
	const (
		defaultHeadroomSec = 8
		minHeadroomSec     = 6
		maxHeadroomSec     = 12
		targetSafetyFactor = 2
		segmentSafetyCount = 4
		traceGuardedSec    = 10
		traceConservative  = 12
	)

	metrics := parseHLSPlaylistMetrics(content)
	clientFamily := sessionPlaybackClientFamily(rec)
	headroom := defaultHeadroomSec
	mode := "balanced"
	reasons := make([]string, 0, 4)

	if metrics.TargetDurationSec > 0 {
		targetHeadroom := metrics.TargetDurationSec * targetSafetyFactor
		if targetHeadroom > headroom {
			headroom = targetHeadroom
			reasons = append(reasons, "target_duration_guard")
		}
	}
	if metrics.RecentSegmentCount > 0 {
		cadenceHeadroom := int(math.Ceil(metrics.RecentAvgSegmentDuration * segmentSafetyCount))
		if cadenceHeadroom > headroom {
			headroom = cadenceHeadroom
			reasons = append(reasons, "segment_cadence_guard")
		}
	}

	switch clientFamily {
	case "ios_safari_native":
		reasons = append(reasons, "client_family_ios_native")
		if 10 > headroom {
			headroom = 10
		}
		mode = "native_guarded"
	case "safari_native", "android_tv_native":
		reasons = append(reasons, "client_family_native")
		if 8 > headroom {
			headroom = 8
		}
		mode = "native_guarded"
	}

	evidenceCount, evidenceReasons := hlsStartupRiskEvidence(rec)
	switch {
	case evidenceCount >= 2:
		if traceConservative > headroom {
			headroom = traceConservative
		}
		reasons = append(reasons, evidenceReasons...)
		mode = "trace_conservative"
	case evidenceCount == 1:
		if traceGuardedSec > headroom {
			headroom = traceGuardedSec
		}
		reasons = append(reasons, evidenceReasons...)
		if mode == "balanced" {
			mode = "trace_guarded"
		}
	}

	if headroom < minHeadroomSec {
		headroom = minHeadroomSec
	}
	if headroom > maxHeadroomSec {
		headroom = maxHeadroomSec
	}

	return hlsStartupPolicy{
		ClientFamily:       clientFamily,
		StartupHeadroomSec: headroom,
		Mode:               mode,
		Reasons:            uniqueStrings(reasons),
	}
}

func parseHLSPlaylistMetrics(content []byte) hlsPlaylistMetrics {
	const recentSegmentCount = 6

	metrics := hlsPlaylistMetrics{}
	recentDurations := make([]float64, 0, recentSegmentCount)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))
			target, err := strconv.Atoi(raw)
			if err == nil && target > 0 {
				metrics.TargetDurationSec = target
			}
			continue
		}
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}

		raw := strings.TrimSpace(strings.TrimPrefix(line, "#EXTINF:"))
		raw = strings.TrimSuffix(raw, ",")
		duration, err := strconv.ParseFloat(raw, 64)
		if err != nil || duration <= 0 {
			continue
		}
		if len(recentDurations) == recentSegmentCount {
			copy(recentDurations, recentDurations[1:])
			recentDurations[len(recentDurations)-1] = duration
		} else {
			recentDurations = append(recentDurations, duration)
		}
	}

	if len(recentDurations) == 0 {
		return metrics
	}
	sum := 0.0
	for _, duration := range recentDurations {
		sum += duration
	}
	metrics.RecentSegmentCount = len(recentDurations)
	metrics.RecentAvgSegmentDuration = sum / float64(len(recentDurations))
	return metrics
}

func sessionPlaybackClientFamily(rec *model.SessionRecord) string {
	if rec == nil {
		return ""
	}
	if rec.PlaybackTrace != nil && rec.PlaybackTrace.Client != nil {
		if family := strings.TrimSpace(rec.PlaybackTrace.Client.ClientFamily); family != "" {
			return family
		}
	}
	if rec.ContextData != nil {
		return strings.TrimSpace(rec.ContextData[model.CtxKeyClientFamily])
	}
	return ""
}

func hlsStartupRiskEvidence(rec *model.SessionRecord) (int, []string) {
	if rec == nil || rec.PlaybackTrace == nil || rec.PlaybackTrace.HLS == nil {
		return 0, nil
	}
	trace := rec.PlaybackTrace.HLS
	evidence := 0
	reasons := make([]string, 0, 3)

	if trace.LatestSegmentLagMs >= durationToMilliseconds(hlsProducerSlowLag) {
		evidence++
		reasons = append(reasons, "trace_producer_lag")
	}
	if trace.LastSegmentGapMs >= durationToMilliseconds(hlsSegmentStaleAfter) {
		evidence++
		reasons = append(reasons, "trace_segment_gap")
	}
	if trace.PlaylistRequestCount >= hlsPlaylistOnlyPollThreshold && trace.SegmentRequestCount == 0 {
		evidence++
		reasons = append(reasons, "trace_playlist_only")
	}
	return evidence, reasons
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
