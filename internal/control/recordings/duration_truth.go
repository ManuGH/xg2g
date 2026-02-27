package recordings

import (
	"math"
	"sort"
)

// DurationTruthSource encodes where the selected duration came from.
type DurationTruthSource string

const (
	DurationTruthSourceMetadata  DurationTruthSource = "source_metadata"
	DurationTruthSourceFFProbe   DurationTruthSource = "ffprobe"
	DurationTruthSourceContainer DurationTruthSource = "container"
	DurationTruthSourceHeuristic DurationTruthSource = "heuristic"
	DurationTruthSourceUnknown   DurationTruthSource = "unknown"
)

var validDurationTruthSources = map[DurationTruthSource]bool{
	DurationTruthSourceMetadata:  true,
	DurationTruthSourceFFProbe:   true,
	DurationTruthSourceContainer: true,
	DurationTruthSourceHeuristic: true,
	DurationTruthSourceUnknown:   true,
}

func (s DurationTruthSource) Valid() bool {
	return validDurationTruthSources[s]
}

func AllDurationTruthSources() []DurationTruthSource {
	return []DurationTruthSource{
		DurationTruthSourceMetadata,
		DurationTruthSourceFFProbe,
		DurationTruthSourceContainer,
		DurationTruthSourceHeuristic,
		DurationTruthSourceUnknown,
	}
}

// DurationTruthConfidence indicates how reliable the chosen duration is.
type DurationTruthConfidence string

const (
	DurationTruthConfidenceHigh   DurationTruthConfidence = "high"
	DurationTruthConfidenceMedium DurationTruthConfidence = "medium"
	DurationTruthConfidenceLow    DurationTruthConfidence = "low"
)

var validDurationTruthConfidence = map[DurationTruthConfidence]bool{
	DurationTruthConfidenceHigh:   true,
	DurationTruthConfidenceMedium: true,
	DurationTruthConfidenceLow:    true,
}

func (c DurationTruthConfidence) Valid() bool {
	return validDurationTruthConfidence[c]
}

func AllDurationTruthConfidence() []DurationTruthConfidence {
	return []DurationTruthConfidence{
		DurationTruthConfidenceHigh,
		DurationTruthConfidenceMedium,
		DurationTruthConfidenceLow,
	}
}

// DurationReasonCode is the frozen reason vocabulary for duration provenance/fallbacks.
type DurationReasonCode string

const (
	DurationReasonFromSourceMetadata DurationReasonCode = "duration_from_source_metadata"
	DurationReasonFromFFProbe        DurationReasonCode = "duration_from_ffprobe"
	DurationReasonFromContainer      DurationReasonCode = "duration_from_container"
	DurationReasonFromHeuristic      DurationReasonCode = "duration_from_heuristic"
	DurationReasonPrimaryMissing     DurationReasonCode = "duration_primary_missing"
	DurationReasonProbeFailed        DurationReasonCode = "duration_probe_failed"
	DurationReasonContainerMissing   DurationReasonCode = "duration_container_missing"
	DurationReasonInconsistentClamp  DurationReasonCode = "duration_inconsistent_clamped"
	DurationReasonUnknownDeniedSeek  DurationReasonCode = "duration_unknown_denied_seek"
	DurationReasonResumeClamped      DurationReasonCode = "resume_clamped_to_duration"
)

var validDurationReasonCodes = map[DurationReasonCode]bool{
	DurationReasonFromSourceMetadata: true,
	DurationReasonFromFFProbe:        true,
	DurationReasonFromContainer:      true,
	DurationReasonFromHeuristic:      true,
	DurationReasonPrimaryMissing:     true,
	DurationReasonProbeFailed:        true,
	DurationReasonContainerMissing:   true,
	DurationReasonInconsistentClamp:  true,
	DurationReasonUnknownDeniedSeek:  true,
	DurationReasonResumeClamped:      true,
}

func (r DurationReasonCode) Valid() bool {
	return validDurationReasonCodes[r]
}

func AllDurationReasonCodes() []DurationReasonCode {
	return []DurationReasonCode{
		DurationReasonFromSourceMetadata,
		DurationReasonFromFFProbe,
		DurationReasonFromContainer,
		DurationReasonFromHeuristic,
		DurationReasonPrimaryMissing,
		DurationReasonProbeFailed,
		DurationReasonContainerMissing,
		DurationReasonInconsistentClamp,
		DurationReasonUnknownDeniedSeek,
		DurationReasonResumeClamped,
	}
}

var durationReasonPriority = map[DurationReasonCode]int{
	DurationReasonUnknownDeniedSeek:  0,
	DurationReasonInconsistentClamp:  1,
	DurationReasonProbeFailed:        2,
	DurationReasonContainerMissing:   3,
	DurationReasonPrimaryMissing:     4,
	DurationReasonFromHeuristic:      5,
	DurationReasonFromContainer:      6,
	DurationReasonFromFFProbe:        7,
	DurationReasonFromSourceMetadata: 8,
	DurationReasonResumeClamped:      9,
}

func durationReasonRank(r DurationReasonCode) int {
	if rank, ok := durationReasonPriority[r]; ok {
		return rank
	}
	return 100
}

func sortDurationReasonsByPriority(reasons []DurationReasonCode) {
	sort.SliceStable(reasons, func(i, j int) bool {
		ri := durationReasonRank(reasons[i])
		rj := durationReasonRank(reasons[j])
		if ri != rj {
			return ri < rj
		}
		return reasons[i] < reasons[j]
	})
}

func DurationReasonPrimaryFrom(reasons []DurationReasonCode) DurationReasonCode {
	if len(reasons) == 0 {
		return DurationReasonUnknownDeniedSeek
	}
	best := reasons[0]
	bestRank := durationReasonRank(best)
	for _, r := range reasons[1:] {
		rank := durationReasonRank(r)
		if rank < bestRank || (rank == bestRank && r < best) {
			best = r
			bestRank = rank
		}
	}
	return best
}

// DurationTruth is the normalized duration contract propagated to API/UI layers.
type DurationTruth struct {
	DurationMs *int64
	Source     DurationTruthSource
	Confidence DurationTruthConfidence
	Reasons    []DurationReasonCode
}

func (d DurationTruth) HasDuration() bool {
	return d.DurationMs != nil && *d.DurationMs > 0
}

func (d DurationTruth) DurationSeconds() *int64 {
	if d.DurationMs == nil {
		return nil
	}
	seconds := *d.DurationMs / 1000
	if seconds <= 0 {
		return nil
	}
	return &seconds
}

func (d DurationTruth) ReasonStrings() []string {
	if len(d.Reasons) == 0 {
		return []string{}
	}
	out := make([]string, len(d.Reasons))
	for i, r := range d.Reasons {
		out[i] = string(r)
	}
	return out
}

// DurationTruthResolveInput configures deterministic priority-based selection.
type DurationTruthResolveInput struct {
	PrimaryDurationSeconds   int64 // source metadata
	SecondaryDurationSeconds int64 // ffprobe / container
	SecondarySource          DurationTruthSource
	SecondaryFailed          bool
	HeuristicDurationSeconds int64
	AllowHeuristic           bool
}

const (
	defaultDurationMaxMs = int64(30 * 24 * 60 * 60 * 1000) // 30 days
)

// ResolveDurationTruth applies deterministic precedence:
// primary(source metadata) -> secondary(ffprobe/container) -> tertiary(heuristic) -> unknown.
func ResolveDurationTruth(in DurationTruthResolveInput) DurationTruth {
	out := DurationTruth{
		Source:     DurationTruthSourceUnknown,
		Confidence: DurationTruthConfidenceLow,
		Reasons:    []DurationReasonCode{},
	}

	appendReason := func(reason DurationReasonCode) {
		if !reason.Valid() {
			return
		}
		for _, existing := range out.Reasons {
			if existing == reason {
				return
			}
		}
		out.Reasons = append(out.Reasons, reason)
	}

	normalizeDuration := func(seconds int64) (ms int64, ok bool, clamped bool) {
		if seconds <= 0 {
			return 0, false, false
		}
		if seconds > math.MaxInt64/1000 {
			return defaultDurationMaxMs, true, true
		}
		ms = seconds * 1000
		if ms > defaultDurationMaxMs {
			return defaultDurationMaxMs, true, true
		}
		return ms, true, false
	}

	setDuration := func(ms int64) {
		v := ms
		out.DurationMs = &v
	}

	if ms, ok, clamped := normalizeDuration(in.PrimaryDurationSeconds); ok {
		setDuration(ms)
		out.Source = DurationTruthSourceMetadata
		out.Confidence = DurationTruthConfidenceHigh
		appendReason(DurationReasonFromSourceMetadata)
		if clamped {
			appendReason(DurationReasonInconsistentClamp)
		}
	} else {
		appendReason(DurationReasonPrimaryMissing)
	}

	if !out.HasDuration() {
		secondarySource := in.SecondarySource
		if !secondarySource.Valid() || secondarySource == DurationTruthSourceUnknown || secondarySource == DurationTruthSourceHeuristic {
			secondarySource = DurationTruthSourceFFProbe
		}

		if ms, ok, clamped := normalizeDuration(in.SecondaryDurationSeconds); ok {
			setDuration(ms)
			out.Source = secondarySource
			out.Confidence = DurationTruthConfidenceMedium
			if secondarySource == DurationTruthSourceContainer {
				appendReason(DurationReasonFromContainer)
			} else {
				appendReason(DurationReasonFromFFProbe)
			}
			if clamped {
				appendReason(DurationReasonInconsistentClamp)
			}
		} else {
			if in.SecondaryFailed {
				appendReason(DurationReasonProbeFailed)
			}
			appendReason(DurationReasonContainerMissing)
		}
	}

	if !out.HasDuration() && in.AllowHeuristic {
		if ms, ok, clamped := normalizeDuration(in.HeuristicDurationSeconds); ok {
			setDuration(ms)
			out.Source = DurationTruthSourceHeuristic
			out.Confidence = DurationTruthConfidenceLow
			appendReason(DurationReasonFromHeuristic)
			if clamped {
				appendReason(DurationReasonInconsistentClamp)
			}
		}
	}

	if !out.HasDuration() {
		out.Source = DurationTruthSourceUnknown
		out.Confidence = DurationTruthConfidenceLow
		appendReason(DurationReasonUnknownDeniedSeek)
	}

	sortDurationReasonsByPriority(out.Reasons)
	return out
}
