// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MultiplexID represents an immutable, distinct broadcast Transport Stream / Multiplex.
// Multiple television channels and radio stations sharing the exact same frequency / transponder
// share the identical MultiplexID.
type MultiplexID struct {
	DVBType        DVBType         `json:"dvbType"`
	DVBNamespace   uint32          `json:"dvbNamespace"` // e.g. 0x00C00000 for Astra 19.2E
	TSID           uint16          `json:"tsid"`         // Transport Stream ID
	ONID           uint16          `json:"onid"`         // Original Network ID
	RFPlane        *RFPlane        `json:"rfPlane,omitempty"`
	TransponderKey *TransponderKey `json:"transponderKey,omitempty"`
}

// String returns a deterministic, canonical string representation for the Multiplex.
// Ensures consistent map keys and log outputs without padding or casing ambiguity.
func (m MultiplexID) String() string {
	if m.TransponderKey != nil {
		return fmt.Sprintf("%s:%08X:%04X:%04X:[%s]", m.DVBType, m.DVBNamespace, m.TSID, m.ONID, m.TransponderKey.Canonical())
	}
	if m.RFPlane != nil {
		return fmt.Sprintf("%s:%08X:%04X:%04X:[%s]", m.DVBType, m.DVBNamespace, m.TSID, m.ONID, m.RFPlane.String())
	}
	return fmt.Sprintf("%s:%08X:%04X:%04X", m.DVBType, m.DVBNamespace, m.TSID, m.ONID)
}

// IsSamePhysicalMultiplex checks if two MultiplexIDs share the same transport stream identity.
func (m MultiplexID) IsSamePhysicalMultiplex(other MultiplexID) bool {
	if m.TransponderKey != nil && other.TransponderKey != nil {
		return m.TransponderKey.Canonical() == other.TransponderKey.Canonical() &&
			m.TSID == other.TSID &&
			m.ONID == other.ONID
	}
	if m.TransponderKey != nil || other.TransponderKey != nil {
		// Asymmetric transponder definitions: cannot guarantee identical physical RF stream
		return false
	}
	return m.DVBType == other.DVBType &&
		m.DVBNamespace == other.DVBNamespace &&
		m.TSID == other.TSID &&
		m.ONID == other.ONID
}

// ParseServiceRef extracts the MultiplexID from an Enigma2 Service Reference string.
// Standard E2 format: 1:0:19:283D:3FB:1:C00000:0:0:0:
// Optional query params for detailed RF attributes: ?freq=11493000&pol=H&stream_id=1&pls_mode=GOLD&pls_code=12345&plp=0
// fields: [0]=type, [1]=reserved, [2]=service_type, [3]=service_id, [4]=tsid, [5]=onid, [6]=dvb_namespace, [7..]=reserved
func ParseServiceRef(serviceRef string) (MultiplexID, error) {
	s := strings.TrimSpace(serviceRef)

	var queryParams url.Values
	if qIdx := strings.Index(s, "?"); qIdx != -1 {
		rawQuery := s[qIdx+1:]
		s = s[:qIdx]
		if parsedQ, err := url.ParseQuery(rawQuery); err == nil {
			queryParams = parsedQ
		}
	}

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
	var tpKey *TransponderKey

	switch dvbType {
	case DVBTypeSat:
		satPos := SatellitePositionFromNamespace(uint32(namespace64))
		pol := PolarizationHorizontal
		band := BandHigh
		if queryParams != nil {
			if p := strings.ToUpper(queryParams.Get("pol")); p == "V" {
				pol = PolarizationVertical
			}
			if b := strings.ToUpper(queryParams.Get("band")); b == "LOW" {
				band = BandLow
			}
		}
		plane = &RFPlane{
			SatPosition:  satPos,
			Band:         band,
			Polarization: pol,
		}

		streamID := -1
		if queryParams != nil && queryParams.Get("stream_id") != "" {
			if sid, err := strconv.Atoi(queryParams.Get("stream_id")); err == nil {
				streamID = sid
			}
		}

		var plsMode PLSMode
		var plsCode uint32
		if queryParams != nil {
			plsMode = PLSMode(strings.ToUpper(queryParams.Get("pls_mode")))
			if pc, err := strconv.ParseUint(queryParams.Get("pls_code"), 10, 32); err == nil {
				plsCode = uint32(pc)
			}
		}

		var freqHz uint64
		if queryParams != nil && queryParams.Get("freq") != "" {
			if f, err := strconv.ParseUint(queryParams.Get("freq"), 10, 64); err == nil {
				freqHz = f
			}
		}

		delSys := DeliverySystemDVBS2
		if queryParams != nil && queryParams.Get("system") != "" {
			delSys = DeliverySystem(strings.ToUpper(queryParams.Get("system")))
		}

		tpKey = &TransponderKey{
			DeliverySystem:  delSys,
			OrbitalPosition: int(satPos),
			FrequencyHz:     freqHz,
			Polarization:    pol,
			StreamID:        streamID,
			PLSMode:         plsMode,
			PLSCode:         plsCode,
		}

	case DVBTypeCable:
		var freqHz uint64
		if queryParams != nil && queryParams.Get("freq") != "" {
			if f, err := strconv.ParseUint(queryParams.Get("freq"), 10, 64); err == nil {
				freqHz = f
			}
		}
		tpKey = &TransponderKey{
			DeliverySystem: DeliverySystemDVBC,
			FrequencyHz:    freqHz,
			StreamID:       -1,
		}

	case DVBTypeTerrestrial:
		var freqHz uint64
		streamID := -1
		if queryParams != nil {
			if f, err := strconv.ParseUint(queryParams.Get("freq"), 10, 64); err == nil {
				freqHz = f
			}
			if plp, err := strconv.Atoi(queryParams.Get("plp")); err == nil {
				streamID = plp
			} else if sid, err := strconv.Atoi(queryParams.Get("stream_id")); err == nil {
				streamID = sid
			}
		}
		tpKey = &TransponderKey{
			DeliverySystem: DeliverySystemDVBT2,
			FrequencyHz:    freqHz,
			StreamID:       streamID,
		}
	}

	return MultiplexID{
		DVBType:        dvbType,
		DVBNamespace:   uint32(namespace64),
		TSID:           uint16(tsid64),
		ONID:           uint16(onid64),
		RFPlane:        plane,
		TransponderKey: tpKey,
	}, nil
}

// DetermineDVBType derives DVB broadcast type from standard Enigma2 DVB Namespace upper bits.
func DetermineDVBType(namespace uint32) DVBType {
	switch namespace {
	case 0xFFFF0000, 0xFFFF:
		return DVBTypeCable
	case 0xEEEE0000, 0xEEEE:
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
		TransponderKey: &TransponderKey{
			DeliverySystem:  DeliverySystemDVBS2,
			OrbitalPosition: int(satPos),
			Polarization:    pol,
			StreamID:        -1,
		},
	}
}

// BuildMultiplexWithTransponder creates a MultiplexID with an authoritative TransponderKey.
func BuildMultiplexWithTransponder(dvbType DVBType, namespace uint32, tsid uint16, onid uint16, tpKey TransponderKey) MultiplexID {
	var plane *RFPlane
	if dvbType == DVBTypeSat {
		band := BandHigh
		if tpKey.FrequencyHz > 0 && tpKey.FrequencyHz < 11700000000 {
			band = BandLow
		}
		plane = &RFPlane{
			SatPosition:  SatellitePosition(tpKey.OrbitalPosition),
			Band:         band,
			Polarization: tpKey.Polarization,
		}
	}

	return MultiplexID{
		DVBType:        dvbType,
		DVBNamespace:   namespace,
		TSID:           tsid,
		ONID:           onid,
		RFPlane:        plane,
		TransponderKey: &tpKey,
	}
}
