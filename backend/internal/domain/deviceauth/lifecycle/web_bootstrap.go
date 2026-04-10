// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package lifecycle

import (
	"errors"
	"time"

	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

var (
	ErrWebBootstrapSecretMismatch  = errors.New("web bootstrap secret mismatch")
	ErrWebBootstrapAlreadyExpired  = errors.New("web bootstrap already expired")
	ErrWebBootstrapAlreadyConsumed = errors.New("web bootstrap already consumed")
	ErrWebBootstrapAlreadyRevoked  = errors.New("web bootstrap already revoked")
)

type StartWebBootstrapInput struct {
	BootstrapID           string
	BootstrapSecretHash   string
	SourceAccessSessionID string
	TargetPath            string
	Now                   time.Time
	ExpiresAt             time.Time
}

type ConsumeWebBootstrapInput struct {
	BootstrapSecretHash string
	ConsumedAt          time.Time
}

func StartWebBootstrap(input StartWebBootstrapInput) (deviceauthmodel.WebBootstrapRecord, error) {
	return deviceauthmodel.PrepareWebBootstrapRecord(deviceauthmodel.WebBootstrapRecord{
		BootstrapID:           input.BootstrapID,
		BootstrapSecretHash:   input.BootstrapSecretHash,
		SourceAccessSessionID: input.SourceAccessSessionID,
		TargetPath:            input.TargetPath,
		CreatedAt:             input.Now.UTC(),
		ExpiresAt:             input.ExpiresAt.UTC(),
	})
}

func ExpireWebBootstrapIfElapsed(record deviceauthmodel.WebBootstrapRecord, now time.Time) (deviceauthmodel.WebBootstrapRecord, bool, error) {
	if record.IsConsumed() || record.IsRevoked() || !record.IsExpired(now) {
		return record, false, nil
	}
	return record, true, nil
}

func ConsumeWebBootstrap(record deviceauthmodel.WebBootstrapRecord, input ConsumeWebBootstrapInput, now time.Time) (deviceauthmodel.WebBootstrapRecord, error) {
	switch {
	case record.IsRevoked():
		return deviceauthmodel.WebBootstrapRecord{}, ErrWebBootstrapAlreadyRevoked
	case record.IsConsumed():
		return deviceauthmodel.WebBootstrapRecord{}, ErrWebBootstrapAlreadyConsumed
	case record.IsExpired(now):
		return deviceauthmodel.WebBootstrapRecord{}, ErrWebBootstrapAlreadyExpired
	case record.BootstrapSecretHash != input.BootstrapSecretHash:
		return deviceauthmodel.WebBootstrapRecord{}, ErrWebBootstrapSecretMismatch
	}

	consumedAt := input.ConsumedAt.UTC()
	record.ConsumedAt = &consumedAt
	return deviceauthmodel.PrepareWebBootstrapRecord(record)
}
