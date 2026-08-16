// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"strconv"
	"strings"
)

// MultiplexID represents an immutable, distinct broadcast Transport Stream / Multiplex.
// Multiple television channels and radio stations sharing the exact same frequency / transponder
// share the identical MultiplexID.
type MultiplexID struct {
	DVBType      DVBType  `json:"dvbType"`
	DVBNamespace uint32   `json:"dvbNamespace"` // e.g. 0x00C00000 for Astra 19.2E
	TSID         uint16   `json:"tsid"`         // Transport Stream ID
	ONID         uint16   `json:"onid"`         // Original Network ID
	RFPlane      *RFPlane `json:"rfPlane,omitempty"`
}

// String returns a deterministic, canonical string representation for the Multiplex.
// Ensures consistent map keys and log outputs without padding or casing ambiguity.
func (m MultiplexID) String() string {
	if m.RFPlane != nil {
		return fmt.Sprintf("%s:%08X:%04X:%04X:[%s]", m.DVBType, m.DVBNamespace, m.TSID, m.ONID, m.RFPlane.String())
	}
	return fmt.Sprintf("%s:%08X:%04X:%04X", m.DVBType, m.DVBNamespace, m.TSID, m.ONID)
}

// IsSamePhysicalMultiplex checks if two MultiplexIDs share the same transport stream identity.
func (m MultiplexID) IsSamePhysicalMultiplex(other MultiplexID) bool {
	return m.DVBType == other.DVBType &&
		m.DVBNamespace == other.DVBNamespace &&
		m.TSID == other.TSID &&
		m.ONID == other.ONID
}

// ParseServiceRef extracts the MultiplexID from an Enigma2 Service Reference string.
// Standard E2 format: 1:0:19:283D:3FB:1:C00000:0:0:0:
// fields: [0]=type, [1]=reserved, [2]=service_type, [3]=service_id, [4]=tsid, [5]=onid, [6]=dvb_namespace, [7..]=reserved
func ParseServiceRef(serviceRef string) (MultiplexID, error) {
	s := strings.TrimSpace(serviceRef)
	s = strings.TrimSuffix(s, ":")
	parts := strings.Split(s, ":")
	if len(parts) < 7 {
		return MultiplexID{}, fmt.Errorf("invalid enigma2 service reference format: %q", serviceRef)
	}

	tsid64, err := strconv.ParseUint(parts[4], 16, 16)
	if err != nil {
		return MultiplexID{}, fmt.Errorf("invalid tsid %q in service ref: %w", parts[4], err)
	}

	onid64, err := strconv.ParseUint(parts[5], 16, 16)
	if err != nil {
		return MultiplexID{}, fmt.Errorf("invalid onid %q in service ref: %w", parts[5], err)
	}

	namespace64, err := strconv.ParseUint(parts[6], 16, 32)
	if err != nil {
		return MultiplexID{}, fmt.Errorf("invalid dvb_namespace %q in service ref: %w", parts[6], err)
	}

	dvbType := DetermineDVBType(uint32(namespace64))
	var plane *RFPlane
	if dvbType == DVBTypeSat {
		satPos := SatellitePositionFromNamespace(uint32(namespace64))
		// Default plane placeholder with sat position until frequency details are attached
		plane = &RFPlane{
			SatPosition:  satPos,
			Band:         BandHigh,
			Polarization: PolarizationHorizontal,
		}
	}

	return MultiplexID{
		DVBType:      dvbType,
		DVBNamespace: uint32(namespace64),
		TSID:         uint16(tsid64),
		ONID:         uint16(onid64),
		RFPlane:      plane,
	}, nil
}

// DetermineDVBType derives DVB broadcast type from standard Enigma2 DVB Namespace upper bits.
func DetermineDVBType(namespace uint32) DVBType {
	switch {
	case namespace == 0xFFFF0000 || namespace == 0xFFFF:
		return DVBTypeCable
	case namespace == 0xEEEE0000 || namespace == 0xEEEE:
		return DVBTypeTerrestrial
	default:
		return DVBTypeSat
	}
}

// SatellitePositionFromNamespace extracts the orbital position from an Enigma2 DVB Satellite Namespace.
// In Enigma2: namespace upper 16-bits encode orbital position (e.g. 0x00C0 = 192 = 19.2°E, 0x0082 = 130 = 13.0°E, 0x011A = 282 = 28.2°E).
func SatellitePositionFromNamespace(namespace uint32) SatellitePosition {
	posHex := (namespace >> 16) & 0xFFFF
	if posHex == 0 {
		posHex = namespace & 0xFFFF
	}
	return SatellitePosition(posHex)
}

// BuildSatMultiplexID constructs a satellite MultiplexID with complete RF plane information.
func BuildSatMultiplexID(satPos SatellitePosition, namespace uint32, tsid uint16, onid uint16, band Band, pol Polarization) MultiplexID {
	return MultiplexID{
		DVBType:      DVBTypeSat,
		DVBNamespace: namespace,
		TSID:         tsid,
		ONID:         onid,
		RFPlane: &RFPlane{
			SatPosition:  satPos,
			Band:         band,
			Polarization: pol,
		},
	}
}
