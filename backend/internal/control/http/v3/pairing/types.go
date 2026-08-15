package pairing

import (
	"context"
	"errors"
	"time"

	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	"github.com/ManuGH/xg2g/internal/domain/identity"
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
	DeviceEnroller             DeviceEnroller
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
	// DeviceJWK is the device's P-256 public key. Required: without it there is
	// no cryptographic identity to bind the grant to.
	DeviceJWK identity.JWKECPublicKey
}

// ExchangeResult is identity-shaped on purpose.
//
// The old deviceauth fields (deviceGrant, deviceGrantId, accessSessionId) are
// gone because the concepts are gone: the rotating secret is now a refresh
// token in an identity refresh family, and the access token is DPoP-bound to
// the device key rather than tied to a separate session row.
type ExchangeResult struct {
	PairingID     string
	DeviceID      string
	TokenType     string
	AccessToken   string
	RefreshToken  string
	ExpiresIn     int
	Scope         string
	PolicyVersion string
	Endpoints     []connectivitydomain.PublishedEndpoint
}

// DeviceEnroller is the identity side of an approved pairing.
//
// A narrow port rather than the whole identity service: the pairing package
// must not learn how devices, grants, refresh families or DPoP tokens are
// built. It knows only that an approved pairing plus a validated key yields
// exactly one bound device identity.
type DeviceEnroller interface {
	// ResolveOwner maps the pairing's owner to a canonical identity user.
	//
	// It must never create a user and must never fall back to a synthesised id.
	// An unknown or ambiguous owner is an error, raised before the pairing is
	// consumed so nothing is burnt by a misconfiguration.
	ResolveOwner(ctx context.Context, ownerID string) (string, error)

	// EnrollDevice registers — or re-uses, keyed by the server-computed
	// thumbprint — the device identity, then issues its bound grant and tokens.
	EnrollDevice(ctx context.Context, in EnrollDeviceInput) (*EnrollDeviceResult, error)
}

type EnrollDeviceInput struct {
	UserID     string
	DeviceName string
	Platform   string
	JWK        identity.JWKECPublicKey
	Scopes     string
}

type EnrollDeviceResult struct {
	DeviceID     string
	TokenType    string
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Scope        string
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
