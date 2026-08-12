// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package webauthn

import (
	"crypto"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrAuthDataTooShort       = errors.New("authenticator data is too short")
	ErrInvalidBackupState     = errors.New("invalid backup state: backup state cannot be true when backup eligible is false")
	ErrAttestedDataMissing    = errors.New("attested credential data flag set but data truncated")
	ErrAttestedDataUnexpected = errors.New("attested credential data present in assertion")
)

const (
	flagUserPresent        byte = 0x01 // UP
	flagUserVerified       byte = 0x04 // UV
	flagBackupEligible     byte = 0x08 // BE
	flagBackupState        byte = 0x10 // BS
	flagAttestedCredential byte = 0x40 // AT
	flagExtensionData      byte = 0x80 // ED
)

// AuthenticatorData represents parsed WebAuthn authData.
type AuthenticatorData struct {
	RPIDHash       [32]byte
	Flags          byte
	UserPresent    bool
	UserVerified   bool
	BackupEligible bool
	BackupState    bool
	SignCount      uint32

	// Attested Credential Data (if AT flag set)
	AAGUID       string
	CredentialID []byte
	PublicKey    crypto.PublicKey
	PublicKeyDER []byte

	RawBytes []byte
}

// ParseAuthenticatorData parses raw authData bytes and enforces WebAuthn L3 invariants.
func ParseAuthenticatorData(data []byte, isRegistration bool) (*AuthenticatorData, error) {
	if len(data) < 37 {
		return nil, ErrAuthDataTooShort
	}

	ad := &AuthenticatorData{
		RawBytes: data,
	}
	copy(ad.RPIDHash[:], data[:32])
	ad.Flags = data[32]
	ad.SignCount = binary.BigEndian.Uint32(data[33:37])

	ad.UserPresent = (ad.Flags & flagUserPresent) != 0
	ad.UserVerified = (ad.Flags & flagUserVerified) != 0
	ad.BackupEligible = (ad.Flags & flagBackupEligible) != 0
	ad.BackupState = (ad.Flags & flagBackupState) != 0

	// WebAuthn Invariant: BS=1 is invalid when BE=0
	if ad.BackupState && !ad.BackupEligible {
		return nil, ErrInvalidBackupState
	}

	hasAT := (ad.Flags & flagAttestedCredential) != 0
	if isRegistration && !hasAT {
		return nil, fmt.Errorf("%w: registration requires AT flag", ErrAttestedDataMissing)
	}
	if !isRegistration && hasAT {
		return nil, ErrAttestedDataUnexpected
	}

	if hasAT {
		offset := 37
		if len(data) < offset+16+2 {
			return nil, ErrAttestedDataMissing
		}

		aaguidBytes := data[offset : offset+16]
		ad.AAGUID = hex.EncodeToString(aaguidBytes)
		offset += 16

		credIDLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if len(data) < offset+credIDLen {
			return nil, ErrAttestedDataMissing
		}

		ad.CredentialID = make([]byte, credIDLen)
		copy(ad.CredentialID, data[offset:offset+credIDLen])
		offset += credIDLen

		coseData := data[offset:]
		coseObj, err := DecodeCBOR(coseData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode credential COSE key: %w", err)
		}
		coseMap, ok := coseObj.(map[any]any)
		if !ok {
			return nil, errors.New("credential COSE key is not a map")
		}

		pub, spki, err := ParseCOSEPublicKey(coseMap)
		if err != nil {
			return nil, fmt.Errorf("failed to parse credential public key: %w", err)
		}
		ad.PublicKey = pub
		ad.PublicKeyDER = spki
	}

	return ad, nil
}
