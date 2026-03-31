package intents

import (
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/rs/zerolog"
)

// Intent carries normalized input used by intent processing.
type Intent struct {
	Type          model.IntentType
	SessionID     string
	ServiceRef    string
	Params        map[string]string
	StartMs       *int64
	CorrelationID string
	DecisionTrace string
	Mode          string
	UserAgent     string
	PrincipalID   string
	Logger        zerolog.Logger
}

// Result is the normalized outcome consumed by the v3 HTTP adapter.
type Result struct {
	SessionID     string
	Status        string
	CorrelationID string
}

type ErrorKind uint8

const (
	ErrorInvalidInput ErrorKind = iota
	ErrorAdmissionUnavailable
	ErrorAdmissionRejected
	ErrorNoTunerSlots
	ErrorStoreUnavailable
	ErrorPublishUnavailable
)

// Error captures non-transport intent processing failures.
type Error struct {
	Kind             ErrorKind
	Message          string
	RetryAfter       string
	AdmissionProblem *admission.Problem
	Cause            error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "intent processing error"
}
