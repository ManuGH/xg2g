package admission

import (
	"fmt"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
)

// Admission Control Problem Codes (Stable)
const (
	CodeEngineDisabled = "ADMISSION_ENGINE_DISABLED"
	CodeNoTuners       = "ADMISSION_NO_TUNERS"
	CodeSessionsFull   = "ADMISSION_SESSIONS_FULL"
	CodeTranscodesFull = "ADMISSION_TRANSCODES_FULL"
	CodeStateUnknown   = "ADMISSION_STATE_UNKNOWN"
)

// Problem is a lightweight wrapper around RFC7807 data for internal passing.
// This allows the controller to return a pure error value that the transport layer
// can convert to a wire response using problem.Write.
type Problem struct {
	Status int
	Type   string
	Title  string
	Code   string
	Detail string
	Extra  map[string]any
}

func (p *Problem) Error() string {
	return fmt.Sprintf("[%s] %s: %s", p.Code, p.Title, p.Detail)
}

// NewEngineDisabled returns a 503 problem when the engine is explicitly disabled.
func NewEngineDisabled() *Problem {
	return &Problem{
		Status: http.StatusServiceUnavailable,
		Type:   "admission/engine-disabled",
		Title:  "Streaming unavailable",
		Code:   CodeEngineDisabled,
		Detail: "The streaming engine is disabled by configuration.",
	}
}

// NewNoTuners returns a 503 problem when no tuner slots are available.
func NewNoTuners(tunerSlots int) *Problem {
	return &Problem{
		Status: http.StatusServiceUnavailable,
		Type:   "admission/no-tuners",
		Title:  "Streaming unavailable",
		Code:   CodeNoTuners,
		Detail: "No tuner slots are available for streaming.",
		Extra: map[string]any{
			"tuner_slots": tunerSlots,
		},
	}
}

// NewSessionsFull returns a 503 problem when the session limit is reached.
func NewSessionsFull(current, limit int) *Problem {
	return &Problem{
		Status: http.StatusServiceUnavailable,
		Type:   "admission/sessions-full",
		Title:  "Streaming capacity exceeded",
		Code:   CodeSessionsFull,
		Detail: "Maximum number of active sessions reached.",
		Extra: map[string]any{
			"current": current,
			"limit":   limit,
		},
	}
}

// NewTranscodesFull returns a 503 problem when the transcode limit is reached.
func NewTranscodesFull(current, limit int) *Problem {
	return &Problem{
		Status: http.StatusServiceUnavailable,
		Type:   "admission/transcodes-full",
		Title:  "Transcode capacity exceeded",
		Code:   CodeTranscodesFull,
		Detail: "Maximum number of active transcodes reached.",
		Extra: map[string]any{
			"current": current,
			"limit":   limit,
		},
	}
}

// NewStateUnknown returns a 503 problem when runtime state indicates a monitoring failure.
func NewStateUnknown() *Problem {
	return &Problem{
		Status: http.StatusServiceUnavailable,
		Type:   "admission/state-unknown",
		Title:  "Admission state unknown",
		Code:   CodeStateUnknown,
		Detail: "Internal monitoring state is unavailable; failing closed.",
	}
}

// WriteProblem converts an admission.Problem to an HTTP response using the standard problem package.
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem) {
	problem.Write(w, r, p.Status, p.Type, p.Title, p.Code, p.Detail, p.Extra)
}
