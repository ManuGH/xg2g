package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

// HostPressureBand is the coarse-grained host pressure level used by playback policy.
type HostPressureBand string

const (
	HostPressureUnknown     HostPressureBand = ""
	HostPressureNormal      HostPressureBand = "normal"
	HostPressureElevated    HostPressureBand = "elevated"
	HostPressureConstrained HostPressureBand = "constrained"
	HostPressureCritical    HostPressureBand = "critical"
)

// HostPressureSignal records why a host pressure band was selected.
type HostPressureSignal string

const (
	HostPressureSignalCPUUnknown        HostPressureSignal = "cpu_unknown"
	HostPressureSignalCPUElevated       HostPressureSignal = "cpu_elevated"
	HostPressureSignalCPUConstrained    HostPressureSignal = "cpu_constrained"
	HostPressureSignalCPUCritical       HostPressureSignal = "cpu_critical"
	HostPressureSignalSessionsElevated  HostPressureSignal = "sessions_elevated"
	HostPressureSignalSessionsSaturated HostPressureSignal = "sessions_saturated"
	HostPressureSignalVAAPIElevated     HostPressureSignal = "vaapi_elevated"
	HostPressureSignalVAAPISaturated    HostPressureSignal = "vaapi_saturated"
)

// HostPressureState holds the hysteresis state between host pressure evaluations.
type HostPressureState struct {
	Band          HostPressureBand `json:"band,omitempty"`
	RecoveryCount int              `json:"recoveryCount,omitempty"`
}

// HostPressureAssessment is the result of evaluating a runtime snapshot against pressure bands.
type HostPressureAssessment struct {
	RawBand       HostPressureBand     `json:"rawBand,omitempty"`
	EffectiveBand HostPressureBand     `json:"effectiveBand,omitempty"`
	Signals       []HostPressureSignal `json:"signals,omitempty"`
	State         HostPressureState    `json:"state"`
}

// NormalizeHostPressureBand canonicalizes a raw host pressure token.
func NormalizeHostPressureBand(raw string) HostPressureBand {
	switch normalize.Token(raw) {
	case string(HostPressureNormal):
		return HostPressureNormal
	case string(HostPressureElevated):
		return HostPressureElevated
	case string(HostPressureConstrained):
		return HostPressureConstrained
	case string(HostPressureCritical):
		return HostPressureCritical
	default:
		return HostPressureUnknown
	}
}
