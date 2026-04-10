package pairing

import (
	"context"
	"crypto/rand"
	"encoding/base32"
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
	defaultPairingTTL             = 10 * time.Minute
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
	if deps.Generator == nil {
		deps.Generator = cryptoGenerator{}
	}
	if deps.AuditSink == nil {
		deps.AuditSink = noopAuditSink{}
	}
	if deps.PairingTTL <= 0 {
		deps.PairingTTL = defaultPairingTTL
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

func (s *Service) Start(ctx context.Context, input StartInput) (*StartResult, error) {
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "pairing state store is not configured"}
	}

	now := s.now()
	pairingID, err := s.deps.Generator.NewPairingID(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate pairing id", Cause: err}
	}
	pairingSecret, err := s.deps.Generator.NewPairingSecret(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate pairing secret", Cause: err}
	}
	userCode, err := s.deps.Generator.NewUserCode(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate user code", Cause: err}
	}

	record, err := lifecycle.StartPairing(lifecycle.StartInput{
		PairingID:              pairingID,
		PairingSecretHash:      hashOpaqueSecret(pairingSecret),
		UserCode:               userCode,
		QRPayload:              buildQRPayload(pairingID, userCode),
		DeviceName:             input.DeviceName,
		DeviceType:             input.DeviceType,
		RequestedPolicyProfile: input.RequestedPolicyProfile,
		Now:                    now,
		ExpiresAt:              now.Add(s.deps.PairingTTL),
	})
	if err != nil {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "pairing enrollment request is invalid", Cause: err}
	}
	if err := s.deps.StateStore.PutPairing(ctx, &record); err != nil {
		return nil, classifyStoreError("failed to persist pairing enrollment", err)
	}

	s.recordAudit(ctx, AuditEvent{
		Action:    "pairing.start",
		PairingID: record.PairingID,
		Outcome:   "ok",
		At:        now,
	})

	return &StartResult{
		PairingID:     record.PairingID,
		PairingSecret: pairingSecret,
		UserCode:      record.UserCode,
		QRPayload:     record.QRPayload,
		ExpiresAt:     record.ExpiresAt,
	}, nil
}

func (s *Service) Status(ctx context.Context, input StatusInput) (*StatusResult, error) {
	record, err := s.lookupPairing(ctx, input.PairingID)
	if err != nil {
		return nil, err
	}
	record, err = s.persistExpiryIfNeeded(ctx, input.PairingID, record)
	if err != nil {
		return nil, err
	}
	if err := lifecycle.ValidatePairingSecret(record, hashOpaqueSecret(input.PairingSecret)); err != nil {
		s.recordAudit(ctx, AuditEvent{
			Action:    "pairing.status",
			PairingID: input.PairingID,
			Outcome:   "denied",
			Reason:    "secret_mismatch",
			At:        s.now(),
		})
		return nil, &Error{Kind: ErrorForbidden, Message: "pairing secret mismatch", Cause: err}
	}

	s.recordAudit(ctx, AuditEvent{
		Action:    "pairing.status",
		PairingID: record.PairingID,
		Outcome:   "ok",
		Reason:    string(record.Status),
		At:        s.now(),
	})

	return projectStatus(record), nil
}

func (s *Service) Approve(ctx context.Context, input ApproveInput) (*ApproveResult, error) {
	if strings.TrimSpace(input.PairingID) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "pairing id is required"}
	}
	if strings.TrimSpace(input.OwnerID) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "owner id is required"}
	}
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "pairing state store is not configured"}
	}

	now := s.now()
	var outcomeErr error
	record, err := s.deps.StateStore.UpdatePairing(ctx, strings.TrimSpace(input.PairingID), func(current *deviceauthmodel.PairingRecord) error {
		expired, changed, err := lifecycle.ExpireIfElapsed(*current, now)
		if err != nil {
			return err
		}
		if changed {
			*current = expired
			outcomeErr = lifecycle.ErrPairingAlreadyExpired
			return nil
		}

		next, err := lifecycle.ApprovePairing(*current, lifecycle.ApproveInput{
			OwnerID:               input.OwnerID,
			ApprovedPolicyProfile: input.ApprovedPolicyProfile,
			ApprovedAt:            now,
		}, now)
		if err != nil {
			outcomeErr = err
			return nil
		}
		*current = next
		return nil
	})
	if err != nil {
		return nil, classifyStoreError("failed to update pairing approval", err)
	}
	if outcomeErr != nil {
		s.recordAudit(ctx, AuditEvent{
			Action:    "pairing.approve",
			PairingID: input.PairingID,
			OwnerID:   input.OwnerID,
			Outcome:   "rejected",
			Reason:    classifyReason(outcomeErr),
			At:        now,
		})
		return nil, classifyLifecycleError("pairing approval rejected", outcomeErr)
	}

	s.recordAudit(ctx, AuditEvent{
		Action:    "pairing.approve",
		PairingID: record.PairingID,
		OwnerID:   record.OwnerID,
		Outcome:   "ok",
		Reason:    "approved",
		At:        now,
	})

	return &ApproveResult{
		PairingID:             record.PairingID,
		Status:                record.Status,
		OwnerID:               record.OwnerID,
		ApprovedPolicyProfile: record.ApprovedPolicyProfile,
		ApprovedAt:            cloneTime(record.ApprovedAt),
		ExpiresAt:             record.ExpiresAt,
	}, nil
}

func (s *Service) Exchange(ctx context.Context, input ExchangeInput) (*ExchangeResult, error) {
	if strings.TrimSpace(input.PairingID) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "pairing id is required"}
	}
	if strings.TrimSpace(input.PairingSecret) == "" {
		return nil, &Error{Kind: ErrorInvalidInput, Message: "pairing secret is required"}
	}
	if s.deps.StateStore == nil {
		return nil, &Error{Kind: ErrorStore, Message: "pairing state store is not configured"}
	}

	now := s.now()
	pairingSecretHash := hashOpaqueSecret(input.PairingSecret)
	var outcomeErr error
	record, err := s.deps.StateStore.UpdatePairing(ctx, strings.TrimSpace(input.PairingID), func(current *deviceauthmodel.PairingRecord) error {
		expired, changed, err := lifecycle.ExpireIfElapsed(*current, now)
		if err != nil {
			return err
		}
		if changed {
			*current = expired
			outcomeErr = lifecycle.ErrPairingAlreadyExpired
			return nil
		}

		next, err := lifecycle.ConsumePairing(*current, lifecycle.ConsumeInput{
			PairingSecretHash: pairingSecretHash,
			ConsumedAt:        now,
		}, now)
		if err != nil {
			outcomeErr = err
			return nil
		}
		*current = next
		return nil
	})
	if err != nil {
		return nil, classifyStoreError("failed to consume pairing exchange", err)
	}
	if outcomeErr != nil {
		s.recordAudit(ctx, AuditEvent{
			Action:    "pairing.exchange",
			PairingID: input.PairingID,
			Outcome:   "rejected",
			Reason:    classifyReason(outcomeErr),
			At:        now,
		})
		return nil, classifyLifecycleError("pairing exchange rejected", outcomeErr)
	}

	deviceID, err := s.deps.Generator.NewDeviceID(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate device id", Cause: err}
	}
	deviceGrantID, err := s.deps.Generator.NewDeviceGrantID(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate device grant id", Cause: err}
	}
	deviceGrant, err := s.deps.Generator.NewDeviceGrantSecret(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate device grant", Cause: err}
	}
	accessSessionID, err := s.deps.Generator.NewAccessSessionID(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate access session id", Cause: err}
	}
	accessToken, err := s.deps.Generator.NewAccessToken(ctx)
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to generate access token", Cause: err}
	}

	deviceRecord, err := deviceauthmodel.PrepareDeviceRecord(deviceauthmodel.DeviceRecord{
		DeviceID:      deviceID,
		OwnerID:       record.OwnerID,
		DeviceName:    record.DeviceName,
		DeviceType:    record.DeviceType,
		PolicyProfile: firstNonEmpty(record.ApprovedPolicyProfile, record.RequestedPolicyProfile),
		CreatedAt:     now,
	})
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to build device record", Cause: err}
	}
	rotateAfter := now.Add(s.deps.DeviceGrantRotateAfter)
	deviceGrantRecord, err := deviceauthmodel.PrepareDeviceGrantRecord(deviceauthmodel.DeviceGrantRecord{
		GrantID:     deviceGrantID,
		DeviceID:    deviceID,
		GrantHash:   hashOpaqueSecret(deviceGrant),
		IssuedAt:    now,
		ExpiresAt:   now.Add(s.deps.DeviceGrantTTL),
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to build device grant record", Cause: err}
	}
	accessSessionRecord, err := deviceauthmodel.PrepareAccessSessionRecord(deviceauthmodel.AccessSessionRecord{
		SessionID:     accessSessionID,
		SubjectID:     record.OwnerID,
		DeviceID:      deviceID,
		TokenHash:     hashOpaqueSecret(accessToken),
		PolicyVersion: s.deps.PolicyVersion,
		Scopes:        append([]string(nil), s.deps.DefaultScopes...),
		AuthStrength:  s.deps.AuthStrength,
		IssuedAt:      now,
		ExpiresAt:     now.Add(s.deps.AccessSessionTTL),
	})
	if err != nil {
		return nil, &Error{Kind: ErrorInternal, Message: "failed to build access session record", Cause: err}
	}

	if err := s.deps.StateStore.PutDevice(ctx, &deviceRecord); err != nil {
		return nil, classifyStoreError("failed to persist device record", err)
	}
	if err := s.deps.StateStore.PutDeviceGrant(ctx, &deviceGrantRecord); err != nil {
		return nil, classifyStoreError("failed to persist device grant", err)
	}
	if err := s.deps.StateStore.PutAccessSession(ctx, &accessSessionRecord); err != nil {
		return nil, classifyStoreError("failed to persist access session", err)
	}

	s.recordAudit(ctx, AuditEvent{
		Action:    "pairing.exchange",
		PairingID: record.PairingID,
		DeviceID:  deviceRecord.DeviceID,
		GrantID:   deviceGrantRecord.GrantID,
		SessionID: accessSessionRecord.SessionID,
		OwnerID:   record.OwnerID,
		Outcome:   "ok",
		Reason:    "consumed",
		At:        now,
	})

	endpoints, err := s.publishedEndpoints(ctx)
	if err != nil {
		return nil, err
	}

	return &ExchangeResult{
		PairingID:            record.PairingID,
		DeviceID:             deviceRecord.DeviceID,
		DeviceGrantID:        deviceGrantRecord.GrantID,
		DeviceGrant:          deviceGrant,
		DeviceGrantExpiresAt: deviceGrantRecord.ExpiresAt,
		AccessSessionID:      accessSessionRecord.SessionID,
		AccessToken:          accessToken,
		AccessTokenExpiresAt: accessSessionRecord.ExpiresAt,
		PolicyVersion:        accessSessionRecord.PolicyVersion,
		Scopes:               append([]string(nil), accessSessionRecord.Scopes...),
		Endpoints:            endpoints,
	}, nil
}

func (s *Service) lookupPairing(ctx context.Context, pairingID string) (deviceauthmodel.PairingRecord, error) {
	if strings.TrimSpace(pairingID) == "" {
		return deviceauthmodel.PairingRecord{}, &Error{Kind: ErrorInvalidInput, Message: "pairing id is required"}
	}
	if s.deps.StateStore == nil {
		return deviceauthmodel.PairingRecord{}, &Error{Kind: ErrorStore, Message: "pairing state store is not configured"}
	}
	record, err := s.deps.StateStore.GetPairing(ctx, strings.TrimSpace(pairingID))
	if err != nil {
		return deviceauthmodel.PairingRecord{}, classifyStoreError("failed to read pairing state", err)
	}
	if record == nil {
		return deviceauthmodel.PairingRecord{}, &Error{Kind: ErrorNotFound, Message: "pairing enrollment not found"}
	}
	return *record, nil
}

func (s *Service) persistExpiryIfNeeded(ctx context.Context, pairingID string, record deviceauthmodel.PairingRecord) (deviceauthmodel.PairingRecord, error) {
	now := s.now()
	expired, changed, err := lifecycle.ExpireIfElapsed(record, now)
	if err != nil {
		return deviceauthmodel.PairingRecord{}, &Error{Kind: ErrorInternal, Message: "failed to evaluate pairing expiry", Cause: err}
	}
	if !changed {
		return record, nil
	}
	updated, err := s.deps.StateStore.UpdatePairing(ctx, pairingID, func(current *deviceauthmodel.PairingRecord) error {
		*current = expired
		return nil
	})
	if err != nil {
		return deviceauthmodel.PairingRecord{}, classifyStoreError("failed to persist pairing expiry", err)
	}
	s.recordAudit(ctx, AuditEvent{
		Action:    "pairing.expire",
		PairingID: pairingID,
		OwnerID:   expired.OwnerID,
		Outcome:   "ok",
		Reason:    "expired",
		At:        now,
	})
	return *updated, nil
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

func projectStatus(record deviceauthmodel.PairingRecord) *StatusResult {
	return &StatusResult{
		PairingID:              record.PairingID,
		Status:                 record.Status,
		UserCode:               record.UserCode,
		DeviceName:             record.DeviceName,
		DeviceType:             record.DeviceType,
		RequestedPolicyProfile: record.RequestedPolicyProfile,
		ApprovedPolicyProfile:  record.ApprovedPolicyProfile,
		ExpiresAt:              record.ExpiresAt,
		ApprovedAt:             cloneTime(record.ApprovedAt),
		ConsumedAt:             cloneTime(record.ConsumedAt),
	}
}

func classifyStoreError(message string, err error) error {
	switch {
	case errors.Is(err, deviceauthstore.ErrNotFound):
		return &Error{Kind: ErrorNotFound, Message: "pairing enrollment not found", Cause: err}
	case errors.Is(err, deviceauthstore.ErrConflict):
		return &Error{Kind: ErrorConflict, Message: "pairing state conflict", Cause: err}
	default:
		return &Error{Kind: ErrorStore, Message: message, Cause: err}
	}
}

func classifyLifecycleError(message string, err error) error {
	switch {
	case errors.Is(err, lifecycle.ErrPairingSecretMismatch):
		return &Error{Kind: ErrorForbidden, Message: "pairing secret mismatch", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingAlreadyExpired):
		return &Error{Kind: ErrorExpired, Message: "pairing has expired", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingConsumedBeforeUse), errors.Is(err, lifecycle.ErrPairingAlreadyConsumed):
		return &Error{Kind: ErrorConsumed, Message: "pairing has already been exchanged", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingAlreadyRevoked):
		return &Error{Kind: ErrorRevoked, Message: "pairing has been revoked", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingNotApproved):
		return &Error{Kind: ErrorPending, Message: "pairing is still pending approval", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingAlreadyApproved):
		return &Error{Kind: ErrorConflict, Message: "pairing is already approved", Cause: err}
	case errors.Is(err, lifecycle.ErrPairingNotPending):
		return &Error{Kind: ErrorConflict, Message: message, Cause: err}
	case errors.Is(err, lifecycle.ErrPairingOwnerRequired):
		return &Error{Kind: ErrorInvalidInput, Message: "owner id is required", Cause: err}
	default:
		return &Error{Kind: ErrorInternal, Message: message, Cause: err}
	}
}

func classifyReason(err error) string {
	switch {
	case errors.Is(err, lifecycle.ErrPairingSecretMismatch):
		return "secret_mismatch"
	case errors.Is(err, lifecycle.ErrPairingAlreadyExpired):
		return "expired"
	case errors.Is(err, lifecycle.ErrPairingAlreadyConsumed), errors.Is(err, lifecycle.ErrPairingConsumedBeforeUse):
		return "consumed"
	case errors.Is(err, lifecycle.ErrPairingAlreadyRevoked):
		return "revoked"
	case errors.Is(err, lifecycle.ErrPairingAlreadyApproved):
		return "already_approved"
	case errors.Is(err, lifecycle.ErrPairingNotApproved):
		return "pending"
	case errors.Is(err, lifecycle.ErrPairingOwnerRequired):
		return "owner_required"
	default:
		return "unknown"
	}
}

func hashOpaqueSecret(secret string) string {
	return deviceauthmodel.HashOpaqueSecret(secret)
}

func buildQRPayload(pairingID, userCode string) string {
	return fmt.Sprintf("xg2g://pair?pairing_id=%s&user_code=%s", pairingID, userCode)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

type noopAuditSink struct{}

func (noopAuditSink) Record(context.Context, AuditEvent) error { return nil }

type cryptoGenerator struct{}

func (cryptoGenerator) NewPairingID(context.Context) (string, error) {
	return opaqueToken("pair", 12)
}

func (cryptoGenerator) NewPairingSecret(context.Context) (string, error) {
	return opaqueSecret(24)
}

func (cryptoGenerator) NewUserCode(context.Context) (string, error) {
	alphabet := []byte("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")
	bytes, err := randomBytes(8)
	if err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i, b := range bytes {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out[:4]) + "-" + string(out[4:]), nil
}

func (cryptoGenerator) NewDeviceID(context.Context) (string, error) {
	return opaqueToken("dev", 12)
}

func (cryptoGenerator) NewDeviceGrantID(context.Context) (string, error) {
	return opaqueToken("grant", 12)
}

func (cryptoGenerator) NewDeviceGrantSecret(context.Context) (string, error) {
	return opaqueSecret(32)
}

func (cryptoGenerator) NewAccessSessionID(context.Context) (string, error) {
	return opaqueToken("sess", 12)
}

func (cryptoGenerator) NewAccessToken(context.Context) (string, error) {
	return opaqueSecret(32)
}

func opaqueToken(prefix string, size int) (string, error) {
	bytes, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func opaqueSecret(size int) (string, error) {
	bytes, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(bytes), "="), nil
}

func randomBytes(size int) ([]byte, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}
