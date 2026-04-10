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
	defaultWebBootstrapTTL        = 90 * time.Second
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
	if deps.WebBootstrapTTL <= 0 {
		deps.WebBootstrapTTL = defaultWebBootstrapTTL
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

func (s *Service) RefreshSession(ctx context.Context, input RefreshSessionInput) (*RefreshSessionResult, error) {
	if strings.TrimSpace(input.DeviceGrantID) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "device grant id is required"}
	}
	if strings.TrimSpace(input.DeviceGrant) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "device grant is required"}
	}
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "device auth state store is not configured"}
	}

	now := s.now()
	deviceGrantHash := hashOpaqueSecret(input.DeviceGrant)
	var claimErr error
	var claimedGrant deviceauthmodel.DeviceGrantRecord
	var grantNeedsRotation bool
	grant, err := s.deps.StateStore.UpdateDeviceGrant(ctx, strings.TrimSpace(input.DeviceGrantID), func(current *deviceauthmodel.DeviceGrantRecord) error {
		next, needsRotation, err := lifecycle.ClaimDeviceGrant(*current, lifecycle.ClaimDeviceGrantInput{
			GrantHash: deviceGrantHash,
			ClaimedAt: now,
		}, now)
		if err != nil {
			claimErr = err
			claimedGrant = *current
			return nil
		}
		*current = next
		claimedGrant = next
		grantNeedsRotation = needsRotation
		return nil
	})
	if err != nil {
		return nil, classifyStoreError("failed to load device grant", err)
	}
	if grant == nil {
		return nil, &Error{Kind: ErrorNotFound, Message: "device grant not found"}
	}
	if claimErr != nil {
		s.recordAudit(ctx, AuditEvent{
			Action:   "device.session.refresh",
			GrantID:  firstNonEmpty(claimedGrant.GrantID, strings.TrimSpace(input.DeviceGrantID)),
			DeviceID: claimedGrant.DeviceID,
			Outcome:  "denied",
			Reason:   classifyDeviceGrantReason(claimErr),
			At:       now,
		})
		return nil, classifyDeviceGrantClaimError(claimErr)
	}
	claimedGrant = *grant

	device, err := s.deps.StateStore.GetDevice(ctx, claimedGrant.DeviceID)
	if err != nil {
		return nil, classifyStoreError("failed to load enrolled device", err)
	}
	if device == nil {
		return nil, &Error{Kind: ErrorNotFound, Message: "enrolled device not found"}
	}
	if !device.CanIssueSessions(now) {
		return nil, &Error{Kind: ErrorRevoked, Message: "device has been revoked"}
	}

	result := &RefreshSessionResult{
		DeviceID: device.DeviceID,
	}
	activeGrantID := claimedGrant.GrantID

	if grantNeedsRotation {
		rotatedGrantID, err := newOpaqueID("dgr", 12)
		if err != nil {
			return nil, &Error{Kind: ErrorInternal, Message: "failed to generate rotated device grant id", Cause: err}
		}
		rotatedGrantSecret, err := newOpaqueSecret(24)
		if err != nil {
			return nil, &Error{Kind: ErrorInternal, Message: "failed to generate rotated device grant secret", Cause: err}
		}
		rotateAfter := now.Add(s.deps.DeviceGrantRotateAfter)
		rotatedGrantRecord, err := deviceauthmodel.PrepareDeviceGrantRecord(deviceauthmodel.DeviceGrantRecord{
			GrantID:     rotatedGrantID,
			DeviceID:    device.DeviceID,
			GrantHash:   hashOpaqueSecret(rotatedGrantSecret),
			IssuedAt:    now,
			ExpiresAt:   now.Add(s.deps.DeviceGrantTTL),
			RotateAfter: &rotateAfter,
		})
		if err != nil {
			return nil, &Error{Kind: ErrorInternal, Message: "failed to prepare rotated device grant", Cause: err}
		}
		if err := s.deps.StateStore.PutDeviceGrant(ctx, &rotatedGrantRecord); err != nil {
			return nil, classifyStoreError("failed to persist rotated device grant", err)
		}

		activeGrantID = rotatedGrantRecord.GrantID
		result.RotatedDeviceGrantID = rotatedGrantRecord.GrantID
		result.RotatedDeviceGrant = rotatedGrantSecret
		result.RotatedGrantExpiresAt = cloneTime(rotatedGrantRecord.ExpiresAt)
	}

	sessionRecord, accessToken, err := s.issueAccessSession(ctx, issueAccessSessionInput{
		SubjectID:     device.OwnerID,
		DeviceID:      device.DeviceID,
		Scopes:        append([]string(nil), s.deps.DefaultScopes...),
		PolicyVersion: s.deps.PolicyVersion,
		AuthStrength:  s.deps.AuthStrength,
		IssuedAt:      now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.touchDevice(ctx, device.DeviceID, now); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, AuditEvent{
		Action:    "device.session.refresh",
		DeviceID:  device.DeviceID,
		GrantID:   activeGrantID,
		SessionID: sessionRecord.SessionID,
		OwnerID:   device.OwnerID,
		Outcome:   "ok",
		Reason:    "issued",
		At:        now,
	})

	result.AccessSessionID = sessionRecord.SessionID
	result.AccessToken = accessToken
	result.AccessTokenExpiresAt = sessionRecord.ExpiresAt
	result.PolicyVersion = sessionRecord.PolicyVersion
	result.Scopes = append([]string(nil), sessionRecord.Scopes...)
	endpoints, err := s.publishedEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	result.Endpoints = endpoints
	return result, nil
}

func (s *Service) StartWebBootstrap(ctx context.Context, input StartWebBootstrapInput) (*StartWebBootstrapResult, error) {
	if strings.TrimSpace(input.SourceAccessToken) == "" {
		return nil, &Error{Kind: ErrorUnauthorized, Message: "device access bearer token is required"}
	}
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "device auth state store is not configured"}
	}

	now := s.now()
	sourceSession, err := s.deps.StateStore.GetAccessSessionByTokenHash(ctx, hashOpaqueSecret(input.SourceAccessToken))
	if err != nil {
		if errors.Is(err, deviceauthstore.ErrNotFound) {
			return nil, &Error{Kind: ErrorUnauthorized, Message: "device access session not found"}
		}
		return nil, classifyStoreError("failed to resolve source access session", err)
	}
	if sourceSession == nil {
		return nil, &Error{Kind: ErrorUnauthorized, Message: "device access session not found"}
	}
	if sourceSession.IsRevoked() {
		return nil, &Error{Kind: ErrorRevoked, Message: "source access session has been revoked"}
	}
	if !sourceSession.IsActive(now) {
		return nil, &Error{Kind: ErrorExpired, Message: "source access session has expired"}
	}

	device, err := s.deps.StateStore.GetDevice(ctx, sourceSession.DeviceID)
	if err != nil {
		return nil, classifyStoreError("failed to load source device", err)
	}
	if device == nil {
		return nil, &Error{Kind: ErrorNotFound, Message: "source device not found"}
	}
	if !device.CanIssueSessions(now) {
		return nil, &Error{Kind: ErrorRevoked, Message: "source device has been revoked"}
	}

	bootstrapID, err := newOpaqueID("wbs", 12)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate web bootstrap id", Cause: err}
	}
	bootstrapToken, err := newOpaqueSecret(24)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate web bootstrap token", Cause: err}
	}
	record, err := lifecycle.StartWebBootstrap(lifecycle.StartWebBootstrapInput{
		BootstrapID:           bootstrapID,
		BootstrapSecretHash:   hashOpaqueSecret(bootstrapToken),
		SourceAccessSessionID: sourceSession.SessionID,
		TargetPath:            input.TargetPath,
		Now:                   now,
		ExpiresAt:             now.Add(s.deps.WebBootstrapTTL),
	})
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "web bootstrap request is invalid", Cause: err}
	}
	if err := s.deps.StateStore.PutWebBootstrap(ctx, &record); err != nil {
		return nil, classifyStoreError("failed to persist web bootstrap", err)
	}

	s.recordAudit(ctx, AuditEvent{
		Action:      "web.bootstrap.start",
		BootstrapID: record.BootstrapID,
		DeviceID:    sourceSession.DeviceID,
		SessionID:   sourceSession.SessionID,
		OwnerID:     sourceSession.SubjectID,
		Outcome:     "ok",
		Reason:      "issued",
		At:          now,
	})

	return &StartWebBootstrapResult{
		BootstrapID:    record.BootstrapID,
		BootstrapToken: bootstrapToken,
		CompletePath:   fmt.Sprintf("/api/v3/auth/web-bootstrap/%s", record.BootstrapID),
		TargetPath:     record.TargetPath,
		ExpiresAt:      record.ExpiresAt,
	}, nil
}

func (s *Service) CompleteWebBootstrap(ctx context.Context, input CompleteWebBootstrapInput) (*CompleteWebBootstrapResult, error) {
	if strings.TrimSpace(input.BootstrapID) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "web bootstrap id is required"}
	}
	if strings.TrimSpace(input.BootstrapToken) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "web bootstrap token is required"}
	}
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "device auth state store is not configured"}
	}

	now := s.now()
	var lifecycleErr error
	record, err := s.deps.StateStore.UpdateWebBootstrap(ctx, strings.TrimSpace(input.BootstrapID), func(current *deviceauthmodel.WebBootstrapRecord) error {
		next, err := lifecycle.ConsumeWebBootstrap(*current, lifecycle.ConsumeWebBootstrapInput{
			BootstrapSecretHash: hashOpaqueSecret(input.BootstrapToken),
			ConsumedAt:          now,
		}, now)
		if err != nil {
			lifecycleErr = err
			return nil
		}
		*current = next
		return nil
	})
	if err != nil {
		return nil, classifyStoreError("failed to consume web bootstrap", err)
	}
	if lifecycleErr != nil {
		return nil, classifyWebBootstrapError(lifecycleErr)
	}

	sourceSession, err := s.deps.StateStore.GetAccessSession(ctx, record.SourceAccessSessionID)
	if err != nil {
		if errors.Is(err, deviceauthstore.ErrNotFound) {
			return nil, &Error{Kind: ErrorRevoked, Message: "source access session is no longer available"}
		}
		return nil, classifyStoreError("failed to load source access session", err)
	}
	if sourceSession == nil {
		return nil, &Error{Kind: ErrorRevoked, Message: "source access session is no longer available"}
	}
	if sourceSession.IsRevoked() {
		return nil, &Error{Kind: ErrorRevoked, Message: "source access session has been revoked"}
	}
	if !sourceSession.IsActive(now) {
		return nil, &Error{Kind: ErrorExpired, Message: "source access session has expired"}
	}

	device, err := s.deps.StateStore.GetDevice(ctx, sourceSession.DeviceID)
	if err != nil {
		return nil, classifyStoreError("failed to load source device", err)
	}
	if device == nil {
		return nil, &Error{Kind: ErrorRevoked, Message: "source device is no longer available"}
	}
	if !device.CanIssueSessions(now) {
		return nil, &Error{Kind: ErrorRevoked, Message: "source device has been revoked"}
	}

	sessionRecord, accessToken, err := s.issueAccessSession(ctx, issueAccessSessionInput{
		SubjectID:     sourceSession.SubjectID,
		DeviceID:      sourceSession.DeviceID,
		Scopes:        append([]string(nil), sourceSession.Scopes...),
		PolicyVersion: sourceSession.PolicyVersion,
		AuthStrength:  sourceSession.AuthStrength,
		IssuedAt:      now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.touchDevice(ctx, sourceSession.DeviceID, now); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, AuditEvent{
		Action:      "web.bootstrap.complete",
		BootstrapID: record.BootstrapID,
		DeviceID:    sourceSession.DeviceID,
		SessionID:   sessionRecord.SessionID,
		OwnerID:     sourceSession.SubjectID,
		Outcome:     "ok",
		Reason:      "consumed",
		At:          now,
	})

	return &CompleteWebBootstrapResult{
		TargetPath:           record.TargetPath,
		AccessSessionID:      sessionRecord.SessionID,
		AccessToken:          accessToken,
		AccessTokenExpiresAt: sessionRecord.ExpiresAt,
	}, nil
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

func classifyWebBootstrapError(err error) error {
	switch {
	case errors.Is(err, lifecycle.ErrWebBootstrapSecretMismatch):
		return &Error{Kind: ErrorForbidden, Message: "web bootstrap token mismatch", Cause: err}
	case errors.Is(err, lifecycle.ErrWebBootstrapAlreadyExpired):
		return &Error{Kind: ErrorExpired, Message: "web bootstrap has expired", Cause: err}
	case errors.Is(err, lifecycle.ErrWebBootstrapAlreadyConsumed):
		return &Error{Kind: ErrorConsumed, Message: "web bootstrap has already been used", Cause: err}
	case errors.Is(err, lifecycle.ErrWebBootstrapAlreadyRevoked):
		return &Error{Kind: ErrorRevoked, Message: "web bootstrap has been revoked", Cause: err}
	default:
		return &Error{Kind: ErrorInternal, Message: "web bootstrap failed", Cause: err}
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
