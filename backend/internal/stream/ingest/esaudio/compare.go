// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

// Compare names the fields two observations disagree on, in a fixed order, and
// returns nothing when they are identical.
//
// Exact, with no tolerance anywhere - Frames included. A frame count is not
// telemetry: a track whose count is zero is left out of the topology entirely,
// so the difference between 0 and 1 is a difference in what the product does,
// and a frame one implementation sees and the other does not is exactly the
// resync divergence a differential exists to find.
//
// Named fields rather than a bare inequality, because the answer to "these two
// disagree" is always "about what", and a live shadow that can only say "not
// equal" makes that question expensive at the worst moment.
func Compare(reference, other Observation) []string {
	var fields []string
	if reference.Channels != other.Channels {
		fields = append(fields, "channels")
	}
	if reference.LFE != other.LFE {
		fields = append(fields, "lfe")
	}
	if reference.Acmod != other.Acmod {
		fields = append(fields, "acmod")
	}
	if reference.HasAcmod != other.HasAcmod {
		fields = append(fields, "hasAcmod")
	}
	if reference.DependentSubstream != other.DependentSubstream {
		fields = append(fields, "dependentSubstream")
	}
	if reference.Frames != other.Frames {
		fields = append(fields, "frames")
	}
	return fields
}
