// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// AllocationOwner denotes whether an allocation belongs to an xg2g session or an external receiver activity.
type AllocationOwner string

const (
	AllocationOwnerXG2G     AllocationOwner = "XG2G"
	AllocationOwnerExternal AllocationOwner = "EXTERNAL"
)

// DemodAllocation represents an active hardware or virtual demodulator assignment.
type DemodAllocation struct {
	DemodID     DemodulatorID   `json:"demodId"`
	InputID     InputID         `json:"inputId"`
	MultiplexID MultiplexID     `json:"multiplexId"`
	RFPlane     *RFPlane        `json:"rfPlane,omitempty"`
	Owner       AllocationOwner `json:"owner"`
	SessionIDs  []string        `json:"sessionIds"` // Multiple xg2g sessions can share the same multiplex
}

// HasSession checks if a given session ID is registered on this multiplex allocation.
func (d *DemodAllocation) HasSession(sessionID string) bool {
	for _, s := range d.SessionIDs {
		if s == sessionID {
			return true
		}
	}
	return false
}

// RemoveSession removes a session from this demod allocation.
func (d *DemodAllocation) RemoveSession(sessionID string) bool {
	for i, s := range d.SessionIDs {
		if s == sessionID {
			d.SessionIDs = append(d.SessionIDs[:i], d.SessionIDs[i+1:]...)
			return true
		}
	}
	return false
}

// ExternalAllocation represents a receiver-observed tuner allocation not initiated by xg2g (e.g. HDMI Live, PiP, local DVR).
type ExternalAllocation struct {
	Source      string         `json:"source"`
	DemodID     *DemodulatorID `json:"demodId,omitempty"`
	InputID     *InputID       `json:"inputId,omitempty"`
	MultiplexID *MultiplexID   `json:"multiplexId,omitempty"`
	RFPlane     *RFPlane       `json:"rfPlane,omitempty"`
}

// RuntimeAllocation maintains the combined active state of all demodulators, RF planes, and external allocations.
type RuntimeAllocation struct {
	mu                  sync.RWMutex
	ActiveMultiplexes   map[string]*DemodAllocation // Keyed by MultiplexID canonical string
	ActiveInputPlanes   map[InputID]RFPlane
	ExternalAllocations []ExternalAllocation
}

// NewRuntimeAllocation initializes an empty runtime allocation state.
func NewRuntimeAllocation() *RuntimeAllocation {
	return &RuntimeAllocation{
		ActiveMultiplexes:   make(map[string]*DemodAllocation),
		ActiveInputPlanes:   make(map[InputID]RFPlane),
		ExternalAllocations: nil,
	}
}

// Clone creates a deep copy of the runtime allocation state for safe inspection.
func (r *RuntimeAllocation) Clone() *RuntimeAllocation {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := &RuntimeAllocation{
		ActiveMultiplexes:   make(map[string]*DemodAllocation, len(r.ActiveMultiplexes)),
		ActiveInputPlanes:   make(map[InputID]RFPlane, len(r.ActiveInputPlanes)),
		ExternalAllocations: append([]ExternalAllocation(nil), r.ExternalAllocations...),
	}
	for k, v := range r.ActiveMultiplexes {
		copyAlloc := *v
		copyAlloc.SessionIDs = append([]string(nil), v.SessionIDs...)
		out.ActiveMultiplexes[k] = &copyAlloc
	}
	for k, v := range r.ActiveInputPlanes {
		out.ActiveInputPlanes[k] = v
	}
	return out
}

// AllocationDecision contains the verdict and assignment details for a stream allocation request.
type AllocationDecision struct {
	Allowed        bool           `json:"allowed"`
	DemodID        DemodulatorID  `json:"demodId,omitempty"`
	InputID        InputID        `json:"inputId,omitempty"`
	ReusedDemod    bool           `json:"reusedDemod"`
	Reason         string         `json:"reason"`
	EvaluationMode EvaluationMode `json:"evaluationMode"`
}

// Allocator evaluates and manages front-end physical RF topology capacity.
type Allocator struct {
	topology ReceiverTopology
	mode     EvaluationMode
}

// NewAllocator creates a new Allocator for a given verified or observed receiver topology.
func NewAllocator(topology ReceiverTopology, mode EvaluationMode) *Allocator {
	return &Allocator{
		topology: topology,
		mode:     mode,
	}
}

// Topology returns the active receiver topology.
func (a *Allocator) Topology() ReceiverTopology {
	return a.topology
}

// Mode returns the active evaluation mode.
func (a *Allocator) Mode() EvaluationMode {
	return a.mode
}

// CanAllocate evaluates whether target Multiplex can be tuned given current runtime allocations.
func (a *Allocator) CanAllocate(runtime *RuntimeAllocation, target MultiplexID, sessionID string) AllocationDecision {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()

	// 1. Multiplex-Reuse: If identical physical transport stream is already tuned on an active demodulator
	for _, alloc := range runtime.ActiveMultiplexes {
		if alloc.MultiplexID.IsSamePhysicalMultiplex(target) {
			return AllocationDecision{
				Allowed:        true,
				DemodID:        alloc.DemodID,
				InputID:        alloc.InputID,
				ReusedDemod:    true,
				Reason:         fmt.Sprintf("Reusing active multiplex %s on demod %s", target.String(), alloc.DemodID),
				EvaluationMode: a.mode,
			}
		}
	}

	// 2. Identify occupied demodulators, input usage counts, and active RF planes
	occupiedDemods := make(map[DemodulatorID]bool)
	inputMuxCount := make(map[InputID]int)
	effectivePlanes := make(map[InputID]RFPlane, len(runtime.ActiveInputPlanes))

	for inID, pl := range runtime.ActiveInputPlanes {
		effectivePlanes[inID] = pl
	}

	for _, alloc := range runtime.ActiveMultiplexes {
		occupiedDemods[alloc.DemodID] = true
		inputMuxCount[alloc.InputID]++
	}
	for _, ext := range runtime.ExternalAllocations {
		if ext.DemodID != nil {
			occupiedDemods[*ext.DemodID] = true
		}
		if ext.InputID != nil {
			inputMuxCount[*ext.InputID]++
			if ext.RFPlane != nil {
				effectivePlanes[*ext.InputID] = *ext.RFPlane
			}
		}
	}

	// 3. Search for an available, physically compatible demodulator
	for _, demod := range a.topology.Demodulators {
		if occupiedDemods[demod.ID] {
			continue
		}

		input, ok := a.topology.FindInput(demod.InputID)
		if !ok {
			continue
		}

		// Check DVB Type support
		if !supportsDVBType(demod.DVBTypes, target.DVBType) {
			continue
		}

		// Check Physical Delivery Type Constraints
		switch input.DeliveryType {
		case DeliveryLegacyUniversal:
			if target.RFPlane == nil {
				// SAT without plane info or DVB-C/T on legacy input: allow assignment
				return AllocationDecision{
					Allowed:        true,
					DemodID:        demod.ID,
					InputID:        input.ID,
					ReusedDemod:    false,
					Reason:         fmt.Sprintf("Allocating free demod %s on input %s", demod.ID, input.ID),
					EvaluationMode: a.mode,
				}
			}

			activePlane, planeActive := effectivePlanes[input.ID]
			if !planeActive {
				// Input is idle -> can lock to new RFPlane
				return AllocationDecision{
					Allowed:        true,
					DemodID:        demod.ID,
					InputID:        input.ID,
					ReusedDemod:    false,
					Reason:         fmt.Sprintf("Allocating free demod %s, locking input %s to plane %s", demod.ID, input.ID, target.RFPlane.String()),
					EvaluationMode: a.mode,
				}
			}

			// Input is active on a plane -> check if target matches the active plane
			if activePlane.String() == target.RFPlane.String() {
				// FBC multi-demod sharing the active RF plane
				return AllocationDecision{
					Allowed:        true,
					DemodID:        demod.ID,
					InputID:        input.ID,
					ReusedDemod:    false,
					Reason:         fmt.Sprintf("Allocating free demod %s sharing active plane %s on input %s", demod.ID, activePlane.String(), input.ID),
					EvaluationMode: a.mode,
				}
			}

			// Plane conflict on this input -> try next demodulator/input
			continue

		case DeliveryUnicable1, DeliveryUnicable2JESS:
			maxBands := input.UserBands
			if maxBands <= 0 {
				maxBands = 8 // Standard Unicable default
			}
			if inputMuxCount[input.ID] < maxBands {
				return AllocationDecision{
					Allowed:        true,
					DemodID:        demod.ID,
					InputID:        input.ID,
					ReusedDemod:    false,
					Reason:         fmt.Sprintf("Allocating Unicable demod %s on input %s (slot %d/%d)", demod.ID, input.ID, inputMuxCount[input.ID]+1, maxBands),
					EvaluationMode: a.mode,
				}
			}
			// User bands exhausted on this input -> try next
			continue

		case DeliveryCable, DeliveryTerrestrial:
			// Full frequency agility
			return AllocationDecision{
				Allowed:        true,
				DemodID:        demod.ID,
				InputID:        input.ID,
				ReusedDemod:    false,
				Reason:         fmt.Sprintf("Allocating free demod %s on input %s", demod.ID, input.ID),
				EvaluationMode: a.mode,
			}
		}
	}

	// 4. Overload / Capacity Exhaustion
	var failureReason string
	if len(occupiedDemods) >= len(a.topology.Demodulators) {
		failureReason = fmt.Sprintf("All %d hardware demodulators occupied", len(a.topology.Demodulators))
	} else {
		failureReason = "RF Plane conflict: no free input available for requested satellite polarization/band"
	}

	if a.mode == EvaluationModeEnforce {
		return AllocationDecision{
			Allowed:        false,
			Reason:         failureReason,
			EvaluationMode: a.mode,
		}
	}

	// Audit-Only / Fail-Open mode
	return AllocationDecision{
		Allowed:        true,
		Reason:         fmt.Sprintf("Audit-only override: %s (permitting stream)", failureReason),
		EvaluationMode: a.mode,
	}
}

// EvaluateWithUpcomingReservations simulates future recording reservations to ensure
// newly starting live streams do not starve upcoming scheduled DVR recordings.
func (a *Allocator) EvaluateWithUpcomingReservations(
	runtime *RuntimeAllocation,
	planner *ReservationPlanner,
	target MultiplexID,
	sessionID string,
	priority Priority,
	now time.Time,
) AllocationDecision {
	basicDecision := a.CanAllocate(runtime, target, sessionID)
	if !basicDecision.Allowed {
		return basicDecision
	}

	// Active/upcoming recordings and multiplex reuse have guaranteed priority
	if priority >= PriorityUpcomingRecording || basicDecision.ReusedDemod {
		return basicDecision
	}

	if planner == nil {
		return basicDecision
	}

	upcoming := planner.UpcomingReservations(now)
	if len(upcoming) == 0 {
		return basicDecision
	}

	// Simulate admitting this stream on a cloned runtime state
	simulatedRuntime := runtime.Clone()
	_, err := a.Allocate(simulatedRuntime, target, sessionID, AllocationOwnerXG2G)
	if err != nil {
		return basicDecision
	}

	// Ensure each upcoming recording can still be satisfied
	for _, res := range upcoming {
		resDecision := a.CanAllocate(simulatedRuntime, res.MultiplexID, "sim-"+res.ID)
		if !resDecision.Allowed {
			reason := fmt.Sprintf("Blocked: front-end capacity reserved for upcoming recording %q starting at %s (plane %s)",
				res.Title, res.StartTime.Format("15:04"), res.MultiplexID.String())
			if a.mode == EvaluationModeEnforce {
				return AllocationDecision{
					Allowed:        false,
					Reason:         reason,
					EvaluationMode: a.mode,
				}
			}
			return AllocationDecision{
				Allowed:        true,
				Reason:         fmt.Sprintf("Audit-only override: %s (permitting stream)", reason),
				EvaluationMode: a.mode,
			}
		}
		_, _ = a.Allocate(simulatedRuntime, res.MultiplexID, "sim-"+res.ID, AllocationOwnerXG2G)
	}

	return basicDecision
}

// Allocate commits an allocation if permitted by capacity constraints.
func (a *Allocator) Allocate(runtime *RuntimeAllocation, target MultiplexID, sessionID string, owner AllocationOwner) (AllocationDecision, error) {
	decision := a.CanAllocate(runtime, target, sessionID)
	if !decision.Allowed {
		return decision, fmt.Errorf("allocation rejected: %s", decision.Reason)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	// 1. If multiplex is already allocated, join the session to it
	for _, alloc := range runtime.ActiveMultiplexes {
		if alloc.MultiplexID.IsSamePhysicalMultiplex(target) {
			if !alloc.HasSession(sessionID) {
				alloc.SessionIDs = append(alloc.SessionIDs, sessionID)
			}
			return decision, nil
		}
	}

	// 2. Register new allocation on the chosen demodulator
	key := target.String()
	alloc := &DemodAllocation{
		DemodID:     decision.DemodID,
		InputID:     decision.InputID,
		MultiplexID: target,
		RFPlane:     target.RFPlane,
		Owner:       owner,
		SessionIDs:  []string{sessionID},
	}
	runtime.ActiveMultiplexes[key] = alloc

	// Update active input plane if DVB-S with plane
	if target.RFPlane != nil {
		runtime.ActiveInputPlanes[decision.InputID] = *target.RFPlane
	}

	return decision, nil
}

// Release removes a session from active allocations and frees the demodulator / RF plane if no sessions remain.
func (a *Allocator) Release(runtime *RuntimeAllocation, sessionID string) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}

	releasedAny := false
	for key, alloc := range runtime.ActiveMultiplexes {
		if alloc.RemoveSession(sessionID) {
			releasedAny = true
			if len(alloc.SessionIDs) == 0 {
				inputID := alloc.InputID
				delete(runtime.ActiveMultiplexes, key)

				// Check if any other multiplexes on this input still use the RF plane
				inputStillActive := false
				for _, remaining := range runtime.ActiveMultiplexes {
					if remaining.InputID == inputID {
						inputStillActive = true
						break
					}
				}
				if !inputStillActive {
					delete(runtime.ActiveInputPlanes, inputID)
				}
			}
		}
	}

	return releasedAny
}

func supportsDVBType(supported []DVBType, target DVBType) bool {
	if len(supported) == 0 {
		return true // Default unrestricted
	}
	for _, s := range supported {
		if s == target {
			return true
		}
	}
	return false
}
