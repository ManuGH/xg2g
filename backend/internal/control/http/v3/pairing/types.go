package pairing

import (
	"context"
	"errors"
	"time"

	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

type Generator interface {
	NewPairingID(ctx context.Context) (string, error)
	NewPairingSecret(ctx context.Context) (string, error)
	NewUserCode(ctx context.Context) (string, error)
	NewDeviceID(ctx context.Context) (string, error)
	NewDeviceGrantID(ctx context.Context) (string, error)
	NewDeviceGrantSecret(ctx context.Context) (string, error)
	NewAccessSessionID(ctx context.Context) (string, error)
	NewAccessToken(ctx context.Context) (string, error)
}

type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

type PublishedEndpointsProvider interface {
	PublishedEndpoints(ctx context.Context) ([]connectivitydomain.PublishedEndpoint, error)
}

type AuditEvent struct {
	Action    string
	PairingID string
	DeviceID  string
	GrantID   string
	SessionID string
	OwnerID   string
	Outcome   string
	Reason    string
	At        time.Time
}

type Deps struct {
	StateStore                 deviceauthstore.StateStore
	PublishedEndpointsProvider PublishedEndpointsProvider
	AuditSink                  AuditSink
	Generator                  Generator
	Now                        func() time.Time
	PairingTTL                 time.Duration
	DeviceGrantTTL             time.Duration
	DeviceGrantRotateAfter     time.Duration
	AccessSessionTTL           time.Duration
	DefaultScopes              []string
	PolicyVersion              string
	AuthStrength               string
}

type StartInput struct {
	DeviceName             string
	DeviceType             deviceauthmodel.DeviceType
	RequestedPolicyProfile string
}

type StartResult struct {
	PairingID     string
	PairingSecret string
	UserCode      string
	QRPayload     string
	ExpiresAt     time.Time
}

type StatusInput struct {
	PairingID     string
	PairingSecret string
}

type StatusResult struct {
	PairingID              string
	Status                 deviceauthmodel.PairingStatus
	UserCode               string
	DeviceName             string
	DeviceType             deviceauthmodel.DeviceType
	RequestedPolicyProfile string
	ApprovedPolicyProfile  string
	ExpiresAt              time.Time
	ApprovedAt             *time.Time
	ConsumedAt             *time.Time
}

type ApproveInput struct {
	PairingID             string
	OwnerID               string
	ApprovedPolicyProfile string
}

type ApproveResult struct {
	PairingID             string
	Status                deviceauthmodel.PairingStatus
	OwnerID               string
	ApprovedPolicyProfile string
	ApprovedAt            *time.Time
	ExpiresAt             time.Time
}

type ExchangeInput struct {
	PairingID     string
	PairingSecret string
}

type ExchangeResult struct {
	PairingID            string
	DeviceID             string
	DeviceGrantID        string
	DeviceGrant          string
	DeviceGrantExpiresAt time.Time
	AccessSessionID      string
	AccessToken          string
	AccessTokenExpiresAt time.Time
	PolicyVersion        string
	Scopes               []string
	Endpoints            []connectivitydomain.PublishedEndpoint
}

type ErrorKind uint8

const (
	ErrorInvalidInput ErrorKind = iota
	ErrorNotFound
	ErrorConflict
	ErrorForbidden
	ErrorPending
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
	return "pairing service error"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func IsStoreError(err error) bool {
	return errors.Is(err, deviceauthstore.ErrNotFound) || errors.Is(err, deviceauthstore.ErrConflict)
}
