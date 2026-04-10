// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package lifecycle

import (
	"errors"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

var (
	ErrPairingOwnerRequired     = errors.New("pairing owner id is required")
	ErrPairingSecretMismatch    = errors.New("pairing secret does not match")
	ErrPairingNotPending        = errors.New("pairing is not pending")
	ErrPairingNotApproved       = errors.New("pairing is not approved")
	ErrPairingAlreadyApproved   = errors.New("pairing is already approved")
	ErrPairingAlreadyExpired    = errors.New("pairing is expired")
	ErrPairingAlreadyConsumed   = errors.New("pairing is already consumed")
	ErrPairingAlreadyRevoked    = errors.New("pairing is revoked")
	ErrPairingConsumedBeforeUse = errors.New("pairing has already been exchanged")
)

type StartInput struct {
	PairingID              string
	PairingSecretHash      string
	UserCode               string
	QRPayload              string
	DeviceName             string
	DeviceType             model.DeviceType
	RequestedPolicyProfile string
	Now                    time.Time
	ExpiresAt              time.Time
}

type ApproveInput struct {
	OwnerID               string
	ApprovedPolicyProfile string
	ApprovedAt            time.Time
}

type ConsumeInput struct {
	PairingSecretHash string
	ConsumedAt        time.Time
}

func StartPairing(input StartInput) (model.PairingRecord, error) {
	return model.PreparePairingRecord(model.PairingRecord{
		PairingID:              input.PairingID,
		PairingSecretHash:      strings.TrimSpace(input.PairingSecretHash),
		UserCode:               input.UserCode,
		QRPayload:              input.QRPayload,
		DeviceName:             input.DeviceName,
		DeviceType:             input.DeviceType,
		RequestedPolicyProfile: input.RequestedPolicyProfile,
		Status:                 model.PairingPending,
		CreatedAt:              input.Now.UTC(),
		ExpiresAt:              input.ExpiresAt.UTC(),
	})
}

func ExpireIfElapsed(record model.PairingRecord, now time.Time) (model.PairingRecord, bool, error) {
	record, err := model.PreparePairingRecord(record)
	if err != nil {
		return model.PairingRecord{}, false, err
	}
	if record.Status.IsTerminal() || !record.IsExpired(now) {
		return record, false, nil
	}

	record.Status = model.PairingExpired
	record.ConsumedAt = nil
	record.RevokedAt = nil
	record.ApprovedAt = nilIfNil(record.ApprovedAt)

	prepared, err := model.PreparePairingRecord(record)
	if err != nil {
		return model.PairingRecord{}, false, err
	}
	return prepared, true, nil
}

func ApprovePairing(record model.PairingRecord, input ApproveInput, now time.Time) (model.PairingRecord, error) {
	record, err := model.PreparePairingRecord(record)
	if err != nil {
		return model.PairingRecord{}, err
	}
	if strings.TrimSpace(input.OwnerID) == "" {
		return model.PairingRecord{}, ErrPairingOwnerRequired
	}
	if record.IsExpired(now) {
		return model.PairingRecord{}, ErrPairingAlreadyExpired
	}
	switch record.Status {
	case model.PairingPending:
	case model.PairingApproved:
		return model.PairingRecord{}, ErrPairingAlreadyApproved
	case model.PairingConsumed:
		return model.PairingRecord{}, ErrPairingAlreadyConsumed
	case model.PairingRevoked:
		return model.PairingRecord{}, ErrPairingAlreadyRevoked
	case model.PairingExpired:
		return model.PairingRecord{}, ErrPairingAlreadyExpired
	default:
		return model.PairingRecord{}, ErrPairingNotPending
	}

	record.OwnerID = strings.TrimSpace(input.OwnerID)
	record.ApprovedPolicyProfile = strings.TrimSpace(input.ApprovedPolicyProfile)
	record.Status = model.PairingApproved
	record.ApprovedAt = utcTimePtr(input.ApprovedAt)
	record.ConsumedAt = nil
	record.RevokedAt = nil

	return model.PreparePairingRecord(record)
}

func ConsumePairing(record model.PairingRecord, input ConsumeInput, now time.Time) (model.PairingRecord, error) {
	record, err := model.PreparePairingRecord(record)
	if err != nil {
		return model.PairingRecord{}, err
	}
	if strings.TrimSpace(input.PairingSecretHash) == "" || record.PairingSecretHash != strings.TrimSpace(input.PairingSecretHash) {
		return model.PairingRecord{}, ErrPairingSecretMismatch
	}
	if record.IsExpired(now) {
		return model.PairingRecord{}, ErrPairingAlreadyExpired
	}
	switch record.Status {
	case model.PairingApproved:
	case model.PairingPending:
		return model.PairingRecord{}, ErrPairingNotApproved
	case model.PairingConsumed:
		return model.PairingRecord{}, ErrPairingConsumedBeforeUse
	case model.PairingRevoked:
		return model.PairingRecord{}, ErrPairingAlreadyRevoked
	case model.PairingExpired:
		return model.PairingRecord{}, ErrPairingAlreadyExpired
	default:
		return model.PairingRecord{}, ErrPairingNotApproved
	}

	record.Status = model.PairingConsumed
	record.ConsumedAt = utcTimePtr(input.ConsumedAt)
	record.RevokedAt = nil

	return model.PreparePairingRecord(record)
}

func ValidatePairingSecret(record model.PairingRecord, pairingSecretHash string) error {
	record, err := model.PreparePairingRecord(record)
	if err != nil {
		return err
	}
	if strings.TrimSpace(pairingSecretHash) == "" || record.PairingSecretHash != strings.TrimSpace(pairingSecretHash) {
		return ErrPairingSecretMismatch
	}
	return nil
}

func ClassifyPairingState(record model.PairingRecord, now time.Time) error {
	record, err := model.PreparePairingRecord(record)
	if err != nil {
		return err
	}
	if record.IsExpired(now) && !record.Status.IsTerminal() {
		return ErrPairingAlreadyExpired
	}
	switch record.Status {
	case model.PairingPending:
		return ErrPairingNotApproved
	case model.PairingApproved:
		return nil
	case model.PairingConsumed:
		return ErrPairingAlreadyConsumed
	case model.PairingRevoked:
		return ErrPairingAlreadyRevoked
	case model.PairingExpired:
		return ErrPairingAlreadyExpired
	default:
		return ErrPairingNotApproved
	}
}

func utcTimePtr(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}

func nilIfNil(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return utcTimePtr(*value)
}
