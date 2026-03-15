package hardware

import (
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

const (
	minHostCPUSamples             = 15
	elevatedCPULoadPerCore        = 0.75
	constrainedCPULoadPerCore     = 1.00
	criticalCPULoadPerCore        = 1.50
	elevatedSessionUtilization    = 0.85
	constrainedSessionUtilization = 1.00
	elevatedVAAPIUtilization      = 0.75
	constrainedVAAPIUtilization   = 1.00
)

// PressureTracker keeps host pressure hysteresis state between evaluations.
type PressureTracker struct {
	mu    sync.Mutex
	state playbackprofile.HostPressureState
}

// NewPressureTracker creates a tracker with empty hysteresis state.
func NewPressureTracker() *PressureTracker {
	return &PressureTracker{}
}

// Evaluate derives host pressure for the given snapshot and updates the internal hysteresis state.
func (t *PressureTracker) Evaluate(snapshot playbackprofile.HostRuntimeSnapshot) playbackprofile.HostPressureAssessment {
	t.mu.Lock()
	defer t.mu.Unlock()

	assessment := AssessHostPressure(snapshot, t.state)
	t.state = assessment.State
	return assessment
}

// State returns the current hysteresis state.
func (t *PressureTracker) State() playbackprofile.HostPressureState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

// AssessHostPressure derives the raw and hysteresis-stabilized host pressure band.
func AssessHostPressure(snapshot playbackprofile.HostRuntimeSnapshot, previous playbackprofile.HostPressureState) playbackprofile.HostPressureAssessment {
	rawBand, signals := classifyHostPressure(snapshot)
	prevBand := playbackprofile.NormalizeHostPressureBand(string(previous.Band))

	if prevBand == playbackprofile.HostPressureUnknown {
		return playbackprofile.HostPressureAssessment{
			RawBand:       rawBand,
			EffectiveBand: rawBand,
			Signals:       signals,
			State: playbackprofile.HostPressureState{
				Band: rawBand,
			},
		}
	}

	if hostPressureSeverity(rawBand) >= hostPressureSeverity(prevBand) {
		return playbackprofile.HostPressureAssessment{
			RawBand:       rawBand,
			EffectiveBand: rawBand,
			Signals:       signals,
			State: playbackprofile.HostPressureState{
				Band: rawBand,
			},
		}
	}

	recoveryCount := previous.RecoveryCount + 1
	if recoveryCount >= hostPressureRecoveryPasses(prevBand, rawBand) {
		return playbackprofile.HostPressureAssessment{
			RawBand:       rawBand,
			EffectiveBand: rawBand,
			Signals:       signals,
			State: playbackprofile.HostPressureState{
				Band: rawBand,
			},
		}
	}

	return playbackprofile.HostPressureAssessment{
		RawBand:       rawBand,
		EffectiveBand: prevBand,
		Signals:       signals,
		State: playbackprofile.HostPressureState{
			Band:          prevBand,
			RecoveryCount: recoveryCount,
		},
	}
}

func classifyHostPressure(snapshot playbackprofile.HostRuntimeSnapshot) (playbackprofile.HostPressureBand, []playbackprofile.HostPressureSignal) {
	band := playbackprofile.HostPressureNormal
	signals := make([]playbackprofile.HostPressureSignal, 0, 3)

	coreCount := snapshot.CPU.CoreCount
	if coreCount <= 0 || snapshot.CPU.SampleCount < minHostCPUSamples {
		band = maxHostPressureBand(band, playbackprofile.HostPressureConstrained)
		signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalCPUUnknown)
	} else {
		cpuRatio := snapshot.CPU.Load1m / float64(coreCount)
		switch {
		case cpuRatio >= criticalCPULoadPerCore:
			band = maxHostPressureBand(band, playbackprofile.HostPressureCritical)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalCPUCritical)
		case cpuRatio >= constrainedCPULoadPerCore:
			band = maxHostPressureBand(band, playbackprofile.HostPressureConstrained)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalCPUConstrained)
		case cpuRatio >= elevatedCPULoadPerCore:
			band = maxHostPressureBand(band, playbackprofile.HostPressureElevated)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalCPUElevated)
		}
	}

	if maxSessions := snapshot.Concurrency.MaxSessions; maxSessions > 0 {
		sessionRatio := float64(snapshot.Concurrency.SessionsActive) / float64(maxSessions)
		switch {
		case sessionRatio >= constrainedSessionUtilization:
			band = maxHostPressureBand(band, playbackprofile.HostPressureConstrained)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalSessionsSaturated)
		case sessionRatio >= elevatedSessionUtilization:
			band = maxHostPressureBand(band, playbackprofile.HostPressureElevated)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalSessionsElevated)
		}
	}

	if maxTokens := snapshot.Concurrency.MaxVAAPITokens; maxTokens > 0 {
		vaapiRatio := float64(snapshot.Concurrency.ActiveVAAPITokens) / float64(maxTokens)
		switch {
		case vaapiRatio >= constrainedVAAPIUtilization:
			band = maxHostPressureBand(band, playbackprofile.HostPressureConstrained)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalVAAPISaturated)
		case vaapiRatio >= elevatedVAAPIUtilization:
			band = maxHostPressureBand(band, playbackprofile.HostPressureElevated)
			signals = appendHostPressureSignal(signals, playbackprofile.HostPressureSignalVAAPIElevated)
		}
	}

	return band, signals
}

func appendHostPressureSignal(signals []playbackprofile.HostPressureSignal, signal playbackprofile.HostPressureSignal) []playbackprofile.HostPressureSignal {
	for _, existing := range signals {
		if existing == signal {
			return signals
		}
	}
	return append(signals, signal)
}

func maxHostPressureBand(a, b playbackprofile.HostPressureBand) playbackprofile.HostPressureBand {
	if hostPressureSeverity(b) > hostPressureSeverity(a) {
		return b
	}
	return a
}

func hostPressureSeverity(band playbackprofile.HostPressureBand) int {
	switch playbackprofile.NormalizeHostPressureBand(string(band)) {
	case playbackprofile.HostPressureCritical:
		return 4
	case playbackprofile.HostPressureConstrained:
		return 3
	case playbackprofile.HostPressureElevated:
		return 2
	case playbackprofile.HostPressureNormal:
		return 1
	default:
		return 0
	}
}

func hostPressureRecoveryPasses(from, to playbackprofile.HostPressureBand) int {
	delta := hostPressureSeverity(from) - hostPressureSeverity(to)
	switch {
	case delta <= 1:
		return 2
	default:
		return 3
	}
}
