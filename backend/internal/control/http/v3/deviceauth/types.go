package deviceauth

import (
	"context"
	"time"

	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

type PublishedEndpointsProvider interface {
	PublishedEndpoints(ctx context.Context) ([]connectivitydomain.PublishedEndpoint, error)
}

type AuditEvent struct {
	Action      string
	DeviceID    string
	GrantID     string
	SessionID   string
	BootstrapID string
	OwnerID     string
	Outcome     string
	Reason      string
	At          time.Time
}

type Deps struct {
	StateStore                 deviceauthstore.StateStore
	PublishedEndpointsProvider PublishedEndpointsProvider
	AuditSink                  AuditSink
	Now                        func() time.Time
	DeviceGrantTTL             time.Duration
	DeviceGrantRotateAfter     time.Duration
	AccessSessionTTL           time.Duration
	DefaultScopes              []string
	PolicyVersion              string
	AuthStrength               string
}

type ErrorKind uint8

const (
	ErrorInvalidInput ErrorKind = iota
	ErrorUnauthorized
	ErrorNotFound
	ErrorForbidden
	ErrorConflict
	ErrorExpired
	ErrorConsumed
	ErrorRevoked
	ErrorStore
	ErrorInternal
)

type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
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
	return "device auth service error"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
