// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"time"
)

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

// DeliverySystem represents the specific transmission modulation standard.
type DeliverySystem string

const (
	DeliverySystemDVBS  DeliverySystem = "DVB-S"
	DeliverySystemDVBS2 DeliverySystem = "DVB-S2"
	DeliverySystemDVBC  DeliverySystem = "DVB-C"
	DeliverySystemDVBC2 DeliverySystem = "DVB-C2"
	DeliverySystemDVBT  DeliverySystem = "DVB-T"
	DeliverySystemDVBT2 DeliverySystem = "DVB-T2"
)

// PLSMode represents Physical Layer Scrambling mode for DVB-S2 multistream transponders.
type PLSMode string

const (
	PLSModeRoot  PLSMode = "ROOT"
	PLSModeGold  PLSMode = "GOLD"
	PLSModeCombo PLSMode = "COMBO"
)

// TransponderKey represents the complete, strongly typed physical RF tuning identity of a transponder.
type TransponderKey struct {
	DeliverySystem  DeliverySystem `json:"deliverySystem"`
	OrbitalPosition int            `json:"orbitalPosition"` // In tenths of a degree (e.g. 192 for Astra 19.2E)
	FrequencyHz     uint64         `json:"frequencyHz"`
	Polarization    Polarization   `json:"polarization"`
	StreamID        int            `json:"streamId,omitempty"` // DVB-S2 MIS / DVB-T2 PLP ID (-1 if unused)
	PLSMode         PLSMode        `json:"plsMode,omitempty"`
	PLSCode         uint32         `json:"plsCode,omitempty"`
}

// Canonical returns the deterministic, delivery-system specific canonical string representation.
func (k TransponderKey) Canonical() string {
	switch k.DeliverySystem {
	case DeliverySystemDVBS, DeliverySystemDVBS2:
		base := fmt.Sprintf("%s:%d:%d:%s", k.DeliverySystem, k.OrbitalPosition, k.FrequencyHz, k.Polarization)
		if k.StreamID >= 0 {
			base += fmt.Sprintf(":stream=%d", k.StreamID)
		}
		if k.PLSMode != "" || k.PLSCode > 0 {
			base += fmt.Sprintf(":pls=%s:%d", k.PLSMode, k.PLSCode)
		}
		return base
	case DeliverySystemDVBC, DeliverySystemDVBC2:
		return fmt.Sprintf("%s:%d", k.DeliverySystem, k.FrequencyHz)
	case DeliverySystemDVBT, DeliverySystemDVBT2:
		if k.StreamID >= 0 {
			return fmt.Sprintf("%s:%d:plp=%d", k.DeliverySystem, k.FrequencyHz, k.StreamID)
		}
		return fmt.Sprintf("%s:%d", k.DeliverySystem, k.FrequencyHz)
	default:
		return fmt.Sprintf("%s:%d:%d:%s", k.DeliverySystem, k.OrbitalPosition, k.FrequencyHz, k.Polarization)
	}
}

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

// EvidenceLevel indicates the provenance and confidence of an observed receiver runtime fact.
type EvidenceLevel string

const (
	// EvidenceObserved indicates the fact was directly observed from an authoritative OpenWebIF endpoint field.
	EvidenceObserved EvidenceLevel = "observed"

	// EvidenceInferred indicates the fact was derived from multiple contextual or temporal clues (e.g. active timer window + global isRecording flag).
	EvidenceInferred EvidenceLevel = "inferred"

	// EvidenceUnknown indicates the fact could not be determined or is missing/unreliable.
	EvidenceUnknown EvidenceLevel = "unknown"
)

// ObservedService contains observed details of a service running on the receiver.
type ObservedService struct {
	ServiceRef  string         `json:"serviceRef"`
	ServiceName string         `json:"serviceName,omitempty"`
	MultiplexID *MultiplexID   `json:"multiplexId,omitempty"`
	RFPlane     *RFPlane       `json:"rfPlane,omitempty"`
	PIDs        map[string]any `json:"pids,omitempty"`
	Evidence    EvidenceLevel  `json:"evidence"`
}

// ObservedRecording contains information on an active or upcoming local DVR timer on the receiver.
type ObservedRecording struct {
	TimerID     string        `json:"timerId"`
	Title       string        `json:"title"`
	ServiceRef  string        `json:"serviceRef"`
	MultiplexID *MultiplexID  `json:"multiplexId,omitempty"`
	StartTime   time.Time     `json:"startTime"`
	EndTime     time.Time     `json:"endTime"`
	IsActiveNow bool          `json:"isActiveNow"`
	Evidence    EvidenceLevel `json:"evidence"`
}

// ObservedStream represents a stream connection reported by OpenWebIF.
type ObservedStream struct {
	RawRef      string        `json:"rawRef"`
	ServiceRef  string        `json:"serviceRef,omitempty"`
	MultiplexID *MultiplexID  `json:"multiplexId,omitempty"`
	TunerIndex  int           `json:"tunerIndex,omitempty"`
	ClientInfo  string        `json:"clientInfo,omitempty"`
	Evidence    EvidenceLevel `json:"evidence"`
}

// ObservedTunerSlot represents the raw status of a physical tuner slot in /api/about.
type ObservedTunerSlot struct {
	Index     int           `json:"index"`
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	RawLive   string        `json:"rawLive,omitempty"`
	RawRec    string        `json:"rawRec,omitempty"`
	RawStream string        `json:"rawStream,omitempty"`
	Evidence  EvidenceLevel `json:"evidence"`
}

// ReceiverRuntimeSnapshot captures a point-in-time evidentiary observation of receiver state
// aggregated across multiple OpenWebIF endpoints (/api/about, /api/getcurrent, /api/statusinfo, /api/timerlist).
type ReceiverRuntimeSnapshot struct {
	ObservedAt         time.Time           `json:"observedAt"`
	InStandby          bool                `json:"inStandby"`
	StandbyEvidence    EvidenceLevel       `json:"standbyEvidence"`
	HDMIPlayback       *ObservedService    `json:"hdmiPlayback,omitempty"` // Retained even during standby, paired with InStandby
	ActiveRecordings   []ObservedRecording `json:"activeRecordings,omitempty"`
	UpcomingRecordings []ObservedRecording `json:"upcomingRecordings,omitempty"`
	ReportedStreams    []ObservedStream    `json:"reportedStreams,omitempty"`
	StreamPresence     StreamPresence      `json:"streamPresence"`
	Tuners             []ObservedTunerSlot `json:"tuners,omitempty"`
}

// IsFresh reports whether the evidentiary snapshot was observed within maxAge.
func (s ReceiverRuntimeSnapshot) IsFresh(maxAge time.Duration, now time.Time) bool {
	if s.ObservedAt.IsZero() {
		return false
	}
	if maxAge <= 0 {
		maxAge = 15 * time.Second
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.Sub(s.ObservedAt) <= maxAge
}

// DemuxLimitConfig specifies demux limits and their provenance.
type DemuxLimitConfig struct {
	MaxMuxMembers int    `json:"maxMuxMembers"`
	Source        string `json:"source"`
}

// AllocationDiagnostics provides sanitized, structured operational facts explaining an admission decision.
// STRICT INVARIANT: Must NEVER include bearer tokens, credentials, IP addresses, or principal IDs.
type AllocationDiagnostics struct {
	ServiceRef         string `json:"serviceRef"`
	MultiplexID        string `json:"multiplexId,omitempty"`
	RequiredPlane      string `json:"requiredPlane,omitempty"`
	EvaluatedInputID   string `json:"evaluatedInputId,omitempty"`
	EvaluatedDemodID   string `json:"evaluatedDemodId,omitempty"`
	ConflictingInputID string `json:"conflictingInputId,omitempty"`
	ActivePlaneOnInput string `json:"activePlaneOnInput,omitempty"`
	ConflictingOwner   string `json:"conflictingOwner,omitempty"`
	ConflictingTimer   string `json:"conflictingTimer,omitempty"`
	DemuxMemberCount   int    `json:"demuxMemberCount,omitempty"`
	DemuxMaxMembers    int    `json:"demuxMaxMembers,omitempty"`
	SnapshotAgeMs      int64  `json:"snapshotAgeMs"`
	SnapshotFresh      bool   `json:"snapshotFresh"`
	DecisionCode       string `json:"decisionCode"`
}
