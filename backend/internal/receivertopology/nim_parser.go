// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"bufio"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrNoNIMConfigFound is returned when no valid NIM configuration keys exist in settings.
	ErrNoNIMConfigFound = errors.New("no valid Enigma2 NIM configuration found in settings")
	// ErrInvalidNIMConfig is returned when NIM settings are contradictory or invalid.
	ErrInvalidNIMConfig = errors.New("invalid Enigma2 NIM configuration")
)

// NIMSlot holds raw configuration key-value pairs for one tuner slot (e.g. config.Nims.0.*).
type NIMSlot struct {
	Index        int
	ConfigMode   string
	DiseqcMode   string
	SatPos       int
	Unicable     bool
	UnicableMode string
	SCR          int
	Frequency    int
	ConnectedTo  int // -1 if not linked
	DVBType      string
	RawKeys      map[string]string
}

// ParseNIMSettings parses Enigma2 /etc/enigma2/settings text into a structured ReceiverTopology.
// Discovered topologies are assigned ConfidenceObserved.
func ParseNIMSettings(settingsContent string) (ReceiverTopology, error) {
	if strings.TrimSpace(settingsContent) == "" {
		return ReceiverTopology{}, ErrNoNIMConfigFound
	}

	slotsMap := make(map[int]*NIMSlot)
	scanner := bufio.NewScanner(strings.NewReader(settingsContent))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Match config.Nims.<index>.<subKey>
		if !strings.HasPrefix(key, "config.Nims.") {
			continue
		}

		keyParts := strings.Split(key, ".")
		if len(keyParts) < 4 {
			continue
		}

		slotIdx, err := strconv.Atoi(keyParts[2])
		if err != nil || slotIdx < 0 {
			continue
		}

		slot, exists := slotsMap[slotIdx]
		if !exists {
			slot = &NIMSlot{
				Index:       slotIdx,
				ConnectedTo: -1,
				SCR:         -1,
				RawKeys:     make(map[string]string),
			}
			slotsMap[slotIdx] = slot
		}

		subKey := strings.Join(keyParts[3:], ".")
		slot.RawKeys[subKey] = val

		switch subKey {
		case "configMode":
			slot.ConfigMode = strings.ToLower(val)
		case "diseqcmode":
			slot.DiseqcMode = strings.ToLower(val)
		case "diseqcA":
			if pos, err := strconv.Atoi(val); err == nil {
				slot.SatPos = pos
			}
		case "unicableMode":
			slot.UnicableMode = strings.ToLower(val)
		case "unicable.scr":
			if scr, err := strconv.Atoi(val); err == nil {
				slot.SCR = scr
			}
		case "unicable.frequency":
			if freq, err := strconv.Atoi(val); err == nil {
				slot.Frequency = freq
			}
		case "connectedTo":
			if parent, err := strconv.Atoi(val); err == nil {
				slot.ConnectedTo = parent
			}
		case "dvbType":
			slot.DVBType = strings.ToUpper(val)
		}
	}

	if len(slotsMap) == 0 {
		return ReceiverTopology{}, ErrNoNIMConfigFound
	}

	// Sort slot indices
	var indices []int
	for idx := range slotsMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var inputs []PhysicalInput
	var demods []Demodulator
	inputMap := make(map[InputID]bool)
	slotToInput := make(map[int]InputID)

	// Check if this is an FBC front-end (typically 8 slots with slots >= 2 auto-configured/virtual)
	isFBC := len(indices) >= 8

	// Detect unicable user bands count across all slots
	unicableSCRs := make(map[int]bool)
	for _, idx := range indices {
		slot := slotsMap[idx]
		if slot.ConfigMode == "unicable" || strings.Contains(slot.UnicableMode, "unicable") || slot.SCR >= 0 {
			slot.Unicable = true
			if slot.SCR >= 0 {
				unicableSCRs[slot.SCR] = true
			}
		}
	}

	for _, idx := range indices {
		slot := slotsMap[idx]
		tunerChar := string(rune('A' + idx))
		demodID := DemodulatorID("tuner_" + strings.ToLower(tunerChar))

		// Check if slot is explicitly disabled/unconfigured
		if slot.ConfigMode == "nothing" || slot.ConfigMode == "not_configured" {
			continue
		}

		dvbTypes := []DVBType{DVBTypeSat}
		if slot.DVBType == "DVB-C" || strings.Contains(slot.RawKeys["dvbType"], "DVB-C") {
			dvbTypes = []DVBType{DVBTypeCable}
		} else if slot.DVBType == "DVB-T" || strings.Contains(slot.RawKeys["dvbType"], "DVB-T") {
			dvbTypes = []DVBType{DVBTypeTerrestrial}
		}

		// Determine Physical Input assignment
		var assignedInput InputID

		if slot.Unicable {
			// All unicable SCRs share the single physical Unicable feeder
			assignedInput = "input_a"
			if !inputMap[assignedInput] {
				userBands := len(unicableSCRs)
				if userBands < 1 {
					userBands = 8 // Default standard unicable capacity
				}
				delivery := DeliveryUnicable1
				if userBands > 8 {
					delivery = DeliveryUnicable2JESS
				}
				inputs = append(inputs, PhysicalInput{
					ID:           assignedInput,
					Label:        "Unicable SCR Feeder (In A)",
					DeliveryType: delivery,
					UserBands:    userBands,
				})
				inputMap[assignedInput] = true
			}
			slotToInput[idx] = assignedInput

		} else if slot.ConfigMode == "loopthrough" || slot.ConnectedTo >= 0 {
			// Linked / loopthrough to a parent tuner
			parentIdx := slot.ConnectedTo
			if parentIdx < 0 {
				parentIdx = 0
			}
			parentInput, ok := slotToInput[parentIdx]
			if !ok {
				parentInput = "input_a"
			}
			assignedInput = parentInput
			slotToInput[idx] = assignedInput

		} else if idx == 0 {
			// Primary Tuner A (independent physical input)
			assignedInput = "input_a"
			if !inputMap[assignedInput] {
				inputs = append(inputs, PhysicalInput{
					ID:           assignedInput,
					Label:        "Tuner A (In)",
					DeliveryType: DeliveryLegacyUniversal,
				})
				inputMap[assignedInput] = true
			}
			slotToInput[idx] = assignedInput

		} else if idx == 1 && (slot.ConfigMode == "simple" || slot.ConfigMode == "advanced" || slot.ConfigMode == "equal" || slot.ConfigMode == "satposdepends") {
			// Secondary Tuner B configured as independent cable
			assignedInput = "input_b"
			if !inputMap[assignedInput] {
				inputs = append(inputs, PhysicalInput{
					ID:           assignedInput,
					Label:        "Tuner B (In)",
					DeliveryType: DeliveryLegacyUniversal,
				})
				inputMap[assignedInput] = true
			}
			slotToInput[idx] = assignedInput

		} else {
			// Slots >= 2 (FBC virtual demods or secondary discrete tuners)
			if isFBC {
				// FBC virtual demods route through primary input_a by default
				assignedInput = "input_a"
			} else {
				// Discrete multi-tuner
				assignedInput = InputID(fmt.Sprintf("input_%c", 'a'+idx))
				if !inputMap[assignedInput] {
					inputs = append(inputs, PhysicalInput{
						ID:           assignedInput,
						Label:        fmt.Sprintf("Tuner %c (In)", 'A'+idx),
						DeliveryType: DeliveryLegacyUniversal,
					})
					inputMap[assignedInput] = true
				}
			}
			slotToInput[idx] = assignedInput
		}

		isVirtual := isFBC && idx >= 2
		demods = append(demods, Demodulator{
			ID:           demodID,
			InputID:      assignedInput,
			DVBTypes:     dvbTypes,
			IsFBCVirtual: isVirtual,
		})
	}

	if len(inputs) == 0 || len(demods) == 0 {
		return ReceiverTopology{}, fmt.Errorf("%w: no active physical inputs or demodulators configured", ErrInvalidNIMConfig)
	}

	return ReceiverTopology{
		Model:        "Enigma2 Receiver (Observed)",
		Confidence:   ConfidenceObserved,
		Inputs:       inputs,
		Demodulators: demods,
	}, nil
}
