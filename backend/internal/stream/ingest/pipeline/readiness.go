// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// Readiness observation.
//
// HTTP 200 says nothing about whether a channel can be presented. Measured against
// the reference receiver, the streamserver answers 200 and then delivers: nothing at
// all; bytes but no PSI; PSI and video that never descrambles; or a clean EOF two
// seconds later. All four are indistinguishable at the status line, and the current
// pipeline treats the first byte that arrives as success.
//
// This observer names the conditions that actually have to hold, and records when
// each one became true. It decides nothing: the stream is served exactly as before,
// on the same schedule. The point of running it in observation mode first is to find
// out, against real broadcasts, whether these six conditions really mark the moment a
// commit would be safe - and to change the definition before it gates anything if the
// measurements say one of them is redundant, wrong, or not enough.

// ReadinessCriterion names one condition. The identifiers appear in logs, so they
// are stable strings rather than an integer enum.
type ReadinessCriterion string

const (
	// CriterionTransport: PAT seen and the PMT for the requested program fully
	// reassembled, multi-packet sections included.
	CriterionTransport ReadinessCriterion = "transport"
	// CriterionProgram: the PMT names a video PID with a supported codec and at
	// least one audio PID.
	CriterionProgram ReadinessCriterion = "program"
	// CriterionDescrambled: a complete access unit arrived with no scrambled packet
	// in it, and audio is arriving clear.
	CriterionDescrambled ReadinessCriterion = "descrambled"
	// CriterionRandomAccess: an entry point a decoder can be started on is in the
	// buffer.
	CriterionRandomAccess ReadinessCriterion = "random_access"
	// CriterionCodecConfig: the parameter sets for the active codec were seen.
	CriterionCodecConfig ReadinessCriterion = "codec_config"
	// CriterionAudio: an audio stream is named and its packets are arriving clear.
	//
	// This is the server's half of the question. Whether an audio frame actually
	// decodes, and how much of it has to be buffered before a clock can start, is
	// only visible on the client - which is why the observation phase compares both.
	CriterionAudio ReadinessCriterion = "audio"
)

// AllReadinessCriteria is the evaluation order, which is also the order they are
// expected to become true in.
var AllReadinessCriteria = []ReadinessCriterion{
	CriterionTransport,
	CriterionProgram,
	CriterionDescrambled,
	CriterionRandomAccess,
	CriterionCodecConfig,
	CriterionAudio,
}

// criterionState records when a condition became true, or why it has not.
type criterionState struct {
	metAfter time.Duration
	met      bool
	reason   string
}

// ReadinessObserver evaluates the criteria against ring facts over time.
//
// Not safe for concurrent use; one observer belongs to one ingest.
type ReadinessObserver struct {
	mu         sync.Mutex
	start      time.Time
	generation uint64
	states     map[ReadinessCriterion]*criterionState
	// resets counts how often the stream's PSI changed identity underneath the
	// observation. A PMT version bump or a codec change makes every timestamp
	// collected so far describe a different stream, so they are discarded.
	resets int
	logger zerolog.Logger
}

// NewReadinessObserver starts an observation at the given instant.
func NewReadinessObserver(start time.Time, logger zerolog.Logger) *ReadinessObserver {
	o := &ReadinessObserver{start: start, logger: logger}
	o.resetStatesLocked()
	return o
}

func (o *ReadinessObserver) resetStatesLocked() {
	o.states = make(map[ReadinessCriterion]*criterionState, len(AllReadinessCriteria))
	for _, c := range AllReadinessCriteria {
		o.states[c] = &criterionState{reason: "not evaluated yet"}
	}
}

// Observe folds one snapshot of ring facts into the observation.
func (o *ReadinessObserver) Observe(facts ring.ReadinessFacts, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Generations only ever move forward. A snapshot carrying an older one was
	// produced by a stream that has already been replaced - a preparation that was
	// abandoned, or a sample that was in flight when the PSI changed - and acting on
	// it would take the observation backwards onto a stream nobody is watching.
	if facts.Generation < o.generation {
		return
	}

	// A generation change means the PSI now describes a different stream: a new PMT
	// version, a different program, or a codec change. Timestamps from before it
	// belong to the stream that was replaced, and carrying them forward would report
	// a readiness that was never reached for what is playing now.
	//
	// The very first snapshot is not such a change. The observer starts at generation
	// zero meaning "nothing seen yet", and the ingest it is timing began when the
	// observer was created, not when its first PSI happened to arrive.
	if facts.Generation != o.generation {
		if o.generation != 0 {
			o.resets++
			o.logger.Info().
				Uint64("previous_generation", o.generation).
				Uint64("generation", facts.Generation).
				Int("resets", o.resets).
				Str("event", "zap.readiness.reset").
				Msg("stream identity changed; readiness observation restarted")
			o.start = now
			o.resetStatesLocked()
		}
		o.generation = facts.Generation
	}

	elapsed := now.Sub(o.start)
	for _, c := range AllReadinessCriteria {
		st := o.states[c]
		if st.met {
			continue
		}
		ok, reason := evaluateCriterion(c, facts)
		st.reason = reason
		if !ok {
			continue
		}
		st.met = true
		st.metAfter = elapsed
		o.logger.Info().
			Str("criterion", string(c)).
			Dur("after", elapsed).
			Uint64("generation", facts.Generation).
			Str("event", "zap.readiness.criterion").
			Msg("readiness criterion satisfied")
	}
}

// evaluateCriterion answers one condition and says why when the answer is no.
func evaluateCriterion(c ReadinessCriterion, f ring.ReadinessFacts) (bool, string) {
	switch c {
	case CriterionTransport:
		if !f.HasPAT {
			return false, "no PAT"
		}
		if !f.HasPMT {
			return false, "PAT seen, PMT not yet complete"
		}
		return true, ""

	case CriterionProgram:
		if f.VideoPID == 0 || f.VideoCodec == ring.CodecUnknown {
			return false, "PMT names no video stream this build can decode"
		}
		if len(f.AudioPIDs) == 0 {
			return false, "PMT names no audio stream"
		}
		return true, ""

	case CriterionDescrambled:
		// Deliberately not a packet count. Descrambling was measured coming up
		// intermittently, with clear and scrambled packets interleaved for most of
		// a second, so any threshold of "N clear packets" can be met in the middle
		// of that. A whole access unit with no scrambled packet in it cannot.
		if f.CleanEntryPoints == 0 {
			if f.Scrambling.VideoScrambled > 0 && f.Scrambling.VideoClear == 0 {
				return false, "video is scrambled; receiver is not descrambling this service"
			}
			return false, "no access unit has arrived free of scrambled packets"
		}
		if len(f.AudioPIDs) > 0 && f.Scrambling.AudioClearRun == 0 {
			return false, "video is clear but audio is not"
		}
		return true, ""

	case CriterionRandomAccess:
		if f.RandomAccess.IRAPPoints+f.RandomAccess.IntraPoints == 0 {
			if f.RandomAccess.PredictedRejected > 0 {
				return false, fmt.Sprintf("only predicted pictures so far (%d rejected)", f.RandomAccess.PredictedRejected)
			}
			return false, "no entry point yet"
		}
		if !f.AttachAvailable {
			return false, "entry point was found but has already left the buffer"
		}
		return true, ""

	case CriterionCodecConfig:
		if !f.ParameterSetsSeen {
			return false, "decoder configuration not seen"
		}
		return true, ""

	case CriterionAudio:
		if len(f.AudioPIDs) == 0 {
			return false, "no audio stream named"
		}
		if f.Scrambling.AudioClear == 0 {
			return false, "no clear audio packet yet"
		}
		return true, ""
	}
	return false, "unknown criterion"
}

// Snapshot is the observation so far.
type Snapshot struct {
	Generation uint64
	Ready      bool
	ReadyAfter time.Duration
	Met        map[ReadinessCriterion]time.Duration
	Pending    map[ReadinessCriterion]string
	Resets     int
}

// Snapshot returns the current observation.
func (o *ReadinessObserver) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()

	s := Snapshot{
		Generation: o.generation,
		Ready:      true,
		Met:        make(map[ReadinessCriterion]time.Duration),
		Pending:    make(map[ReadinessCriterion]string),
		Resets:     o.resets,
	}
	for _, c := range AllReadinessCriteria {
		st := o.states[c]
		if st.met {
			s.Met[c] = st.metAfter
			if st.metAfter > s.ReadyAfter {
				s.ReadyAfter = st.metAfter
			}
			continue
		}
		s.Ready = false
		s.Pending[c] = st.reason
	}
	if !s.Ready {
		s.ReadyAfter = 0
	}
	return s
}

// readinessSampleInterval matches the attach poll, so the observation resolves
// events at the same granularity the serving path already works at.
const readinessSampleInterval = 10 * time.Millisecond

// ObserveReadiness samples a pipeline until every criterion is satisfied, the
// pipeline ends, or the context is cancelled, then logs the outcome once.
//
// It runs alongside the stream and never gates it: nothing in the serving path waits
// on this, and the only thing it touches on the ring is a lock-guarded snapshot.
func ObserveReadiness(ctx context.Context, pipe *SessionPipeline, logger zerolog.Logger, budget time.Duration) {
	if pipe == nil {
		return
	}
	master := pipe.MasterRing()
	if master == nil {
		return
	}

	start := time.Now()
	observer := NewReadinessObserver(start, logger)
	ticker := time.NewTicker(readinessSampleInterval)
	defer ticker.Stop()
	deadline := start.Add(budget)

	for {
		observer.Observe(master.ReadinessFacts(), time.Now())
		snap := observer.Snapshot()
		if snap.Ready {
			logReadiness(logger, snap, "zap.readiness.ready", "channel became presentable")
			return
		}

		select {
		case <-ctx.Done():
			logReadiness(logger, observer.Snapshot(), "zap.readiness.abandoned", "readiness observation cancelled")
			return
		case <-pipe.Done():
			logReadiness(logger, observer.Snapshot(), "zap.readiness.ended", "ingest ended before the channel was presentable")
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				logReadiness(logger, observer.Snapshot(), "zap.readiness.timeout", "channel did not become presentable within the observation budget")
				return
			}
		}
	}
}

func logReadiness(logger zerolog.Logger, snap Snapshot, event, message string) {
	evt := logger.Info().
		Str("event", event).
		Bool("ready", snap.Ready).
		Uint64("generation", snap.Generation).
		Int("psi_resets", snap.Resets)

	if snap.Ready {
		evt = evt.Dur("ready_after", snap.ReadyAfter)
	}
	for _, c := range AllReadinessCriteria {
		if d, ok := snap.Met[c]; ok {
			evt = evt.Dur("met_"+string(c), d)
			continue
		}
		if reason, ok := snap.Pending[c]; ok {
			evt = evt.Str("pending_"+string(c), reason)
		}
	}
	evt.Msg(message)
}
