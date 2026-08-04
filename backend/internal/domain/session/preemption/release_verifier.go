// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package preemption

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ComputeObservationHash computes a deterministic SHA-256 hash over a ReleaseObservation dataset.
func ComputeObservationHash(obs ReleaseObservation) (string, error) {
	if strings.TrimSpace(obs.ReceiverID) == "" {
		return "", fmt.Errorf("empty receiver ID")
	}
	if strings.TrimSpace(obs.ObservationRevision) == "" {
		return "", fmt.Errorf("empty observation revision")
	}
	if strings.TrimSpace(obs.AllocationSourceRevision) == "" {
		return "", fmt.Errorf("empty allocation source revision")
	}
	if strings.TrimSpace(obs.HardwareSourceRevision) == "" {
		return "", fmt.Errorf("empty hardware source revision")
	}
	if strings.TrimSpace(obs.LeaseSourceRevision) == "" {
		return "", fmt.Errorf("empty lease source revision")
	}
	if strings.TrimSpace(obs.HardwareProfileRevision) == "" {
		return "", fmt.Errorf("empty hardware profile revision")
	}
	if obs.Evidence != EvidenceDirectObservation {
		return "", fmt.Errorf("invalid evidence classification '%s'", obs.Evidence)
	}
	if obs.ObservedAt.IsZero() {
		return "", fmt.Errorf("zero observedAt timestamp")
	}

	canonAllocIDs := make([]string, 0, len(obs.ActiveAllocations))
	seenAllocIDs := make(map[string]struct{}, len(obs.ActiveAllocations))
	for i, alloc := range obs.ActiveAllocations {
		id := strings.TrimSpace(alloc.AllocationID)
		if id == "" {
			return "", fmt.Errorf("empty allocation ID at index %d", i)
		}
		if _, dup := seenAllocIDs[id]; dup {
			return "", fmt.Errorf("duplicate active allocation ID '%s' at index %d", id, i)
		}
		seenAllocIDs[id] = struct{}{}
		canonAllocIDs = append(canonAllocIDs, fmt.Sprintf("%s:%s:%s", id, strings.TrimSpace(alloc.Owner), strings.TrimSpace(alloc.Revision)))
	}
	sort.Strings(canonAllocIDs)

	canonHwBindings := make([]string, 0, len(obs.ActiveHardwareBindings))
	seenHw := make(map[string]struct{}, len(obs.ActiveHardwareBindings))
	for i, b := range obs.ActiveHardwareBindings {
		if !b.Kind.IsValid() {
			return "", fmt.Errorf("invalid resource kind '%s' at hardware binding index %d", b.Kind, i)
		}
		if strings.TrimSpace(b.Resource) == "" {
			return "", fmt.Errorf("empty resource at hardware binding index %d", i)
		}
		if strings.TrimSpace(b.AllocationID) == "" {
			return "", fmt.Errorf("empty allocation ID at hardware binding index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at hardware binding index %d", b.Quantity, i)
		}
		item := fmt.Sprintf("%s:%s:%s:%d", b.Kind, strings.TrimSpace(b.Resource), strings.TrimSpace(b.AllocationID), b.Quantity)
		if _, dup := seenHw[item]; dup {
			return "", fmt.Errorf("duplicate identical hardware binding '%s' at index %d", item, i)
		}
		seenHw[item] = struct{}{}
		canonHwBindings = append(canonHwBindings, item)
	}
	sort.Strings(canonHwBindings)

	canonLeaseBindings := make([]string, 0, len(obs.ActiveLeaseBindings))
	seenLease := make(map[string]struct{}, len(obs.ActiveLeaseBindings))
	for i, b := range obs.ActiveLeaseBindings {
		if !b.LeaseKind.IsValid() {
			return "", fmt.Errorf("invalid lease kind '%s' at lease binding index %d", b.LeaseKind, i)
		}
		if strings.TrimSpace(b.Resource) == "" {
			return "", fmt.Errorf("empty resource at lease binding index %d", i)
		}
		if strings.TrimSpace(b.ScopeID) == "" {
			return "", fmt.Errorf("empty scope ID at lease binding index %d", i)
		}
		if strings.TrimSpace(b.OwnerID) == "" {
			return "", fmt.Errorf("empty owner ID at lease binding index %d", i)
		}
		if b.Quantity <= 0 {
			return "", fmt.Errorf("non-positive quantity %d at lease binding index %d", b.Quantity, i)
		}
		item := fmt.Sprintf("%s:%s:%s:%s:%d", b.LeaseKind, strings.TrimSpace(b.Resource), strings.TrimSpace(b.ScopeID), strings.TrimSpace(b.OwnerID), b.Quantity)
		if _, dup := seenLease[item]; dup {
			return "", fmt.Errorf("duplicate identical lease binding '%s' at index %d", item, i)
		}
		seenLease[item] = struct{}{}
		canonLeaseBindings = append(canonLeaseBindings, item)
	}
	sort.Strings(canonLeaseBindings)

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "receiver_id=%s\n", obs.ReceiverID)
	_, _ = fmt.Fprintf(h, "obs_rev=%s\n", obs.ObservationRevision)
	_, _ = fmt.Fprintf(h, "alloc_src_rev=%s\n", obs.AllocationSourceRevision)
	_, _ = fmt.Fprintf(h, "hw_src_rev=%s\n", obs.HardwareSourceRevision)
	_, _ = fmt.Fprintf(h, "lease_src_rev=%s\n", obs.LeaseSourceRevision)
	_, _ = fmt.Fprintf(h, "hw_profile_rev=%s\n", obs.HardwareProfileRevision)
	_, _ = fmt.Fprintf(h, "evidence=%s\n", obs.Evidence)
	_, _ = fmt.Fprintf(h, "coverage=%t:%t:%t\n", obs.Coverage.AllocationsComplete, obs.Coverage.HardwareBindingsComplete, obs.Coverage.LeaseBindingsComplete)
	_, _ = fmt.Fprintf(h, "observed_at=%d\n", obs.ObservedAt.UnixNano())
	_, _ = fmt.Fprintf(h, "allocs=%s\n", strings.Join(canonAllocIDs, ";"))
	_, _ = fmt.Fprintf(h, "hw_bindings=%s\n", strings.Join(canonHwBindings, ";"))
	_, _ = fmt.Fprintf(h, "lease_bindings=%s\n", strings.Join(canonLeaseBindings, ";"))

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeEvidenceHash computes a deterministic SHA-256 hash over a TargetTeardownEvidence dataset.
func ComputeEvidenceHash(ev TargetTeardownEvidence) (string, error) {
	if strings.TrimSpace(ev.SagaID) == "" {
		return "", fmt.Errorf("empty saga ID")
	}
	if strings.TrimSpace(ev.PreparedTeardownHash) == "" {
		return "", fmt.Errorf("empty prepared teardown hash")
	}
	if strings.TrimSpace(ev.TargetAllocationID) == "" {
		return "", fmt.Errorf("empty target allocation ID")
	}
	if strings.TrimSpace(ev.DescriptorHash) == "" {
		return "", fmt.Errorf("empty descriptor hash")
	}
	if ev.FencingToken == 0 {
		return "", fmt.Errorf("zero fencing token")
	}
	if !ev.Status.IsValid() {
		return "", fmt.Errorf("invalid teardown status '%s'", ev.Status)
	}
	if ev.AttemptedAt.IsZero() {
		return "", fmt.Errorf("zero attemptedAt timestamp")
	}
	if ev.AcknowledgedAt.IsZero() {
		return "", fmt.Errorf("zero acknowledgedAt timestamp")
	}
	if ev.AttemptedAt.After(ev.AcknowledgedAt) {
		return "", fmt.Errorf("attemptedAt %s > acknowledgedAt %s", ev.AttemptedAt.Format(time.RFC3339), ev.AcknowledgedAt.Format(time.RFC3339))
	}

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "saga_id=%s\n", ev.SagaID)
	_, _ = fmt.Fprintf(h, "prep_hash=%s\n", ev.PreparedTeardownHash)
	_, _ = fmt.Fprintf(h, "target_alloc_id=%s\n", ev.TargetAllocationID)
	_, _ = fmt.Fprintf(h, "desc_hash=%s\n", ev.DescriptorHash)
	_, _ = fmt.Fprintf(h, "fencing_token=%d\n", ev.FencingToken)
	_, _ = fmt.Fprintf(h, "status=%s\n", ev.Status)
	_, _ = fmt.Fprintf(h, "attempted_at=%d\n", ev.AttemptedAt.UnixNano())
	_, _ = fmt.Fprintf(h, "acknowledged_at=%d\n", ev.AcknowledgedAt.UnixNano())

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ResourceReleaseVerifier performs pure, read-only empirical release verification post-teardown.
type ResourceReleaseVerifier struct {
	MaxObservationAge  time.Duration
	ClockSkewTolerance time.Duration
}

// NewResourceReleaseVerifier creates a new ResourceReleaseVerifier instance.
func NewResourceReleaseVerifier() *ResourceReleaseVerifier {
	return &ResourceReleaseVerifier{
		MaxObservationAge:  5 * time.Second,
		ClockSkewTolerance: 1 * time.Second,
	}
}

// VerifyRelease performs pure domain verification of target resource release post-teardown.
func (v *ResourceReleaseVerifier) VerifyRelease(
	prepared *PreparedTeardown,
	teardownEvidence []TargetTeardownEvidence,
	observation ReleaseObservation,
	now time.Time,
) (ReleaseVerificationResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// 1. Validate prepared teardown dataset
	if err := ValidatePreparedTeardown(prepared, now); err != nil {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceMalformed,
		}, nil
	}

	// 2. Validate observation structure & coverage completeness
	if !observation.Coverage.AllocationsComplete || !observation.Coverage.HardwareBindingsComplete || !observation.Coverage.LeaseBindingsComplete {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceIncomplete,
		}, nil
	}

	computedObsHash, err := ComputeObservationHash(observation)
	if err != nil {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceMalformed,
		}, nil
	}
	if observation.ObservationHash != computedObsHash {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceMalformed,
		}, nil
	}

	// 3. Receiver ID & Hardware Profile Revision matching
	if observation.ReceiverID != prepared.ReceiverID {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonRevisionMismatch,
		}, nil
	}
	if observation.HardwareProfileRevision != prepared.HardwareProfileRevision {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonRevisionMismatch,
		}, nil
	}

	// 4. Validate teardown evidence 1:1 bijection
	if len(teardownEvidence) != len(prepared.TargetDescriptors) {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonBijectionInvalid,
		}, nil
	}

	descMap := make(map[string]TargetExecutionDescriptor, len(prepared.TargetDescriptors))
	for _, desc := range prepared.TargetDescriptors {
		descMap[desc.AllocationID] = desc
	}

	evidenceMap := make(map[string]TargetTeardownEvidence, len(teardownEvidence))
	var commonSagaID string

	for i, ev := range teardownEvidence {
		compEvHash, evErr := ComputeEvidenceHash(ev)
		if evErr != nil || ev.EvidenceHash != compEvHash {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonEvidenceMalformed,
			}, nil
		}

		if ev.Status != TeardownStatusStopConfirmed && ev.Status != TeardownStatusAlreadyStopped {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonTargetsNotReleased,
			}, nil
		}

		if ev.PreparedTeardownHash != prepared.PreparedTeardownHash {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonBijectionInvalid,
			}, nil
		}

		desc, exists := descMap[ev.TargetAllocationID]
		if !exists || desc.DescriptorHash != ev.DescriptorHash {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonBijectionInvalid,
			}, nil
		}

		if i == 0 {
			commonSagaID = ev.SagaID
		} else if ev.SagaID != commonSagaID {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonBijectionInvalid,
			}, nil
		}

		if _, duplicate := evidenceMap[ev.TargetAllocationID]; duplicate {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonBijectionInvalid,
			}, nil
		}
		evidenceMap[ev.TargetAllocationID] = ev
	}

	// 5. Temporal ordering & observation age validation
	maxAge := v.MaxObservationAge
	if maxAge == 0 {
		maxAge = 5 * time.Second
	}
	skew := v.ClockSkewTolerance
	if skew == 0 {
		skew = 1 * time.Second
	}

	for _, ev := range teardownEvidence {
		if observation.ObservedAt.Before(ev.AcknowledgedAt) {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonEvidenceStale,
			}, nil
		}
	}

	if observation.ObservedAt.After(now.Add(skew)) {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceStale,
		}, nil
	}

	if now.Sub(observation.ObservedAt) > maxAge {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonEvidenceStale,
		}, nil
	}

	// 6. Build quick lookup sets for active allocations & bindings
	activeAllocMap := make(map[string]ActiveAllocation, len(observation.ActiveAllocations))
	for _, alloc := range observation.ActiveAllocations {
		activeAllocMap[alloc.AllocationID] = alloc
	}

	activeHwBindingsByAlloc := make(map[string][]ObservedResourceBinding, len(observation.ActiveHardwareBindings))
	allHwBindingsByResource := make(map[string][]ObservedResourceBinding, len(observation.ActiveHardwareBindings))
	for _, b := range observation.ActiveHardwareBindings {
		activeHwBindingsByAlloc[b.AllocationID] = append(activeHwBindingsByAlloc[b.AllocationID], b)
		key := fmt.Sprintf("%s:%s", b.Kind, b.Resource)
		allHwBindingsByResource[key] = append(allHwBindingsByResource[key], b)
	}

	activeLeaseBindingsByOwner := make(map[string][]ObservedLeaseBinding, len(observation.ActiveLeaseBindings))
	allLeaseBindingsByResource := make(map[string][]ObservedLeaseBinding, len(observation.ActiveLeaseBindings))
	for _, b := range observation.ActiveLeaseBindings {
		activeLeaseBindingsByOwner[b.OwnerID] = append(activeLeaseBindingsByOwner[b.OwnerID], b)
		key := fmt.Sprintf("%s:%s:%s", b.LeaseKind, b.Resource, b.ScopeID)
		allLeaseBindingsByResource[key] = append(allLeaseBindingsByResource[key], b)
	}

	// 7. Evaluate each target allocation
	targetResults := make([]SingleTargetReleaseResult, 0, len(prepared.TargetDescriptors))
	allReleased := true

	var releasedClaims []ResourceClaim
	var freeClaims []ResourceClaim
	reassignedHwMap := make(map[string]ObservedResourceBinding)
	reassignedLeaseMap := make(map[string]ObservedLeaseBinding)

	for _, desc := range prepared.TargetDescriptors {
		targetID := desc.AllocationID
		targetRes := SingleTargetReleaseResult{
			TargetAllocationID: targetID,
			ReleaseState:       TargetReleased,
			Reason:             ReleaseReasonNone,
		}
		var resourceResults []ResourceReleaseResult

		// A. Check if target allocation is still active in snapshot
		if _, active := activeAllocMap[targetID]; active {
			targetRes.ReleaseState = TargetStillActive
			targetRes.Reason = ReleaseReasonTargetStillObserved
			allReleased = false
		}

		// B. Check if target hardware bindings still remain
		if hwBindings, bound := activeHwBindingsByAlloc[targetID]; bound && len(hwBindings) > 0 {
			targetRes.ReleaseState = TargetBindingRemains
			targetRes.Reason = ReleaseReasonHardwareBinding
			allReleased = false
		}

		// C. Check if target lease bindings still remain (matching targetID, desc.ExpectedOwner, OR eb.ExpectedOwnerID)
		if leaseBindings, bound := activeLeaseBindingsByOwner[targetID]; bound && len(leaseBindings) > 0 {
			targetRes.ReleaseState = TargetBindingRemains
			targetRes.Reason = ReleaseReasonLeaseBinding
			allReleased = false
		}
		if ownerLeases, bound := activeLeaseBindingsByOwner[desc.ExpectedOwner]; bound && len(ownerLeases) > 0 {
			targetRes.ReleaseState = TargetBindingRemains
			targetRes.Reason = ReleaseReasonLeaseBinding
			allReleased = false
		}
		for _, eb := range desc.ExpectedLeaseBindings {
			if strings.TrimSpace(eb.ExpectedOwnerID) != "" {
				if ownerLeases, bound := activeLeaseBindingsByOwner[eb.ExpectedOwnerID]; bound && len(ownerLeases) > 0 {
					for _, ob := range ownerLeases {
						if ob.LeaseKind == eb.LeaseKind && ob.Resource == eb.Resource && ob.ScopeID == eb.ScopeID {
							targetRes.ReleaseState = TargetBindingRemains
							targetRes.Reason = ReleaseReasonLeaseBinding
							allReleased = false
							break
						}
					}
				}
			}
		}

		// D. Process ExpectedClaims (Non-Hardware claims like TUNER_SLOT, RESTRICTED_ACCESS_SLOT, STORAGE_IO)
		for _, claim := range desc.ExpectedClaims {
			releasedClaims = append(releasedClaims, claim)
			key := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)

			// Check if claim is still held by target allocation in activeAllocMap
			if targetAlloc, active := activeAllocMap[targetID]; active {
				for _, ac := range targetAlloc.Claims {
					if ac.Kind == claim.Kind && ac.Resource == claim.Resource {
						targetRes.ReleaseState = TargetStillActive
						targetRes.Reason = ReleaseReasonTargetStillObserved
						allReleased = false
						break
					}
				}
			}

			// Determine disposition cleanly (avoiding contradictory CURRENTLY_FREE if target binding remains!)
			if targetRes.ReleaseState != TargetReleased {
				resourceResults = append(resourceResults, ResourceReleaseResult{
					Resource:    claim,
					Disposition: ResourceUnknown,
				})
			} else {
				// Check if reassigned to another active allocation
				reassigned := false
				for otherID, otherAlloc := range activeAllocMap {
					if otherID != targetID {
						for _, ac := range otherAlloc.Claims {
							if ac.Kind == claim.Kind && ac.Resource == claim.Resource {
								reassigned = true
								break
							}
						}
					}
				}
				disp := ResourceCurrentlyFree
				if reassigned {
					disp = ResourceReassigned
				} else {
					freeClaims = append(freeClaims, claim)
				}
				resourceResults = append(resourceResults, ResourceReleaseResult{
					Resource:    claim,
					Disposition: disp,
				})
			}
			_ = key
		}

		// E. Process ExpectedHardwareClaims & determine disposition
		for _, hwClaim := range desc.ExpectedHardwareClaims {
			releasedClaims = append(releasedClaims, hwClaim)
			key := fmt.Sprintf("%s:%s", hwClaim.Kind, hwClaim.Resource)

			// If target is NOT released, disposition MUST be ResourceUnknown (not contradictory CURRENTLY_FREE!)
			if targetRes.ReleaseState != TargetReleased {
				resourceResults = append(resourceResults, ResourceReleaseResult{
					Resource:    hwClaim,
					Disposition: ResourceUnknown,
				})
				continue
			}

			var reassignedBinding *ObservedResourceBinding
			if bindings, exists := allHwBindingsByResource[key]; exists {
				for _, b := range bindings {
					if b.AllocationID != targetID {
						reassignedBinding = &b
						reassignedHwMap[fmt.Sprintf("%s:%s:%s", b.Kind, b.Resource, b.AllocationID)] = b
						break
					}
				}
			}

			disp := ResourceCurrentlyFree
			if reassignedBinding != nil {
				disp = ResourceReassigned
			} else {
				freeClaims = append(freeClaims, hwClaim)
			}

			resourceResults = append(resourceResults, ResourceReleaseResult{
				Resource:    hwClaim,
				Disposition: disp,
			})
		}

		// F. Process ExpectedLeaseBindings & determine disposition
		for _, eb := range desc.ExpectedLeaseBindings {
			claim := ResourceClaim{Kind: eb.LeaseKind, Resource: eb.Resource, Quantity: eb.Quantity}
			releasedClaims = append(releasedClaims, claim)
			key := fmt.Sprintf("%s:%s:%s", eb.LeaseKind, eb.Resource, eb.ScopeID)

			// If target is NOT released, disposition MUST be ResourceUnknown
			if targetRes.ReleaseState != TargetReleased {
				resourceResults = append(resourceResults, ResourceReleaseResult{
					Resource:    claim,
					Disposition: ResourceUnknown,
				})
				continue
			}

			var reassignedLease *ObservedLeaseBinding
			if bindings, exists := allLeaseBindingsByResource[key]; exists {
				for _, b := range bindings {
					if b.OwnerID != targetID && b.OwnerID != desc.ExpectedOwner && (eb.ExpectedOwnerID == "" || b.OwnerID != eb.ExpectedOwnerID) {
						reassignedLease = &b
						reassignedLeaseMap[fmt.Sprintf("%s:%s:%s:%s", b.LeaseKind, b.Resource, b.ScopeID, b.OwnerID)] = b
						break
					}
				}
			}

			disp := ResourceCurrentlyFree
			if reassignedLease != nil {
				disp = ResourceReassigned
			} else {
				freeClaims = append(freeClaims, claim)
			}

			resourceResults = append(resourceResults, ResourceReleaseResult{
				Resource:    claim,
				Disposition: disp,
			})
		}

		targetRes.Resources = resourceResults
		targetResults = append(targetResults, targetRes)
	}

	if !allReleased {
		return ReleaseVerificationResult{
			Decision:      ReleaseDecisionRejected,
			Reason:        ReleaseVerificationReasonTargetsNotReleased,
			TargetResults: targetResults,
		}, nil
	}

	reassignedHwList := make([]ObservedResourceBinding, 0, len(reassignedHwMap))
	for _, b := range reassignedHwMap {
		reassignedHwList = append(reassignedHwList, b)
	}
	sort.Slice(reassignedHwList, func(i, j int) bool {
		return reassignedHwList[i].Resource < reassignedHwList[j].Resource
	})

	reassignedLeaseList := make([]ObservedLeaseBinding, 0, len(reassignedLeaseMap))
	for _, b := range reassignedLeaseMap {
		reassignedLeaseList = append(reassignedLeaseList, b)
	}
	sort.Slice(reassignedLeaseList, func(i, j int) bool {
		return reassignedLeaseList[i].Resource < reassignedLeaseList[j].Resource
	})

	return ReleaseVerificationResult{
		Decision:                   ReleaseDecisionReleased,
		Reason:                     ReleaseVerificationReasonNone,
		TargetResults:              targetResults,
		ReleasedFromTargetClaims:   releasedClaims,
		CurrentlyFreeClaims:        freeClaims,
		ReassignedHardwareBindings: reassignedHwList,
		ReassignedLeaseBindings:    reassignedLeaseList,
		ObservationRevision:        observation.ObservationRevision,
		VerifiedAt:                 now,
	}, nil
}
