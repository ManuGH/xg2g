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
		owner := strings.TrimSpace(alloc.Owner)
		if owner == "" {
			return "", fmt.Errorf("empty owner for allocation '%s' at index %d", id, i)
		}
		rev := strings.TrimSpace(alloc.Revision)
		if rev == "" {
			return "", fmt.Errorf("empty revision for allocation '%s' at index %d", id, i)
		}
		if _, dup := seenAllocIDs[id]; dup {
			return "", fmt.Errorf("duplicate active allocation ID '%s' at index %d", id, i)
		}
		seenAllocIDs[id] = struct{}{}

		canonClaims, err := formatCanonicalClaimsStrict(alloc.Claims)
		if err != nil {
			return "", fmt.Errorf("invalid claims for active allocation '%s': %w", id, err)
		}
		canonAllocIDs = append(canonAllocIDs, fmt.Sprintf("%s:%s:%s:%d:%t:%t:%s", id, owner, rev, alloc.Priority.BasePriority, alloc.Priority.Foreground, alloc.Priority.UserProtected, canonClaims))
	}
	sort.Strings(canonAllocIDs)

	hwQtyMap := make(map[string]int, len(obs.ActiveHardwareBindings))
	for i, b := range obs.ActiveHardwareBindings {
		if !b.Kind.IsValid() {
			return "", fmt.Errorf("invalid resource kind '%s' at hardware binding index %d", b.Kind, i)
		}
		res := strings.TrimSpace(b.Resource)
		if res == "" {
			return "", fmt.Errorf("empty resource at hardware binding index %d", i)
		}
		allocID := strings.TrimSpace(b.AllocationID)
		if allocID == "" {
			return "", fmt.Errorf("empty allocation ID at hardware binding index %d", i)
		}
		if b.Quantity <= 0 || b.Quantity > MaxClaimQuantitySum {
			return "", fmt.Errorf("invalid quantity %d at hardware binding index %d", b.Quantity, i)
		}
		key := fmt.Sprintf("%s:%s:%s", b.Kind, res, allocID)
		if hwQtyMap[key] > MaxClaimQuantitySum-b.Quantity {
			return "", fmt.Errorf("quantity sum overflow for hardware binding '%s' at index %d", key, i)
		}
		hwQtyMap[key] += b.Quantity
	}
	hwKeys := make([]string, 0, len(hwQtyMap))
	for k := range hwQtyMap {
		hwKeys = append(hwKeys, k)
	}
	sort.Strings(hwKeys)
	canonHwBindings := make([]string, 0, len(hwKeys))
	for _, k := range hwKeys {
		canonHwBindings = append(canonHwBindings, fmt.Sprintf("%s:%d", k, hwQtyMap[k]))
	}

	leaseQtyMap := make(map[string]int, len(obs.ActiveLeaseBindings))
	for i, b := range obs.ActiveLeaseBindings {
		if !b.LeaseKind.IsValid() {
			return "", fmt.Errorf("invalid lease kind '%s' at lease binding index %d", b.LeaseKind, i)
		}
		res := strings.TrimSpace(b.Resource)
		if res == "" {
			return "", fmt.Errorf("empty resource at lease binding index %d", i)
		}
		scopeID := strings.TrimSpace(b.ScopeID)
		if scopeID == "" {
			return "", fmt.Errorf("empty scope ID at lease binding index %d", i)
		}
		ownerID := strings.TrimSpace(b.OwnerID)
		if ownerID == "" {
			return "", fmt.Errorf("empty owner ID at lease binding index %d", i)
		}
		if b.Quantity <= 0 || b.Quantity > MaxClaimQuantitySum {
			return "", fmt.Errorf("invalid quantity %d at lease binding index %d", b.Quantity, i)
		}
		key := fmt.Sprintf("%s:%s:%s:%s", b.LeaseKind, res, scopeID, ownerID)
		if leaseQtyMap[key] > MaxClaimQuantitySum-b.Quantity {
			return "", fmt.Errorf("quantity sum overflow for lease binding '%s' at index %d", key, i)
		}
		leaseQtyMap[key] += b.Quantity
	}
	leaseKeys := make([]string, 0, len(leaseQtyMap))
	for k := range leaseQtyMap {
		leaseKeys = append(leaseKeys, k)
	}
	sort.Strings(leaseKeys)
	canonLeaseBindings := make([]string, 0, len(leaseKeys))
	for _, k := range leaseKeys {
		canonLeaseBindings = append(canonLeaseBindings, fmt.Sprintf("%s:%d", k, leaseQtyMap[k]))
	}

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

	// 4. Validate teardown evidence 1:1 bijection and fencing token consistency
	if len(teardownEvidence) != len(prepared.TargetDescriptors) {
		return ReleaseVerificationResult{
			Decision: ReleaseDecisionRejected,
			Reason:   ReleaseVerificationReasonBijectionInvalid,
		}, nil
	}

	descMap := make(map[string]TargetExecutionDescriptor, len(prepared.TargetDescriptors))
	for _, desc := range prepared.TargetDescriptors {
		descMap[desc.AllocationID] = desc

		// FAIL-CLOSED: Check if target requires hardware/lease bindings, but binding coverage is not authoritative!
		hasHwClaim := len(desc.ExpectedHardwareClaims) > 0
		hasLeaseClaim := false
		for _, c := range desc.ExpectedClaims {
			if c.Kind == ResourceKindRestrictedAccessSlot || c.Kind == ResourceKindTunerSlot {
				hasLeaseClaim = true
				break
			}
		}

		if hasHwClaim && !desc.Coverage.HardwareBindingsAuthoritative {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonEvidenceIncomplete,
			}, nil
		}
		if hasLeaseClaim && !desc.Coverage.LeaseBindingsAuthoritative {
			return ReleaseVerificationResult{
				Decision: ReleaseDecisionRejected,
				Reason:   ReleaseVerificationReasonEvidenceIncomplete,
			}, nil
		}
	}

	evidenceMap := make(map[string]TargetTeardownEvidence, len(teardownEvidence))
	var commonSagaID string
	var commonFencingToken uint64

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
			commonFencingToken = ev.FencingToken
		} else {
			if ev.SagaID != commonSagaID || ev.FencingToken != commonFencingToken {
				return ReleaseVerificationResult{
					Decision: ReleaseDecisionRejected,
					Reason:   ReleaseVerificationReasonBijectionInvalid,
				}, nil
			}
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

	// 6. Build quantity-aggregated lookup maps from observed data
	activeAllocMap := make(map[string]ActiveAllocation, len(observation.ActiveAllocations))
	for _, alloc := range observation.ActiveAllocations {
		activeAllocMap[alloc.AllocationID] = alloc
	}

	activeHwQtyMap := make(map[string]int, len(observation.ActiveHardwareBindings))
	allHwQtyMap := make(map[string]map[string]int)
	for _, b := range observation.ActiveHardwareBindings {
		key := fmt.Sprintf("%s:%s:%s", b.Kind, b.Resource, b.AllocationID)
		activeHwQtyMap[key] += b.Quantity

		resKey := fmt.Sprintf("%s:%s", b.Kind, b.Resource)
		if allHwQtyMap[resKey] == nil {
			allHwQtyMap[resKey] = make(map[string]int)
		}
		allHwQtyMap[resKey][b.AllocationID] += b.Quantity
	}

	activeLeaseQtyMap := make(map[string]int, len(observation.ActiveLeaseBindings))
	allLeaseQtyMap := make(map[string]map[string]int)
	for _, b := range observation.ActiveLeaseBindings {
		key := fmt.Sprintf("%s:%s:%s:%s", b.LeaseKind, b.Resource, b.ScopeID, b.OwnerID)
		activeLeaseQtyMap[key] += b.Quantity

		resKey := fmt.Sprintf("%s:%s:%s", b.LeaseKind, b.Resource, b.ScopeID)
		if allLeaseQtyMap[resKey] == nil {
			allLeaseQtyMap[resKey] = make(map[string]int)
		}
		allLeaseQtyMap[resKey][b.OwnerID] += b.Quantity
	}

	targetResults := make([]SingleTargetReleaseResult, 0, len(prepared.TargetDescriptors))
	allReleased := true

	rawReleasedClaims := make(map[string]int)
	rawFreeClaims := make(map[string]int)
	reassignedHwMap := make(map[string]ObservedResourceBinding)
	reassignedLeaseMap := make(map[string]ObservedLeaseBinding)

	// 7. Evaluate each target allocation strictly against expected bindings
	for _, desc := range prepared.TargetDescriptors {
		targetID := desc.AllocationID
		targetRes := SingleTargetReleaseResult{
			TargetAllocationID: targetID,
			ReleaseState:       TargetReleased,
			Reason:             ReleaseReasonNone,
		}
		var resourceResults []ResourceReleaseResult

		if _, active := activeAllocMap[targetID]; active {
			targetRes.ReleaseState = TargetStillActive
			targetRes.Reason = ReleaseReasonTargetStillObserved
			allReleased = false
		}

		for _, ehb := range desc.ExpectedHardwareBindings {
			key := fmt.Sprintf("%s:%s:%s", ehb.Kind, ehb.Resource, ehb.AllocationID)
			if remainingQty := activeHwQtyMap[key]; remainingQty > 0 {
				targetRes.ReleaseState = TargetBindingRemains
				targetRes.Reason = ReleaseReasonHardwareBinding
				allReleased = false
			}
		}

		for _, elb := range desc.ExpectedLeaseBindings {
			key := fmt.Sprintf("%s:%s:%s:%s", elb.LeaseKind, elb.Resource, elb.ScopeID, elb.ExpectedOwnerID)
			if remainingQty := activeLeaseQtyMap[key]; remainingQty > 0 {
				targetRes.ReleaseState = TargetBindingRemains
				targetRes.Reason = ReleaseReasonLeaseBinding
				allReleased = false
			}
		}

		for _, claim := range desc.ExpectedClaims {
			alreadyInLeases := false
			for _, eb := range desc.ExpectedLeaseBindings {
				if eb.LeaseKind == claim.Kind && eb.Resource == claim.Resource {
					alreadyInLeases = true
					break
				}
			}
			if alreadyInLeases {
				continue
			}

			expectedQty := claim.Quantity
			remainingTargetQty := 0
			if targetAlloc, active := activeAllocMap[targetID]; active {
				for _, ac := range targetAlloc.Claims {
					if ac.Kind == claim.Kind && ac.Resource == claim.Resource {
						remainingTargetQty += ac.Quantity
					}
				}
			}

			releasedQty := expectedQty - remainingTargetQty
			if releasedQty < 0 {
				releasedQty = 0
			}

			otherObservedQty := 0
			for otherID, otherAlloc := range activeAllocMap {
				if otherID != targetID {
					for _, ac := range otherAlloc.Claims {
						if ac.Kind == claim.Kind && ac.Resource == claim.Resource {
							otherObservedQty += ac.Quantity
						}
					}
				}
			}

			disp := ResourceCurrentlyFree
			if targetRes.ReleaseState != TargetReleased || remainingTargetQty > 0 {
				disp = ResourceUnknown
			} else if otherObservedQty > 0 {
				disp = ResourceOtherObserved
			}

			if releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
				rawReleasedClaims[claimKey] += releasedQty
			}
			if disp == ResourceCurrentlyFree && releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
				rawFreeClaims[claimKey] += releasedQty
			}

			resourceResults = append(resourceResults, ResourceReleaseResult{
				Resource:                   claim,
				ExpectedQuantity:           expectedQty,
				RemainingTargetQuantity:    remainingTargetQty,
				ReleasedFromTargetQuantity: releasedQty,
				OtherObservedQuantity:      otherObservedQty,
				CurrentlyFreeQuantity:      0, // Not authoritative without capacity source
				CurrentlyFreeAuthoritative: false,
				Disposition:                disp,
			})
		}

		for _, hwClaim := range desc.ExpectedHardwareClaims {
			expectedQty := hwClaim.Quantity
			key := fmt.Sprintf("%s:%s", hwClaim.Kind, hwClaim.Resource)

			targetKey := fmt.Sprintf("%s:%s:%s", hwClaim.Kind, hwClaim.Resource, targetID)
			remainingTargetQty := activeHwQtyMap[targetKey]

			releasedQty := expectedQty - remainingTargetQty
			if releasedQty < 0 {
				releasedQty = 0
			}

			otherObservedQty := 0
			if allocMap, exists := allHwQtyMap[key]; exists {
				for allocID, qty := range allocMap {
					if allocID != targetID {
						otherObservedQty += qty
						reassignedHwMap[fmt.Sprintf("%s:%s:%s:%d", hwClaim.Kind, hwClaim.Resource, allocID, qty)] = ObservedResourceBinding{
							Kind:         hwClaim.Kind,
							Resource:     hwClaim.Resource,
							Quantity:     qty,
							AllocationID: allocID,
						}
					}
				}
			}

			disp := ResourceCurrentlyFree
			if targetRes.ReleaseState != TargetReleased || remainingTargetQty > 0 {
				disp = ResourceUnknown
			} else if otherObservedQty > 0 {
				disp = ResourceOtherObserved
			}

			if releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", hwClaim.Kind, hwClaim.Resource)
				rawReleasedClaims[claimKey] += releasedQty
			}
			if disp == ResourceCurrentlyFree && releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", hwClaim.Kind, hwClaim.Resource)
				rawFreeClaims[claimKey] += releasedQty
			}

			resourceResults = append(resourceResults, ResourceReleaseResult{
				Resource:                   hwClaim,
				ExpectedQuantity:           expectedQty,
				RemainingTargetQuantity:    remainingTargetQty,
				ReleasedFromTargetQuantity: releasedQty,
				OtherObservedQuantity:      otherObservedQty,
				CurrentlyFreeQuantity:      0, // Not authoritative without capacity source
				CurrentlyFreeAuthoritative: false,
				Disposition:                disp,
			})
		}

		for _, eb := range desc.ExpectedLeaseBindings {
			expectedQty := eb.Quantity
			resKey := fmt.Sprintf("%s:%s:%s", eb.LeaseKind, eb.Resource, eb.ScopeID)
			targetKey := fmt.Sprintf("%s:%s:%s:%s", eb.LeaseKind, eb.Resource, eb.ScopeID, eb.ExpectedOwnerID)

			remainingTargetQty := activeLeaseQtyMap[targetKey]

			releasedQty := expectedQty - remainingTargetQty
			if releasedQty < 0 {
				releasedQty = 0
			}

			otherObservedQty := 0
			if ownerMap, exists := allLeaseQtyMap[resKey]; exists {
				for ownerID, qty := range ownerMap {
					if ownerID != eb.ExpectedOwnerID && ownerID != targetID {
						otherObservedQty += qty
						reassignedLeaseMap[fmt.Sprintf("%s:%s:%s:%s:%d", eb.LeaseKind, eb.Resource, eb.ScopeID, ownerID, qty)] = ObservedLeaseBinding{
							LeaseKind: eb.LeaseKind,
							Resource:  eb.Resource,
							ScopeID:   eb.ScopeID,
							OwnerID:   ownerID,
							Quantity:  qty,
						}
					}
				}
			}

			disp := ResourceCurrentlyFree
			if targetRes.ReleaseState != TargetReleased || remainingTargetQty > 0 {
				disp = ResourceUnknown
			} else if otherObservedQty > 0 {
				disp = ResourceOtherObserved
			}

			claim := ResourceClaim{Kind: eb.LeaseKind, Resource: eb.Resource, Quantity: expectedQty}
			if releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
				rawReleasedClaims[claimKey] += releasedQty
			}
			if disp == ResourceCurrentlyFree && releasedQty > 0 {
				claimKey := fmt.Sprintf("%s:%s", claim.Kind, claim.Resource)
				rawFreeClaims[claimKey] += releasedQty
			}

			resourceResults = append(resourceResults, ResourceReleaseResult{
				Resource:                   claim,
				ExpectedQuantity:           expectedQty,
				RemainingTargetQuantity:    remainingTargetQty,
				ReleasedFromTargetQuantity: releasedQty,
				OtherObservedQuantity:      otherObservedQty,
				CurrentlyFreeQuantity:      0, // Not authoritative without capacity source
				CurrentlyFreeAuthoritative: false,
				Disposition:                disp,
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

	// 8. Aggregate output claims, check overflow, and sort deterministically
	releasedList := make([]ResourceClaim, 0, len(rawReleasedClaims))
	for k, qty := range rawReleasedClaims {
		parts := strings.SplitN(k, ":", 2)
		releasedList = append(releasedList, ResourceClaim{
			Kind:     ResourceKind(parts[0]),
			Resource: parts[1],
			Quantity: qty,
		})
	}
	sort.Slice(releasedList, func(i, j int) bool {
		if releasedList[i].Kind != releasedList[j].Kind {
			return releasedList[i].Kind < releasedList[j].Kind
		}
		return releasedList[i].Resource < releasedList[j].Resource
	})

	freeList := make([]ResourceClaim, 0, len(rawFreeClaims))
	for k, qty := range rawFreeClaims {
		parts := strings.SplitN(k, ":", 2)
		freeList = append(freeList, ResourceClaim{
			Kind:     ResourceKind(parts[0]),
			Resource: parts[1],
			Quantity: qty,
		})
	}
	sort.Slice(freeList, func(i, j int) bool {
		if freeList[i].Kind != freeList[j].Kind {
			return freeList[i].Kind < freeList[j].Kind
		}
		return freeList[i].Resource < freeList[j].Resource
	})

	reassignedHwList := make([]ObservedResourceBinding, 0, len(reassignedHwMap))
	for _, b := range reassignedHwMap {
		reassignedHwList = append(reassignedHwList, b)
	}
	sort.Slice(reassignedHwList, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%s:%s:%d", reassignedHwList[i].Kind, reassignedHwList[i].Resource, reassignedHwList[i].AllocationID, reassignedHwList[i].Quantity)
		keyJ := fmt.Sprintf("%s:%s:%s:%d", reassignedHwList[j].Kind, reassignedHwList[j].Resource, reassignedHwList[j].AllocationID, reassignedHwList[j].Quantity)
		return keyI < keyJ
	})

	reassignedLeaseList := make([]ObservedLeaseBinding, 0, len(reassignedLeaseMap))
	for _, b := range reassignedLeaseMap {
		reassignedLeaseList = append(reassignedLeaseList, b)
	}
	sort.Slice(reassignedLeaseList, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%s:%s:%s:%d", reassignedLeaseList[i].LeaseKind, reassignedLeaseList[i].Resource, reassignedLeaseList[i].ScopeID, reassignedLeaseList[i].OwnerID, reassignedLeaseList[i].Quantity)
		keyJ := fmt.Sprintf("%s:%s:%s:%s:%d", reassignedLeaseList[j].LeaseKind, reassignedLeaseList[j].Resource, reassignedLeaseList[j].ScopeID, reassignedLeaseList[j].OwnerID, reassignedLeaseList[j].Quantity)
		return keyI < keyJ
	})

	return ReleaseVerificationResult{
		Decision:                   ReleaseDecisionReleased,
		Reason:                     ReleaseVerificationReasonNone,
		TargetResults:              targetResults,
		ReleasedFromTargetClaims:   releasedList,
		CurrentlyFreeClaims:        freeList,
		ReassignedHardwareBindings: reassignedHwList,
		ReassignedLeaseBindings:    reassignedLeaseList,
		ObservationRevision:        observation.ObservationRevision,
		VerifiedAt:                 now,
	}, nil
}
