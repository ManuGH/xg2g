// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/receivertopology"
)

// ToDomainTopology maps the decoupled YAML DTO ReceiverTopologyFileConfig into a validated Domain ReceiverTopology.
// Enforces complete, explicit hardware specifications without guessing or phantom defaults.
// Returns (domainTopology, evaluationMode, isConfigured, err).
func ToDomainTopology(dto *ReceiverTopologyFileConfig) (receivertopology.ReceiverTopology, receivertopology.EvaluationMode, bool, error) {
	if dto == nil || (len(dto.Inputs) == 0 && len(dto.Demodulators) == 0 && strings.TrimSpace(dto.Model) == "") {
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, nil
	}

	if len(dto.Inputs) == 0 {
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("receiver topology configuration must define at least one physical input")
	}

	if len(dto.Demodulators) == 0 {
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("receiver topology configuration must explicitly define demodulators")
	}

	model := strings.TrimSpace(dto.Model)
	if model == "" {
		model = "Configured Receiver"
	}

	// Validate Mode
	modeStr := strings.ToLower(strings.TrimSpace(dto.Mode))
	var mode receivertopology.EvaluationMode
	switch modeStr {
	case "enforce", "":
		mode = receivertopology.EvaluationModeEnforce
	case "audit_only":
		mode = receivertopology.EvaluationModeAuditOnly
	default:
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("invalid receiver topology mode %q: must be 'enforce' or 'audit_only'", dto.Mode)
	}

	var domainInputs []receivertopology.PhysicalInput
	inputIDs := make(map[receivertopology.InputID]bool)

	for i, in := range dto.Inputs {
		id := receivertopology.InputID(strings.TrimSpace(in.ID))
		if id == "" {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("input at index %d has empty id", i)
		}
		if inputIDs[id] {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("duplicate physical input id %q", id)
		}
		inputIDs[id] = true

		label := strings.TrimSpace(in.Label)
		if label == "" {
			label = fmt.Sprintf("Input %s", string(id))
		}

		delivery, err := parseDeliveryType(in.DeliveryType, string(id))
		if err != nil {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, err
		}

		var sats []receivertopology.SatellitePosition
		for _, s := range in.Satellites {
			sats = append(sats, receivertopology.SatellitePosition(s))
		}

		domainInputs = append(domainInputs, receivertopology.PhysicalInput{
			ID:           id,
			Label:        label,
			DeliveryType: delivery,
			UserBands:    in.UserBands,
			Satellites:   sats,
		})
	}

	var domainDemods []receivertopology.Demodulator
	demodIDs := make(map[receivertopology.DemodulatorID]bool)

	for i, d := range dto.Demodulators {
		demodID := receivertopology.DemodulatorID(strings.TrimSpace(d.ID))
		if demodID == "" {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("demodulator at index %d has empty id", i)
		}
		if demodIDs[demodID] {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("duplicate demodulator id %q", demodID)
		}
		demodIDs[demodID] = true

		inputID := receivertopology.InputID(strings.TrimSpace(d.InputID))
		if inputID == "" {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("demodulator %q has empty input_id", demodID)
		}
		if !inputIDs[inputID] {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("demodulator %q references non-existent input_id %q", demodID, inputID)
		}

		if len(d.DVBTypes) == 0 {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("demodulator %q has empty dvb_types", demodID)
		}

		var dvbTypes []receivertopology.DVBType
		for _, dt := range d.DVBTypes {
			parsedDT, err := parseDVBType(dt, string(demodID))
			if err != nil {
				return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, err
			}
			dvbTypes = append(dvbTypes, parsedDT)
		}

		domainDemods = append(domainDemods, receivertopology.Demodulator{
			ID:           demodID,
			InputID:      inputID,
			DVBTypes:     dvbTypes,
			IsFBCVirtual: d.IsFBCVirtual,
		})
	}

	domainTopo := receivertopology.ReceiverTopology{
		Model:        model,
		Confidence:   receivertopology.ConfidenceVerified,
		Inputs:       domainInputs,
		Demodulators: domainDemods,
	}

	if err := receivertopology.Validate(domainTopo); err != nil {
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("invalid receiver topology configuration: %w", err)
	}

	return domainTopo, mode, true, nil
}

func parseDeliveryType(raw, inputID string) (receivertopology.DeliveryType, error) {
	norm := strings.ToLower(strings.TrimSpace(raw))
	switch norm {
	case "legacy_universal", "universal", "legacy":
		return receivertopology.DeliveryLegacyUniversal, nil
	case "unicable_1", "unicable", "scr":
		return receivertopology.DeliveryUnicable1, nil
	case "unicable_2_jess", "jess", "unicable2":
		return receivertopology.DeliveryUnicable2JESS, nil
	case "cable", "dvb-c", "dvb_c":
		return receivertopology.DeliveryCable, nil
	case "terrestrial", "dvb-t", "dvb-t2", "dvb_t", "dvb_t2":
		return receivertopology.DeliveryTerrestrial, nil
	default:
		return "", fmt.Errorf("invalid delivery_type %q for input %q", raw, inputID)
	}
}

func parseDVBType(raw, demodID string) (receivertopology.DVBType, error) {
	norm := strings.ToUpper(strings.TrimSpace(raw))
	switch norm {
	case "DVB-S", "DVB_S", "DVB-S2", "DVB_S2", "SAT":
		return receivertopology.DVBTypeSat, nil
	case "DVB-C", "DVB_C", "CABLE":
		return receivertopology.DVBTypeCable, nil
	case "DVB-T", "DVB_T", "DVB-T2", "DVB_T2", "TERRESTRIAL":
		return receivertopology.DVBTypeTerrestrial, nil
	default:
		return "", fmt.Errorf("invalid dvb_type %q for demodulator %q", raw, demodID)
	}
}
