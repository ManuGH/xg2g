// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// DiscoverTopology infers receiver physical topology from OpenWebIF /api/about metadata.
// Discovered topologies obtain ConfidenceObserved (fail-open / audit-only evaluation).
// Configured / verified topologies with explicit multi-cable or Unicable routing take precedence.
func DiscoverTopology(about *openwebif.AboutInfo) ReceiverTopology {
	if about == nil {
		return DefaultFallbackTopology()
	}

	modelName := strings.TrimSpace(about.Info.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(about.Info.Boxtype)
	}
	if modelName == "" {
		modelName = "Generic Enigma2 Receiver"
	}

	rawTuners := about.Info.Tuners
	if len(rawTuners) == 0 {
		return DefaultFallbackTopology()
	}

	var inputs []PhysicalInput
	var demods []Demodulator

	// Detect FBC Tuner presence
	hasFBC := false
	for _, t := range rawTuners {
		if strings.Contains(strings.ToUpper(t.Type), "FBC") {
			hasFBC = true
			break
		}
	}

	if hasFBC {
		// Generic FBC Observed: Root Input A with N multi-demodulators
		// No hardcoded orbital positions or assumptions on secondary cabling
		inputA := PhysicalInput{
			ID:           "input_a",
			Label:        "Tuner A (FBC In)",
			DeliveryType: DeliveryLegacyUniversal,
			Satellites:   nil, // Unrestricted satellite positions
		}
		inputs = append(inputs, inputA)

		for i, t := range rawTuners {
			demodID := DemodulatorID(sanitizeTunerID(t.Name, i))
			dvbTypes := parseDVBTypes(t.Type)
			demods = append(demods, Demodulator{
				ID:           demodID,
				InputID:      "input_a",
				DVBTypes:     dvbTypes,
				IsFBCVirtual: i >= 1,
			})
		}
	} else {
		// Standard Discrete Tuners (Single, Dual, Triple)
		for i, t := range rawTuners {
			inputID := InputID("input_" + string(rune('a'+i)))
			delivery := inferDeliveryType(t.Type)
			inputs = append(inputs, PhysicalInput{
				ID:           inputID,
				Label:        t.Name,
				DeliveryType: delivery,
				Satellites:   nil,
			})

			demodID := DemodulatorID(sanitizeTunerID(t.Name, i))
			dvbTypes := parseDVBTypes(t.Type)
			demods = append(demods, Demodulator{
				ID:           demodID,
				InputID:      inputID,
				DVBTypes:     dvbTypes,
				IsFBCVirtual: false,
			})
		}
	}

	return ReceiverTopology{
		Model:        modelName,
		Confidence:   ConfidenceObserved,
		Inputs:       inputs,
		Demodulators: demods,
	}
}

// ExtractExternalAllocations identifies receiver-observed tuner usage not owned by xg2g sessions.
// Checks demodulator ownership and accurately resolves the associated physical RF input.
func ExtractExternalAllocations(
	about *openwebif.AboutInfo,
	topology ReceiverTopology,
	activeXG2GDemods map[DemodulatorID]bool,
) []ExternalAllocation {
	if about == nil {
		return nil
	}

	var external []ExternalAllocation

	for i, t := range about.Info.Tuners {
		demodID := DemodulatorID(sanitizeTunerID(t.Name, i))

		// If xg2g currently holds a lease on this specific demodulator, it is our own stream
		if activeXG2GDemods[demodID] {
			continue
		}

		// Resolve physical input connected to this demodulator
		var inputID *InputID
		if demod, ok := topology.FindDemod(demodID); ok {
			in := demod.InputID
			inputID = &in
		}

		// Check Live TV on HDMI output
		if sRef := strings.TrimSpace(t.Live); sRef != "" {
			if mux, err := ParseServiceRef(sRef); err == nil {
				external = append(external, ExternalAllocation{
					Source:      "hdmi_live_tv",
					DemodID:     &demodID,
					InputID:     inputID,
					MultiplexID: &mux,
					RFPlane:     mux.RFPlane,
				})
				continue
			}
		}

		// Check Local Receiver Timer Recording
		if sRef := strings.TrimSpace(t.Rec); sRef != "" {
			if mux, err := ParseServiceRef(sRef); err == nil {
				external = append(external, ExternalAllocation{
					Source:      "local_timer_dvr",
					DemodID:     &demodID,
					InputID:     inputID,
					MultiplexID: &mux,
					RFPlane:     mux.RFPlane,
				})
				continue
			}
		}

		// Check External Streaming Client
		if sRef := strings.TrimSpace(t.Stream); sRef != "" {
			if mux, err := ParseServiceRef(sRef); err == nil {
				external = append(external, ExternalAllocation{
					Source:      "external_stream_client",
					DemodID:     &demodID,
					InputID:     inputID,
					MultiplexID: &mux,
					RFPlane:     mux.RFPlane,
				})
				continue
			}
		}
	}

	return external
}

// DefaultFallbackTopology provides a safe generic fallback with 2 standard universal tuners.
func DefaultFallbackTopology() ReceiverTopology {
	inputA := PhysicalInput{ID: "input_a", Label: "Tuner A", DeliveryType: DeliveryLegacyUniversal}
	inputB := PhysicalInput{ID: "input_b", Label: "Tuner B", DeliveryType: DeliveryLegacyUniversal}

	return ReceiverTopology{
		Model:      "Generic Dual Receiver",
		Confidence: ConfidenceDefault,
		Inputs:     []PhysicalInput{inputA, inputB},
		Demodulators: []Demodulator{
			{ID: "demod_a", InputID: "input_a", DVBTypes: []DVBType{DVBTypeSat}},
			{ID: "demod_b", InputID: "input_b", DVBTypes: []DVBType{DVBTypeSat}},
		},
	}
}

func sanitizeTunerID(name string, fallbackIndex int) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		return "demod_" + string(rune('a'+fallbackIndex))
	}
	return name
}

func parseDVBTypes(typeStr string) []DVBType {
	typeUpper := strings.ToUpper(typeStr)
	var types []DVBType
	if strings.Contains(typeUpper, "DVB-S") || strings.Contains(typeUpper, "DVB_S") {
		types = append(types, DVBTypeSat)
	}
	if strings.Contains(typeUpper, "DVB-C") || strings.Contains(typeUpper, "DVB_C") {
		types = append(types, DVBTypeCable)
	}
	if strings.Contains(typeUpper, "DVB-T") || strings.Contains(typeUpper, "DVB_T") {
		types = append(types, DVBTypeTerrestrial)
	}
	if len(types) == 0 {
		types = append(types, DVBTypeSat)
	}
	return types
}

func inferDeliveryType(typeStr string) DeliveryType {
	typeUpper := strings.ToUpper(typeStr)
	switch {
	case strings.Contains(typeUpper, "DVB-C"):
		return DeliveryCable
	case strings.Contains(typeUpper, "DVB-T"):
		return DeliveryTerrestrial
	default:
		return DeliveryLegacyUniversal
	}
}
