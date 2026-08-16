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
// Returns (domainTopology, evaluationMode, isConfigured, err).
func ToDomainTopology(dto *ReceiverTopologyFileConfig) (receivertopology.ReceiverTopology, receivertopology.EvaluationMode, bool, error) {
	if dto == nil || len(dto.Inputs) == 0 {
		return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, nil
	}

	model := strings.TrimSpace(dto.Model)
	if model == "" {
		model = "Configured Receiver"
	}

	var domainInputs []receivertopology.PhysicalInput
	inputIDs := make(map[receivertopology.InputID]bool)

	for i, in := range dto.Inputs {
		id := receivertopology.InputID(strings.TrimSpace(in.ID))
		if id == "" {
			id = receivertopology.InputID(fmt.Sprintf("input_%c", 'a'+i))
		}
		if inputIDs[id] {
			return receivertopology.ReceiverTopology{}, receivertopology.EvaluationModeAuditOnly, false, fmt.Errorf("duplicate physical input id %q", id)
		}
		inputIDs[id] = true

		label := strings.TrimSpace(in.Label)
		if label == "" {
			label = fmt.Sprintf("Input %s", string(id))
		}

		delivery := mapDeliveryType(in.DeliveryType)

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
	if len(dto.Demodulators) > 0 {
		for i, d := range dto.Demodulators {
			demodID := receivertopology.DemodulatorID(strings.TrimSpace(d.ID))
			if demodID == "" {
				demodID = receivertopology.DemodulatorID(fmt.Sprintf("demod_%c", 'a'+i))
			}
			inputID := receivertopology.InputID(strings.TrimSpace(d.InputID))
			if inputID == "" {
				inputID = domainInputs[0].ID
			}

			var dvbTypes []receivertopology.DVBType
			for _, dt := range d.DVBTypes {
				switch strings.ToUpper(strings.TrimSpace(dt)) {
				case "DVB-C", "CABLE":
					dvbTypes = append(dvbTypes, receivertopology.DVBTypeCable)
				case "DVB-T", "DVB-T2", "TERRESTRIAL":
					dvbTypes = append(dvbTypes, receivertopology.DVBTypeTerrestrial)
				default:
					dvbTypes = append(dvbTypes, receivertopology.DVBTypeSat)
				}
			}
			if len(dvbTypes) == 0 {
				dvbTypes = []receivertopology.DVBType{receivertopology.DVBTypeSat}
			}

			domainDemods = append(domainDemods, receivertopology.Demodulator{
				ID:           demodID,
				InputID:      inputID,
				DVBTypes:     dvbTypes,
				IsFBCVirtual: d.IsFBCVirtual,
			})
		}
	} else {
		// Auto-expand demodulators if inputs are defined without explicit demodulator map
		if len(domainInputs) == 1 && (domainInputs[0].DeliveryType == receivertopology.DeliveryUnicable1 || domainInputs[0].DeliveryType == receivertopology.DeliveryUnicable2JESS) {
			// Unicable FBC: 8 demods on primary input
			for i := 0; i < 8; i++ {
				domainDemods = append(domainDemods, receivertopology.Demodulator{
					ID:           receivertopology.DemodulatorID(fmt.Sprintf("demod_%c", 'a'+i)),
					InputID:      domainInputs[0].ID,
					DVBTypes:     []receivertopology.DVBType{receivertopology.DVBTypeSat},
					IsFBCVirtual: i >= 2,
				})
			}
		} else if len(domainInputs) >= 2 {
			// Multi-cable FBC: 8 demods mapped across inputs (Demod A -> In A, Demod B -> In B, C..H virtual)
			for i := 0; i < 8; i++ {
				inID := domainInputs[0].ID
				if i == 1 {
					inID = domainInputs[1].ID
				}
				domainDemods = append(domainDemods, receivertopology.Demodulator{
					ID:           receivertopology.DemodulatorID(fmt.Sprintf("demod_%c", 'a'+i)),
					InputID:      inID,
					DVBTypes:     []receivertopology.DVBType{receivertopology.DVBTypeSat},
					IsFBCVirtual: i >= 2,
				})
			}
		} else {
			// Single standard tuner
			domainDemods = append(domainDemods, receivertopology.Demodulator{
				ID:           "demod_a",
				InputID:      domainInputs[0].ID,
				DVBTypes:     []receivertopology.DVBType{receivertopology.DVBTypeSat},
				IsFBCVirtual: false,
			})
		}
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

	mode := receivertopology.EvaluationModeEnforce
	if strings.EqualFold(strings.TrimSpace(dto.Mode), "audit_only") {
		mode = receivertopology.EvaluationModeAuditOnly
	}

	return domainTopo, mode, true, nil
}

func mapDeliveryType(raw string) receivertopology.DeliveryType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "unicable_1", "unicable", "scr":
		return receivertopology.DeliveryUnicable1
	case "unicable_2_jess", "jess", "unicable2":
		return receivertopology.DeliveryUnicable2JESS
	case "cable", "dvb-c":
		return receivertopology.DeliveryCable
	case "terrestrial", "dvb-t", "dvb-t2":
		return receivertopology.DeliveryTerrestrial
	default:
		return receivertopology.DeliveryLegacyUniversal
	}
}
