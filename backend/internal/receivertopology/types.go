// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import "fmt"

// Confidence represents the verification state of the receiver physical topology.
type Confidence string

const (
	// ConfidenceVerified indicates that the topology config is complete, internally consistent,
	// and verified valid against hardware capabilities (e.g. demod inputs exist, band limits valid).
	ConfidenceVerified Confidence = "VERIFIED"

	// ConfidenceObserved indicates the topology was automatically discovered / inferred from OpenWebIF.
	// Operates in audit-only / fail-open mode if limits are exceeded.
	ConfidenceObserved Confidence = "OBSERVED"

	// ConfidenceDefault indicates standard single/dual fallback without hardware verification.
	ConfidenceDefault Confidence = "DEFAULT"
)

// EvaluationMode defines whether capacity violations actively block requests or audit-log.
type EvaluationMode string

const (
	// EvaluationModeEnforce strictly blocks start requests when hardware demods or RF planes are exhausted.
	EvaluationModeEnforce EvaluationMode = "ENFORCE"

	// EvaluationModeAuditOnly permits requests even when calculating overload (fail-open / observation).
	EvaluationModeAuditOnly EvaluationMode = "AUDIT_ONLY"
)

// DVBType represents the broadcast transmission standard.
type DVBType string

const (
	DVBTypeSat         DVBType = "DVB_S"
	DVBTypeCable       DVBType = "DVB_C"
	DVBTypeTerrestrial DVBType = "DVB_T"
)

// Polarization represents DVB-S signal polarization.
type Polarization string

const (
	PolarizationHorizontal Polarization = "H"
	PolarizationVertical   Polarization = "V"
	PolarizationCircularL  Polarization = "L"
	PolarizationCircularR  Polarization = "R"
)

// Band represents the satellite frequency band (Universal LNB: Low < 11.7 GHz, High >= 11.7 GHz).
type Band string

const (
	BandLow  Band = "LOW"
	BandHigh Band = "HIGH"
)

// SatellitePosition represents satellite orbital position in tenths of a degree (e.g. Astra 19.2E = 192, Hotbird 13.0E = 130).
type SatellitePosition int

// RFPlane identifies a physical satellite RF quadrant: orbital position + frequency band + polarization.
// On Legacy Universal LNB, a single coaxial cable can deliver exactly ONE RFPlane at any given moment.
type RFPlane struct {
	SatPosition  SatellitePosition `json:"satPosition"`
	Band         Band              `json:"band"`
	Polarization Polarization      `json:"polarization"`
}

func (p RFPlane) String() string {
	return fmt.Sprintf("%d:%s:%s", p.SatPosition, p.Band, p.Polarization)
}

// DeliveryType specifies how RF signals reach the receiver tuners.
type DeliveryType string

const (
	// DeliveryLegacyUniversal represents standard Universal LNB (1 cable = 1 RF plane at a time).
	DeliveryLegacyUniversal DeliveryType = "LEGACY_UNIVERSAL"

	// DeliveryUnicable1 represents EN 50494 SCR Unicable (up to 8 SCR user bands per cable).
	DeliveryUnicable1 DeliveryType = "UNICABLE_1"

	// DeliveryUnicable2JESS represents EN 50607 JESS Unicable II (up to 32 user bands per cable).
	DeliveryUnicable2JESS DeliveryType = "UNICABLE_2_JESS"

	// DeliveryCable represents DVB-C full band delivery.
	DeliveryCable DeliveryType = "CABLE"

	// DeliveryTerrestrial represents DVB-T/T2 aerial delivery.
	DeliveryTerrestrial DeliveryType = "TERRESTRIAL"
)

// InputID uniquely identifies a physical coaxial / RF connector on the receiver backplane.
type InputID string

// PhysicalInput represents a physical RF input connector (e.g. "Tuner A (LNB 1 In)", "Tuner B (LNB 2 In)").
type PhysicalInput struct {
	ID           InputID             `json:"id"`
	Label        string              `json:"label"`
	DeliveryType DeliveryType        `json:"deliveryType"`
	UserBands    int                 `json:"userBands,omitempty"`
	Satellites   []SatellitePosition `json:"satellites,omitempty"`
}

// DemodulatorID uniquely identifies a hardware or virtual FBC demodulator.
type DemodulatorID string

// Demodulator represents an RF demodulator (Tuner A through H on an 8-demod FBC front-end).
type Demodulator struct {
	ID           DemodulatorID  `json:"id"`
	InputID      InputID        `json:"inputId"`
	DVBTypes     []DVBType      `json:"dvbTypes"`
	IsFBCVirtual bool           `json:"isFbcVirtual,omitempty"`
	LinkedTo     *DemodulatorID `json:"linkedTo,omitempty"`
}

// ReceiverTopology represents the verified physical and logical front-end topology of a receiver.
type ReceiverTopology struct {
	Model        string          `json:"model"`
	Confidence   Confidence      `json:"confidence"`
	Inputs       []PhysicalInput `json:"inputs"`
	Demodulators []Demodulator   `json:"demodulators"`
}

// TotalDemodulators returns the total count of hardware/virtual demodulators available.
func (t ReceiverTopology) TotalDemodulators() int {
	return len(t.Demodulators)
}

// FindInput looks up an input by ID.
func (t ReceiverTopology) FindInput(id InputID) (PhysicalInput, bool) {
	for _, in := range t.Inputs {
		if in.ID == id {
			return in, true
		}
	}
	return PhysicalInput{}, false
}

// FindDemod looks up a demodulator by ID.
func (t ReceiverTopology) FindDemod(id DemodulatorID) (Demodulator, bool) {
	for _, d := range t.Demodulators {
		if d.ID == id {
			return d, true
		}
	}
	return Demodulator{}, false
}

// EffectiveTunerCapacity calculates the number of physically independent concurrent tuner streams
// supported by the topology based on physical RF inputs, user bands, and delivery types.
func (t ReceiverTopology) EffectiveTunerCapacity() int {
	if len(t.Inputs) == 0 {
		return 0
	}
	total := 0
	for _, input := range t.Inputs {
		switch input.DeliveryType {
		case DeliveryLegacyUniversal:
			// 1 physical coaxial cable = 1 independent RF plane at a time
			total += 1
		case DeliveryUnicable1, DeliveryUnicable2JESS:
			userBands := input.UserBands
			if userBands <= 0 {
				userBands = 8
			}
			demodsOnInput := 0
			for _, d := range t.Demodulators {
				if d.InputID == input.ID {
					demodsOnInput++
				}
			}
			if demodsOnInput > 0 && demodsOnInput < userBands {
				total += demodsOnInput
			} else {
				total += userBands
			}
		case DeliveryCable, DeliveryTerrestrial:
			demodsOnInput := 0
			for _, d := range t.Demodulators {
				if d.InputID == input.ID {
					demodsOnInput++
				}
			}
			if demodsOnInput > 0 {
				total += demodsOnInput
			} else {
				total += 1
			}
		default:
			total += 1
		}
	}
	if total <= 0 {
		return 1
	}
	return total
}

// FrontendUsageKind describes what activity currently occupies a receiver frontend/demodulator.
type FrontendUsageKind string

const (
	FrontendUsageIdle           FrontendUsageKind = "idle"
	FrontendUsageLiveTV         FrontendUsageKind = "hdmi_live"
	FrontendUsageRecording      FrontendUsageKind = "local_dvr"
	FrontendUsageExternalStream FrontendUsageKind = "external_stream"
	FrontendUsageXG2G           FrontendUsageKind = "xg2g_stream"
	FrontendUsageUnknown        FrontendUsageKind = "unknown"
)

// FrontendRuntimeState describes the real-time operational state of a receiver frontend.
type FrontendRuntimeState struct {
	DemodID    DemodulatorID     `json:"demodId"`
	InputID    InputID           `json:"inputId"`
	Usage      FrontendUsageKind `json:"usage"`
	ServiceRef string            `json:"serviceRef,omitempty"`
	Owner      string            `json:"owner,omitempty"`
}
