package deviceauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	"github.com/ManuGH/xg2g/internal/domain/deviceauth/lifecycle"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

const (
	defaultDeviceGrantTTL         = 30 * 24 * time.Hour
	defaultDeviceGrantRotateAfter = 7 * 24 * time.Hour
	defaultAccessSessionTTL       = 15 * time.Minute
	defaultPolicyVersion          = "device-auth-v1"
	defaultAuthStrength           = "paired_device"
)

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.AuditSink == nil {
		deps.AuditSink = noopAuditSink{}
	}
	if deps.DeviceGrantTTL <= 0 {
		deps.DeviceGrantTTL = defaultDeviceGrantTTL
	}
	if deps.DeviceGrantRotateAfter <= 0 || deps.DeviceGrantRotateAfter >= deps.DeviceGrantTTL {
		deps.DeviceGrantRotateAfter = defaultDeviceGrantRotateAfter
	}
	if deps.AccessSessionTTL <= 0 {
		deps.AccessSessionTTL = defaultAccessSessionTTL
	}
	if len(deps.DefaultScopes) == 0 {
		deps.DefaultScopes = []string{"v3:read", "v3:write"}
	}
	if strings.TrimSpace(deps.PolicyVersion) == "" {
		deps.PolicyVersion = defaultPolicyVersion
	}
	if strings.TrimSpace(deps.AuthStrength) == "" {
		deps.AuthStrength = defaultAuthStrength
	}
	return &Service{deps: deps}
}

type issueAccessSessionInput struct {
	SubjectID     string
	DeviceID      string
	Scopes        []string
	PolicyVersion string
	AuthStrength  string
	IssuedAt      time.Time
}

func (s *Service) issueAccessSession(ctx context.Context, input issueAccessSessionInput) (deviceauthmodel.AccessSessionRecord, string, error) {
	accessSessionID, err := newOpaqueID("dss", 12)
	if err != nil {
		return deviceauthmodel.AccessSessionRecord{}, "", &Error{Kind: ErrorInternal, Message: "failed to generate access session id", Cause: err}
	}
	accessToken, err := newOpaqueSecret(32)
	if err != nil {
		return deviceauthmodel.AccessSessionRecord{}, "", &Error{Kind: ErrorInternal, Message: "failed to generate access token", Cause: err}
	}
	sessionRecord, err := deviceauthmodel.PrepareAccessSessionRecord(deviceauthmodel.AccessSessionRecord{
		SessionID:     accessSessionID,
		SubjectID:     input.SubjectID,
		DeviceID:      input.DeviceID,
		TokenHash:     hashOpaqueSecret(accessToken),
		PolicyVersion: firstNonEmpty(input.PolicyVersion, s.deps.PolicyVersion),
		Scopes:        append([]string(nil), input.Scopes...),
		AuthStrength:  firstNonEmpty(input.AuthStrength, s.deps.AuthStrength),
		IssuedAt:      input.IssuedAt.UTC(),
		ExpiresAt:     input.IssuedAt.UTC().Add(s.deps.AccessSessionTTL),
	})
	if err != nil {
		return deviceauthmodel.AccessSessionRecord{}, "", &Error{Kind: ErrorInternal, Message: "failed to build access session", Cause: err}
	}
	if err := s.deps.StateStore.PutAccessSession(ctx, &sessionRecord); err != nil {
		return deviceauthmodel.AccessSessionRecord{}, "", classifyStoreError("failed to persist access session", err)
	}
	return sessionRecord, accessToken, nil
}

func (s *Service) touchDevice(ctx context.Context, deviceID string, now time.Time) (*deviceauthmodel.DeviceRecord, error) {
	device, err := s.deps.StateStore.UpdateDevice(ctx, deviceID, func(current *deviceauthmodel.DeviceRecord) error {
		lastSeenAt := now
		current.LastSeenAt = &lastSeenAt
		return nil
	})
	if err != nil {
		return nil, classifyStoreError("failed to update device activity", err)
	}
	return device, nil
}

func (s *Service) recordAudit(ctx context.Context, event AuditEvent) {
	event.At = event.At.UTC()
	if event.At.IsZero() {
		event.At = s.now()
	}
	_ = s.deps.AuditSink.Record(ctx, event)
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) publishedEndpoints(ctx context.Context) ([]connectivitydomain.PublishedEndpoint, error) {
	if s.deps.PublishedEndpointsProvider == nil {
		return []connectivitydomain.PublishedEndpoint{}, nil
	}
	endpoints, err := s.deps.PublishedEndpointsProvider.PublishedEndpoints(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "published endpoints are not available", Cause: err}
	}
	return connectivitydomain.ClonePublishedEndpoints(endpoints), nil
}

func classifyStoreError(message string, err error) error {
	switch {
	case errors.Is(err, deviceauthstore.ErrNotFound):
		return &Error{Kind: ErrorNotFound, Message: "device auth record not found", Cause: err}
	case errors.Is(err, deviceauthstore.ErrConflict):
		return &Error{Kind: ErrorConflict, Message: "device auth state conflict", Cause: err}
	default:
		return &Error{Kind: ErrorStore, Message: message, Cause: err}
	}
}

func classifyDeviceGrantClaimError(err error) error {
	switch {
	case errors.Is(err, lifecycle.ErrDeviceGrantSecretMismatch):
		return &Error{Kind: ErrorForbidden, Message: "device grant secret mismatch", Cause: err}
	case errors.Is(err, lifecycle.ErrDeviceGrantAlreadyExpired):
		return &Error{Kind: ErrorExpired, Message: "device grant has expired", Cause: err}
	case errors.Is(err, lifecycle.ErrDeviceGrantAlreadyRevoked):
		return &Error{Kind: ErrorRevoked, Message: "device grant has been revoked", Cause: err}
	default:
		return &Error{Kind: ErrorInternal, Message: "device grant claim failed", Cause: err}
	}
}

func classifyDeviceGrantReason(err error) string {
	switch {
	case errors.Is(err, lifecycle.ErrDeviceGrantSecretMismatch):
		return "secret_mismatch"
	case errors.Is(err, lifecycle.ErrDeviceGrantAlreadyExpired):
		return "expired"
	case errors.Is(err, lifecycle.ErrDeviceGrantAlreadyRevoked):
		return "revoked"
	default:
		return "unknown"
	}
}

func hashOpaqueSecret(secret string) string {
	return deviceauthmodel.HashOpaqueSecret(secret)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneTime(value time.Time) *time.Time {
	cloned := value.UTC()
	return &cloned
}

func newOpaqueID(prefix string, bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(raw)), nil
}

func newOpaqueSecret(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type noopAuditSink struct{}

func (noopAuditSink) Record(context.Context, AuditEvent) error { return nil }
