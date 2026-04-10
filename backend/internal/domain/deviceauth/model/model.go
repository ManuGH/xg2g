// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

var (
	ErrInvalidPairingID         = errors.New("pairing id must not be empty")
	ErrInvalidPairingSecretHash = errors.New("pairing secret hash must not be empty")
	ErrInvalidUserCode          = errors.New("pairing user code must not be empty")
	ErrInvalidPairingStatus     = errors.New("pairing status is invalid")
	ErrInvalidPairingCreatedAt  = errors.New("pairing created_at must not be zero")
	ErrInvalidPairingExpiresAt  = errors.New("pairing expires_at must be after created_at")
	ErrInvalidPairingApprovedAt = errors.New("pairing approved_at must not be before created_at")
	ErrPairingApprovalRequired  = errors.New("approved or consumed pairing requires owner and approved_at")
	ErrPairingConsumedAt        = errors.New("consumed pairing requires consumed_at after approved_at")
	ErrPairingRevokedAt         = errors.New("revoked pairing requires revoked_at on or after created_at")

	ErrInvalidDeviceID         = errors.New("device id must not be empty")
	ErrInvalidDeviceOwnerID    = errors.New("device owner id must not be empty")
	ErrInvalidDeviceCreatedAt  = errors.New("device created_at must not be zero")
	ErrInvalidDeviceLastSeenAt = errors.New("device last_seen_at must not be before created_at")
	ErrInvalidDeviceRevokedAt  = errors.New("device revoked_at must not be before created_at")

	ErrInvalidGrantID        = errors.New("device grant id must not be empty")
	ErrInvalidGrantHash      = errors.New("device grant hash must not be empty")
	ErrInvalidGrantIssuedAt  = errors.New("device grant issued_at must not be zero")
	ErrInvalidGrantExpiresAt = errors.New("device grant expires_at must be after issued_at")
	ErrInvalidGrantRotation  = errors.New("device grant rotate_after must be between issued_at and expires_at")
	ErrInvalidGrantLastUsed  = errors.New("device grant last_used_at must not be before issued_at")
	ErrInvalidGrantRevokedAt = errors.New("device grant revoked_at must not be before issued_at")

	ErrInvalidSessionID        = errors.New("access session id must not be empty")
	ErrInvalidSessionSubjectID = errors.New("access session subject id must not be empty")
	ErrInvalidSessionTokenHash = errors.New("access session token hash must not be empty")
	ErrInvalidSessionIssuedAt  = errors.New("access session issued_at must not be zero")
	ErrInvalidSessionExpiresAt = errors.New("access session expires_at must be after issued_at")
	ErrInvalidSessionRevokedAt = errors.New("access session revoked_at must not be before issued_at")

	ErrInvalidWebBootstrapID            = errors.New("web bootstrap id must not be empty")
	ErrInvalidWebBootstrapSecretHash    = errors.New("web bootstrap secret hash must not be empty")
	ErrInvalidWebBootstrapSourceSession = errors.New("web bootstrap source session id must not be empty")
	ErrInvalidWebBootstrapTargetPath    = errors.New("web bootstrap target path must be an absolute same-origin path")
	ErrInvalidWebBootstrapCreatedAt     = errors.New("web bootstrap created_at must not be zero")
	ErrInvalidWebBootstrapExpiresAt     = errors.New("web bootstrap expires_at must be after created_at")
	ErrInvalidWebBootstrapConsumedAt    = errors.New("web bootstrap consumed_at must not be before created_at")
	ErrInvalidWebBootstrapRevokedAt     = errors.New("web bootstrap revoked_at must not be before created_at")
)

// PairingRecord is the server-side enrollment truth for one pending device.
// The stored secret binding is the hashed server copy, not the raw device-held secret.
type PairingRecord struct {
	PairingID              string
	PairingSecretHash      string
	UserCode               string
	QRPayload              string
	DeviceName             string
	DeviceType             DeviceType
	RequestedPolicyProfile string
	ApprovedPolicyProfile  string
	OwnerID                string
	Status                 PairingStatus
	CreatedAt              time.Time
	ExpiresAt              time.Time
	ApprovedAt             *time.Time
	ConsumedAt             *time.Time
	RevokedAt              *time.Time
}

// DeviceRecord is the canonical server identity for one enrolled client.
type DeviceRecord struct {
	DeviceID      string
	OwnerID       string
	DeviceName    string
	DeviceType    DeviceType
	PolicyProfile string
	Capabilities  map[string]any
	CreatedAt     time.Time
	LastSeenAt    *time.Time
	RevokedAt     *time.Time
}

// DeviceGrantRecord is the revocable long-lived credential truth for one device.
// The stored grant binding is the hashed server copy, not the raw client credential.
type DeviceGrantRecord struct {
	GrantID     string
	DeviceID    string
	GrantHash   string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	RotateAfter *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// AccessSessionRecord is the short-lived effective authorization context.
// Native/web token shapes may differ, but they derive from this same session truth.
type AccessSessionRecord struct {
	SessionID     string
	SubjectID     string
	DeviceID      string
	TokenHash     string
	PolicyVersion string
	Scopes        []string
	AuthStrength  string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	RevokedAt     *time.Time
}

// WebBootstrapRecord is a short-lived one-time grant used to convert a native
// device access session into an HttpOnly browser cookie session without
// exposing the device access token to browser JavaScript.
type WebBootstrapRecord struct {
	BootstrapID           string
	BootstrapSecretHash   string
	SourceAccessSessionID string
	TargetPath            string
	CreatedAt             time.Time
	ExpiresAt             time.Time
	ConsumedAt            *time.Time
	RevokedAt             *time.Time
}

func PreparePairingRecord(record PairingRecord) (PairingRecord, error) {
	normalized := PairingRecord{
		PairingID:              trimOpaqueID(record.PairingID),
		PairingSecretHash:      strings.TrimSpace(record.PairingSecretHash),
		UserCode:               normalizeUserCode(record.UserCode),
		QRPayload:              strings.TrimSpace(record.QRPayload),
		DeviceName:             normalizeDeviceName(record.DeviceName, NormalizeDeviceType(record.DeviceType)),
		DeviceType:             NormalizeDeviceType(record.DeviceType),
		RequestedPolicyProfile: strings.TrimSpace(record.RequestedPolicyProfile),
		ApprovedPolicyProfile:  strings.TrimSpace(record.ApprovedPolicyProfile),
		OwnerID:                trimOpaqueID(record.OwnerID),
		Status:                 NormalizePairingStatus(record.Status),
		CreatedAt:              record.CreatedAt.UTC(),
		ExpiresAt:              record.ExpiresAt.UTC(),
		ApprovedAt:             cloneTimePtr(record.ApprovedAt),
		ConsumedAt:             cloneTimePtr(record.ConsumedAt),
		RevokedAt:              cloneTimePtr(record.RevokedAt),
	}

	if normalized.PairingID == "" {
		return PairingRecord{}, ErrInvalidPairingID
	}
	if normalized.PairingSecretHash == "" {
		return PairingRecord{}, ErrInvalidPairingSecretHash
	}
	if normalized.UserCode == "" {
		return PairingRecord{}, ErrInvalidUserCode
	}
	if normalized.CreatedAt.IsZero() {
		return PairingRecord{}, ErrInvalidPairingCreatedAt
	}
	if !normalized.ExpiresAt.After(normalized.CreatedAt) {
		return PairingRecord{}, ErrInvalidPairingExpiresAt
	}
	switch normalized.Status {
	case PairingPending, PairingApproved, PairingExpired, PairingConsumed, PairingRevoked:
	default:
		return PairingRecord{}, ErrInvalidPairingStatus
	}

	if normalized.ApprovedAt != nil {
		normalized.ApprovedAt = utcTimePtr(normalized.ApprovedAt)
		if normalized.ApprovedAt.Before(normalized.CreatedAt) {
			return PairingRecord{}, ErrInvalidPairingApprovedAt
		}
	}
	if normalized.ConsumedAt != nil {
		normalized.ConsumedAt = utcTimePtr(normalized.ConsumedAt)
	}
	if normalized.RevokedAt != nil {
		normalized.RevokedAt = utcTimePtr(normalized.RevokedAt)
	}

	switch normalized.Status {
	case PairingApproved:
		if normalized.OwnerID == "" || normalized.ApprovedAt == nil {
			return PairingRecord{}, ErrPairingApprovalRequired
		}
	case PairingConsumed:
		if normalized.OwnerID == "" || normalized.ApprovedAt == nil {
			return PairingRecord{}, ErrPairingApprovalRequired
		}
		if normalized.ConsumedAt == nil || normalized.ConsumedAt.Before(*normalized.ApprovedAt) {
			return PairingRecord{}, ErrPairingConsumedAt
		}
	case PairingRevoked:
		if normalized.RevokedAt == nil || normalized.RevokedAt.Before(normalized.CreatedAt) {
			return PairingRecord{}, ErrPairingRevokedAt
		}
	}

	return normalized, nil
}

func PrepareDeviceRecord(record DeviceRecord) (DeviceRecord, error) {
	normalizedType := NormalizeDeviceType(record.DeviceType)
	normalized := DeviceRecord{
		DeviceID:      trimOpaqueID(record.DeviceID),
		OwnerID:       trimOpaqueID(record.OwnerID),
		DeviceName:    normalizeDeviceName(record.DeviceName, normalizedType),
		DeviceType:    normalizedType,
		PolicyProfile: strings.TrimSpace(record.PolicyProfile),
		Capabilities:  cloneCapabilities(record.Capabilities),
		CreatedAt:     record.CreatedAt.UTC(),
		LastSeenAt:    cloneTimePtr(record.LastSeenAt),
		RevokedAt:     cloneTimePtr(record.RevokedAt),
	}

	if normalized.DeviceID == "" {
		return DeviceRecord{}, ErrInvalidDeviceID
	}
	if normalized.OwnerID == "" {
		return DeviceRecord{}, ErrInvalidDeviceOwnerID
	}
	if normalized.CreatedAt.IsZero() {
		return DeviceRecord{}, ErrInvalidDeviceCreatedAt
	}
	if normalized.LastSeenAt != nil {
		normalized.LastSeenAt = utcTimePtr(normalized.LastSeenAt)
		if normalized.LastSeenAt.Before(normalized.CreatedAt) {
			return DeviceRecord{}, ErrInvalidDeviceLastSeenAt
		}
	}
	if normalized.RevokedAt != nil {
		normalized.RevokedAt = utcTimePtr(normalized.RevokedAt)
		if normalized.RevokedAt.Before(normalized.CreatedAt) {
			return DeviceRecord{}, ErrInvalidDeviceRevokedAt
		}
	}
	return normalized, nil
}

func PrepareDeviceGrantRecord(record DeviceGrantRecord) (DeviceGrantRecord, error) {
	normalized := DeviceGrantRecord{
		GrantID:     trimOpaqueID(record.GrantID),
		DeviceID:    trimOpaqueID(record.DeviceID),
		GrantHash:   strings.TrimSpace(record.GrantHash),
		IssuedAt:    record.IssuedAt.UTC(),
		ExpiresAt:   record.ExpiresAt.UTC(),
		RotateAfter: cloneTimePtr(record.RotateAfter),
		LastUsedAt:  cloneTimePtr(record.LastUsedAt),
		RevokedAt:   cloneTimePtr(record.RevokedAt),
	}

	if normalized.GrantID == "" {
		return DeviceGrantRecord{}, ErrInvalidGrantID
	}
	if normalized.DeviceID == "" {
		return DeviceGrantRecord{}, ErrInvalidDeviceID
	}
	if normalized.GrantHash == "" {
		return DeviceGrantRecord{}, ErrInvalidGrantHash
	}
	if normalized.IssuedAt.IsZero() {
		return DeviceGrantRecord{}, ErrInvalidGrantIssuedAt
	}
	if !normalized.ExpiresAt.After(normalized.IssuedAt) {
		return DeviceGrantRecord{}, ErrInvalidGrantExpiresAt
	}
	if normalized.RotateAfter != nil {
		normalized.RotateAfter = utcTimePtr(normalized.RotateAfter)
		if normalized.RotateAfter.Before(normalized.IssuedAt) || !normalized.RotateAfter.Before(normalized.ExpiresAt) {
			return DeviceGrantRecord{}, ErrInvalidGrantRotation
		}
	}
	if normalized.LastUsedAt != nil {
		normalized.LastUsedAt = utcTimePtr(normalized.LastUsedAt)
		if normalized.LastUsedAt.Before(normalized.IssuedAt) {
			return DeviceGrantRecord{}, ErrInvalidGrantLastUsed
		}
	}
	if normalized.RevokedAt != nil {
		normalized.RevokedAt = utcTimePtr(normalized.RevokedAt)
		if normalized.RevokedAt.Before(normalized.IssuedAt) {
			return DeviceGrantRecord{}, ErrInvalidGrantRevokedAt
		}
	}

	return normalized, nil
}

func PrepareAccessSessionRecord(record AccessSessionRecord) (AccessSessionRecord, error) {
	normalized := AccessSessionRecord{
		SessionID:     trimOpaqueID(record.SessionID),
		SubjectID:     trimOpaqueID(record.SubjectID),
		DeviceID:      trimOpaqueID(record.DeviceID),
		TokenHash:     strings.TrimSpace(record.TokenHash),
		PolicyVersion: strings.TrimSpace(record.PolicyVersion),
		Scopes:        normalizeScopes(record.Scopes),
		AuthStrength:  strings.TrimSpace(record.AuthStrength),
		IssuedAt:      record.IssuedAt.UTC(),
		ExpiresAt:     record.ExpiresAt.UTC(),
		RevokedAt:     cloneTimePtr(record.RevokedAt),
	}

	if normalized.SessionID == "" {
		return AccessSessionRecord{}, ErrInvalidSessionID
	}
	if normalized.SubjectID == "" {
		return AccessSessionRecord{}, ErrInvalidSessionSubjectID
	}
	if normalized.DeviceID == "" {
		return AccessSessionRecord{}, ErrInvalidDeviceID
	}
	if normalized.TokenHash == "" {
		return AccessSessionRecord{}, ErrInvalidSessionTokenHash
	}
	if normalized.IssuedAt.IsZero() {
		return AccessSessionRecord{}, ErrInvalidSessionIssuedAt
	}
	if !normalized.ExpiresAt.After(normalized.IssuedAt) {
		return AccessSessionRecord{}, ErrInvalidSessionExpiresAt
	}
	if normalized.RevokedAt != nil {
		normalized.RevokedAt = utcTimePtr(normalized.RevokedAt)
		if normalized.RevokedAt.Before(normalized.IssuedAt) {
			return AccessSessionRecord{}, ErrInvalidSessionRevokedAt
		}
	}

	return normalized, nil
}

func PrepareWebBootstrapRecord(record WebBootstrapRecord) (WebBootstrapRecord, error) {
	targetPath, err := normalizeWebBootstrapTargetPath(record.TargetPath)
	if err != nil {
		return WebBootstrapRecord{}, err
	}

	normalized := WebBootstrapRecord{
		BootstrapID:           trimOpaqueID(record.BootstrapID),
		BootstrapSecretHash:   strings.TrimSpace(record.BootstrapSecretHash),
		SourceAccessSessionID: trimOpaqueID(record.SourceAccessSessionID),
		TargetPath:            targetPath,
		CreatedAt:             record.CreatedAt.UTC(),
		ExpiresAt:             record.ExpiresAt.UTC(),
		ConsumedAt:            cloneTimePtr(record.ConsumedAt),
		RevokedAt:             cloneTimePtr(record.RevokedAt),
	}

	if normalized.BootstrapID == "" {
		return WebBootstrapRecord{}, ErrInvalidWebBootstrapID
	}
	if normalized.BootstrapSecretHash == "" {
		return WebBootstrapRecord{}, ErrInvalidWebBootstrapSecretHash
	}
	if normalized.SourceAccessSessionID == "" {
		return WebBootstrapRecord{}, ErrInvalidWebBootstrapSourceSession
	}
	if normalized.CreatedAt.IsZero() {
		return WebBootstrapRecord{}, ErrInvalidWebBootstrapCreatedAt
	}
	if !normalized.ExpiresAt.After(normalized.CreatedAt) {
		return WebBootstrapRecord{}, ErrInvalidWebBootstrapExpiresAt
	}
	if normalized.ConsumedAt != nil {
		normalized.ConsumedAt = utcTimePtr(normalized.ConsumedAt)
		if normalized.ConsumedAt.Before(normalized.CreatedAt) {
			return WebBootstrapRecord{}, ErrInvalidWebBootstrapConsumedAt
		}
	}
	if normalized.RevokedAt != nil {
		normalized.RevokedAt = utcTimePtr(normalized.RevokedAt)
		if normalized.RevokedAt.Before(normalized.CreatedAt) {
			return WebBootstrapRecord{}, ErrInvalidWebBootstrapRevokedAt
		}
	}

	return normalized, nil
}

func (r PairingRecord) IsExpired(now time.Time) bool {
	if r.ExpiresAt.IsZero() {
		return true
	}
	return !now.UTC().Before(r.ExpiresAt.UTC())
}

func (r PairingRecord) CanApprove(now time.Time) bool {
	return NormalizePairingStatus(r.Status) == PairingPending && !r.IsExpired(now)
}

func (r PairingRecord) CanExchange(now time.Time) bool {
	if NormalizePairingStatus(r.Status) != PairingApproved {
		return false
	}
	if r.IsExpired(now) {
		return false
	}
	return r.RevokedAt == nil && r.ConsumedAt == nil
}

func (r DeviceRecord) IsRevoked() bool {
	return r.RevokedAt != nil
}

func (r DeviceRecord) CanIssueSessions(time.Time) bool {
	return !r.IsRevoked()
}

func (r DeviceGrantRecord) IsRevoked() bool {
	return r.RevokedAt != nil
}

func (r DeviceGrantRecord) IsUsable(now time.Time) bool {
	if r.IsRevoked() {
		return false
	}
	return now.UTC().Before(r.ExpiresAt.UTC())
}

func (r DeviceGrantRecord) NeedsRotation(now time.Time) bool {
	if r.RotateAfter == nil {
		return false
	}
	return !now.UTC().Before(r.RotateAfter.UTC())
}

func (r AccessSessionRecord) IsRevoked() bool {
	return r.RevokedAt != nil
}

func (r AccessSessionRecord) IsActive(now time.Time) bool {
	if r.IsRevoked() {
		return false
	}
	return now.UTC().Before(r.ExpiresAt.UTC())
}

func (r WebBootstrapRecord) IsRevoked() bool {
	return r.RevokedAt != nil
}

func (r WebBootstrapRecord) IsConsumed() bool {
	return r.ConsumedAt != nil
}

func (r WebBootstrapRecord) IsExpired(now time.Time) bool {
	if r.ExpiresAt.IsZero() {
		return true
	}
	return !now.UTC().Before(r.ExpiresAt.UTC())
}

func (r WebBootstrapRecord) CanConsume(now time.Time) bool {
	if r.IsRevoked() || r.IsConsumed() {
		return false
	}
	return !r.IsExpired(now)
}

func trimOpaqueID(value string) string {
	return strings.TrimSpace(value)
}

func normalizeUserCode(value string) string {
	replacer := strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(value)))
}

func normalizeDeviceName(value string, deviceType DeviceType) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}

	switch deviceType {
	case DeviceTypeAndroidPhone:
		return "Android Phone"
	case DeviceTypeAndroidTablet:
		return "Android Tablet"
	case DeviceTypeAndroidTV:
		return "Android TV"
	case DeviceTypeBrowser:
		return "Browser"
	default:
		return "Device"
	}
}

func normalizeScopes(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	slices.Sort(out)
	return out
}

func normalizeWebBootstrapTargetPath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = "/ui/"
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", ErrInvalidWebBootstrapTargetPath
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrInvalidWebBootstrapTargetPath
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return "", ErrInvalidWebBootstrapTargetPath
	}
	if parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "", ErrInvalidWebBootstrapTargetPath
	}

	return trimmed, nil
}

func cloneCapabilities(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		cloned[normalizedKey] = value
	}
	return cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
