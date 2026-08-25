// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"strings"
)

// ValidationError describes a structural inconsistency in a receiver topology configuration.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("invalid receiver topology at %s: %s", e.Field, e.Message)
}

// Validate checks the complete internal consistency and hardware validity of a ReceiverTopology.
// A topology can only obtain ConfidenceVerified if Validate passes with zero errors.
func Validate(topology ReceiverTopology) error {
	if strings.TrimSpace(topology.Model) == "" {
		return ValidationError{Field: "model", Message: "receiver model name cannot be empty"}
	}

	if len(topology.Inputs) == 0 {
		return ValidationError{Field: "inputs", Message: "at least one physical RF input must be defined"}
	}

	inputMap := make(map[InputID]PhysicalInput, len(topology.Inputs))
	for i, in := range topology.Inputs {
		if strings.TrimSpace(string(in.ID)) == "" {
			return ValidationError{Field: fmt.Sprintf("inputs[%d].id", i), Message: "input ID cannot be empty"}
		}
		if _, exists := inputMap[in.ID]; exists {
			return ValidationError{Field: fmt.Sprintf("inputs[%d].id", i), Message: fmt.Sprintf("duplicate input ID %q", in.ID)}
		}

		switch in.DeliveryType {
		case DeliveryLegacyUniversal, DeliveryCable, DeliveryTerrestrial:
			// Valid standard delivery
		case DeliveryUnicable1, DeliveryUnicable2JESS:
			if in.UserBands <= 0 {
				return ValidationError{Field: fmt.Sprintf("inputs[%d].userBands", i), Message: "Unicable/JESS inputs must configure userBands > 0"}
			}
		default:
			return ValidationError{Field: fmt.Sprintf("inputs[%d].deliveryType", i), Message: fmt.Sprintf("unknown delivery type %q", in.DeliveryType)}
		}

		inputMap[in.ID] = in
	}

	if len(topology.Demodulators) == 0 {
		return ValidationError{Field: "demodulators", Message: "at least one demodulator must be defined"}
	}

	demodMap := make(map[DemodulatorID]Demodulator, len(topology.Demodulators))
	for i, d := range topology.Demodulators {
		if strings.TrimSpace(string(d.ID)) == "" {
			return ValidationError{Field: fmt.Sprintf("demodulators[%d].id", i), Message: "demodulator ID cannot be empty"}
		}
		if _, exists := demodMap[d.ID]; exists {
			return ValidationError{Field: fmt.Sprintf("demodulators[%d].id", i), Message: fmt.Sprintf("duplicate demodulator ID %q", d.ID)}
		}

		// Verify demodulator points to a valid, existing physical input
		if _, inputExists := inputMap[d.InputID]; !inputExists {
			return ValidationError{
				Field:   fmt.Sprintf("demodulators[%d].inputId", i),
				Message: fmt.Sprintf("demodulator %q references non-existent physical input %q", d.ID, d.InputID),
			}
		}

		// Verify linked demodulator if configured
		if d.LinkedTo != nil {
			if *d.LinkedTo == d.ID {
				return ValidationError{Field: fmt.Sprintf("demodulators[%d].linkedTo", i), Message: "demodulator cannot link to itself"}
			}
		}

		demodMap[d.ID] = d
	}

	// Verify all LinkedTo references exist
	for i, d := range topology.Demodulators {
		if d.LinkedTo != nil {
			if _, targetExists := demodMap[*d.LinkedTo]; !targetExists {
				return ValidationError{
					Field:   fmt.Sprintf("demodulators[%d].linkedTo", i),
					Message: fmt.Sprintf("demodulator %q references non-existent linked demodulator %q", d.ID, *d.LinkedTo),
				}
			}
		}
	}

	return nil
}
