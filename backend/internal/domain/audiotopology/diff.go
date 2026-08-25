package audiotopology

import (
	"fmt"
	"strings"
)

// TrackModification records detailed changes to an individual audio track identified by PID.
type TrackModification struct {
	PID          uint16     `json:"pid"`
	OldTrack     AudioTrack `json:"oldTrack"`
	NewTrack     AudioTrack `json:"newTrack"`
	IsStructural bool       `json:"isStructural"`
	IsMetadata   bool       `json:"isMetadata"`
	Reasons      []string   `json:"reasons"`
}

// TopologyChange captures the difference between two AudioTopology snapshots.
type TopologyChange struct {
	HasChange      bool                `json:"hasChange"`
	IsStructural   bool                `json:"isStructural"`
	IsMetadataOnly bool                `json:"isMetadataOnly"`
	AddedTracks    []AudioTrack        `json:"addedTracks,omitempty"`
	RemovedTracks  []AudioTrack        `json:"removedTracks,omitempty"`
	ModifiedTracks []TrackModification `json:"modifiedTracks,omitempty"`
	Summary        string              `json:"summary,omitempty"`
}

// DiffTopologies performs a pure domain comparison between old and new AudioTopology states.
// It matches tracks by PID and differentiates structural pipeline changes from metadata updates.
func DiffTopologies(oldTopo, newTopo AudioTopology) TopologyChange {
	if oldTopo.StructuralRevision == newTopo.StructuralRevision &&
		oldTopo.MetadataRevision == newTopo.MetadataRevision &&
		oldTopo.Presence == newTopo.Presence {
		return TopologyChange{HasChange: false}
	}

	oldMap := make(map[uint16]AudioTrack, len(oldTopo.Tracks))
	for _, t := range oldTopo.Tracks {
		oldMap[t.PID] = t
	}

	newMap := make(map[uint16]AudioTrack, len(newTopo.Tracks))
	for _, t := range newTopo.Tracks {
		newMap[t.PID] = t
	}

	var added []AudioTrack
	var removed []AudioTrack
	var modified []TrackModification
	var summaries []string
	isStructural := false

	// Check for Added or Modified tracks
	for pid, newTrack := range newMap {
		oldTrack, exists := oldMap[pid]
		if !exists {
			added = append(added, newTrack)
			isStructural = true
			summaries = append(summaries, fmt.Sprintf("added PID %d (%s %s)", pid, newTrack.Codec, newTrack.Label))
			continue
		}

		// Track exists in both: Check properties
		mod := diffTrack(oldTrack, newTrack)
		if mod.IsStructural || mod.IsMetadata {
			modified = append(modified, mod)
			if mod.IsStructural {
				isStructural = true
			}
			summaries = append(summaries, fmt.Sprintf("modified PID %d: %s", pid, strings.Join(mod.Reasons, ", ")))
		}
	}

	// Check for Removed tracks
	for pid, oldTrack := range oldMap {
		if _, exists := newMap[pid]; !exists {
			removed = append(removed, oldTrack)
			isStructural = true
			summaries = append(summaries, fmt.Sprintf("removed PID %d (%s %s)", pid, oldTrack.Codec, oldTrack.Label))
		}
	}

	if oldTopo.StructuralRevision != newTopo.StructuralRevision {
		isStructural = true
	}

	hasChange := len(added) > 0 || len(removed) > 0 || len(modified) > 0 ||
		oldTopo.MetadataRevision != newTopo.MetadataRevision ||
		oldTopo.Presence != newTopo.Presence

	return TopologyChange{
		HasChange:      hasChange,
		IsStructural:   isStructural,
		IsMetadataOnly: hasChange && !isStructural,
		AddedTracks:    added,
		RemovedTracks:  removed,
		ModifiedTracks: modified,
		Summary:        strings.Join(summaries, "; "),
	}
}

func diffTrack(oldTrack, newTrack AudioTrack) TrackModification {
	mod := TrackModification{
		PID:      newTrack.PID,
		OldTrack: oldTrack,
		NewTrack: newTrack,
	}

	// Structural dimensions: Codec, Channels
	if oldTrack.Codec != newTrack.Codec {
		mod.IsStructural = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("codec %s -> %s", oldTrack.Codec, newTrack.Codec))
	}
	if oldTrack.Channels != newTrack.Channels {
		mod.IsStructural = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("channels %d -> %d", oldTrack.Channels, newTrack.Channels))
	}

	// Metadata dimensions: Label, Language, Purpose, Accessibility, Selection, Evidence
	if oldTrack.Label != newTrack.Label {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("label %q -> %q", oldTrack.Label, newTrack.Label))
	}
	if oldTrack.Language.ISO639_2 != newTrack.Language.ISO639_2 {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("language %s -> %s", oldTrack.Language.ISO639_2, newTrack.Language.ISO639_2))
	}
	if oldTrack.Purpose != newTrack.Purpose {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("purpose %s -> %s", oldTrack.Purpose, newTrack.Purpose))
	}
	if oldTrack.Accessibility != newTrack.Accessibility {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, "accessibility updated")
	}
	if oldTrack.Confidence != newTrack.Confidence {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("confidence %s -> %s", oldTrack.Confidence, newTrack.Confidence))
	}
	if len(oldTrack.Evidence) != len(newTrack.Evidence) {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, "evidence updated")
	}
	if oldTrack.ReceiverSelected != newTrack.ReceiverSelected {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("receiverSelected %v -> %v", oldTrack.ReceiverSelected, newTrack.ReceiverSelected))
	}
	if oldTrack.BroadcastDefault != newTrack.BroadcastDefault {
		mod.IsMetadata = true
		mod.Reasons = append(mod.Reasons, fmt.Sprintf("broadcastDefault %v -> %v", oldTrack.BroadcastDefault, newTrack.BroadcastDefault))
	}

	return mod
}
