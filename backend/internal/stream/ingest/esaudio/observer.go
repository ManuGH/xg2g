// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package esaudio

// stableFrames is how many consecutive frames must agree before the observation
// changes.
//
// A syncword is two bytes and audio payload contains them by chance, so a single
// parse is a guess. Requiring a run costs about a tenth of a second at the ~31
// frames per second an AC-3 stream runs at - fast enough to follow a real change
// between programmes, slow enough that a stray match cannot move the fact. A run
// broken by noise simply restarts; the previous observation stands until a new
// one is actually established, so the failure direction is "keeps saying what it
// last proved", never "invents something new".
const stableFrames = 3

// carryBytes is how much tail is kept for the next chunk. A syncframe header
// split across two chunks is completed from it; anything longer cannot be the
// start of an unexamined header.
const carryBytes = headerBytes - 1

// Observation is what an elementary stream has been seen to carry.
//
// The zero value means nothing has been established, which is a different state
// from stereo. Nothing here fills that in - a stream that has not said how many
// channels it has does not have two.
type Observation struct {
	// Channels counts full-bandwidth channels plus LFE, or 0 when no layout has
	// been established.
	Channels int `json:"channels,omitempty"`

	// LFE reports the low frequency effects channel, which is included in
	// Channels and named separately because a layout decision may want it.
	LFE bool `json:"lfe,omitempty"`

	// Acmod is the raw audio coding mode the count was derived from, carried so a
	// consumer can reach layout conclusions this type does not draw.
	Acmod    uint8 `json:"acmod,omitempty"`
	HasAcmod bool  `json:"hasAcmod,omitempty"`

	// DependentSubstream reports that E-AC-3 dependent substream frames were seen
	// on this stream. Channels then describes the independent substream only, and
	// the programme may carry more than it says.
	DependentSubstream bool `json:"dependentSubstream,omitempty"`

	// Frames counts the syncframes parsed since the observation started, so a
	// consumer can tell a settled reading from a stream that has barely arrived.
	Frames uint64 `json:"frames,omitempty"`
}

// Known reports whether the stream has said how many channels it carries.
func (o Observation) Known() bool { return o.Channels > 0 }

// sameLayout compares only the fields a change has to be proved for.
func (o Observation) sameLayout(other Observation) bool {
	return o.Channels == other.Channels && o.LFE == other.LFE && o.Acmod == other.Acmod
}

// Observer follows one audio elementary stream and reports what its frames say
// about the channel layout.
//
// It never stops looking. A service changes its audio between programmes as a
// matter of course - the same PID going from AC-3 2.0 to 5.1 for a film and back
// for the advertisements - so a single reading taken at session start is not an
// answer, it is an answer with an expiry date nobody recorded. Feed it for as
// long as the stream runs and Current() follows.
//
// Not safe for concurrent use; the caller holds the lock it already holds to feed
// the stream.
type Observer struct {
	carry []byte

	// skip is frame payload that ran past the end of a chunk, dropped from the
	// front of the next one instead of being scanned for syncwords it cannot
	// legitimately contain.
	skip int

	current      Observation
	candidate    Observation
	candidateRun int

	frames        uint64
	dependentSeen bool
}

// NewObserver returns an observer with nothing established yet.
func NewObserver() *Observer { return &Observer{} }

// Feed consumes elementary stream bytes. Chunk boundaries are arbitrary: a
// syncframe split across two calls is parsed from the second.
func (o *Observer) Feed(es []byte) {
	if len(es) == 0 {
		return
	}

	if o.skip > 0 {
		if o.skip >= len(es) {
			o.skip -= len(es)
			return
		}
		es = es[o.skip:]
		o.skip = 0
	}

	buf := es
	if len(o.carry) > 0 {
		buf = append(o.carry, es...)
	}

	i := 0
	for i+headerBytes <= len(buf) {
		if buf[i] != syncWordHi || buf[i+1] != syncWordLo {
			i++
			continue
		}
		h, ok := ParseSyncFrame(buf[i:])
		if !ok {
			i++
			continue
		}
		o.observe(h)

		// Step over the whole frame. Its payload is compressed audio and carries
		// the sync pattern by chance often enough to matter, so rescanning it is
		// how a parser talks itself into a layout the stream never had.
		if h.SizeBytes > headerBytes {
			i += h.SizeBytes
		} else {
			i += headerBytes
		}
	}

	if i > len(buf) {
		o.skip = i - len(buf)
		o.carry = o.carry[:0]
		return
	}

	tail := buf[i:]
	if len(tail) > carryBytes {
		tail = tail[len(tail)-carryBytes:]
	}
	o.carry = append(o.carry[:0], tail...)
}

// observe folds one frame into the running state.
func (o *Observer) observe(h FrameHeader) {
	if h.Dependent {
		o.dependentSeen = true
		return
	}
	if h.Channels <= 0 {
		return
	}
	o.frames++

	next := Observation{Channels: h.Channels, LFE: h.LFE, Acmod: h.Acmod, HasAcmod: true}
	if o.candidateRun > 0 && next.sameLayout(o.candidate) {
		o.candidateRun++
	} else {
		o.candidate = next
		o.candidateRun = 1
	}
	if o.candidateRun >= stableFrames {
		o.current = next
	}
}

// Current returns the established observation, or the zero value while none has
// been.
func (o *Observer) Current() Observation {
	obs := o.current
	obs.Frames = o.frames
	obs.DependentSubstream = o.dependentSeen
	return obs
}
