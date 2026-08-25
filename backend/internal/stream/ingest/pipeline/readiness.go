// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"fmt"
	"strings"
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
// streamIdentity is what makes this stream the stream it is. A change here is a
// different programme, not the same one further along.
type streamIdentity struct {
	programNumber uint16
	pmtVersion    uint8
	videoPID      uint16
	videoCodec    ring.VideoCodec
}

type ReadinessObserver struct {
	mu sync.Mutex
	// ingestStart never moves. Readiness measured from here is comparable with the
	// client's time-to-first-picture, which is also measured from one fixed instant.
	ingestStart time.Time
	// start moves to the last re-identification, so per-generation timings describe
	// the stream that is actually playing.
	start      time.Time
	generation uint64
	identity   streamIdentity
	identified bool
	states     map[ReadinessCriterion]*criterionState
	// resets counts how often the stream's PSI changed identity underneath the
	// observation. A PMT version bump or a codec change makes every timestamp
	// collected so far describe a different stream, so they are discarded.
	resets int
	logger zerolog.Logger
}

// NewReadinessObserver starts an observation at the given instant.
func NewReadinessObserver(start time.Time, logger zerolog.Logger) *ReadinessObserver {
	o := &ReadinessObserver{ingestStart: start, start: start, logger: logger}
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
	o.generation = facts.Generation

	// Re-identification is judged on the programme, not on the ring's generation
	// counter. That counter increments twice during an ordinary tune - once for the
	// first PAT, once for the first PMT - and treating those as identity changes
	// restarted the clock mid-tune and reported every timing relative to a moment
	// that meant nothing. What matters is the programme, its PMT version, and the
	// codec: when one of those changes after being established, the stream that was
	// being timed is gone.
	if facts.HasPMT && facts.VideoPID != 0 {
		current := streamIdentity{
			programNumber: facts.ProgramNumber,
			pmtVersion:    facts.PMTVersion,
			videoPID:      facts.VideoPID,
			videoCodec:    facts.VideoCodec,
		}
		switch {
		case !o.identified:
			o.identity = current
			o.identified = true
		case current != o.identity:
			o.resets++
			o.logger.Info().
				Uint16("previous_program", o.identity.programNumber).
				Uint8("previous_pmt_version", o.identity.pmtVersion).
				Uint16("previous_video_pid", o.identity.videoPID).
				Uint16("program", current.programNumber).
				Uint8("pmt_version", current.pmtVersion).
				Uint16("video_pid", current.videoPID).
				Str("codec", string(current.videoCodec)).
				Int("resets", o.resets).
				Str("event", "zap.readiness.reset").
				Msg("stream identity changed; readiness observation restarted")
			o.identity = current
			o.start = now
			o.resetStatesLocked()
		}
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
		//
		// Any complete picture counts, not only one a decoder could start on. Keying
		// this on entry points made it true at the same millisecond as the random
		// access criterion in every measured zap, which is a criterion that adds
		// nothing.
		if f.CleanAccessUnits == 0 {
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
	// ReadyAfter is measured from the last re-identification.
	ReadyAfter time.Duration
	// ReadyAfterIngest is measured from the start of the ingest and never restarts,
	// which is what makes it comparable with a client-side time-to-first-picture.
	ReadyAfterIngest time.Duration
	Met              map[ReadinessCriterion]time.Duration
	Pending          map[ReadinessCriterion]string
	Resets           int
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
		return s
	}
	s.ReadyAfterIngest = s.ReadyAfter + o.start.Sub(o.ingestStart)
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
		evt = evt.Dur("ready_after", snap.ReadyAfter).
			Dur("ready_after_ingest", snap.ReadyAfterIngest)
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

// Awaiting transport readiness.
//
// The observer above answers "is this stream presentable yet" continuously and
// decides nothing. A zap transaction needs the same question answered once, with a
// deadline, and needs the answer to name a cause when it is no - because the failure
// this whole architecture exists to remove is the silent one: an upstream that
// answers HTTP 200, delivers a clean EOF two seconds later, and leaves the previous
// picture frozen on screen with nothing anywhere saying why.
//
// Nothing in the serving path calls this yet. It is the primitive Prepare is built
// on, and it changes no behaviour on its own.

// TransportReadyOutcome names why an await ended without the stream becoming
// presentable. The strings are stable: they appear in logs and in the reason a
// client is given for a failed channel change.
type TransportReadyOutcome string

const (
	// OutcomeReady means every transport criterion was satisfied.
	OutcomeReady TransportReadyOutcome = "ready"
	// OutcomeTimeout means the deadline passed with criteria still unmet.
	OutcomeTimeout TransportReadyOutcome = "timeout"
	// OutcomeIngestEnded means the upstream stopped before the stream was
	// presentable - the clean-EOF case, which is not success however clean it was.
	OutcomeIngestEnded TransportReadyOutcome = "ingest_ended"
	// OutcomeCancelled means the caller abandoned the attempt, which a newer zap
	// on the same client does deliberately.
	OutcomeCancelled TransportReadyOutcome = "cancelled"
)

// TransportNotReadyError reports an await that did not reach readiness.
//
// It carries the observation rather than a message, so a caller can log which
// criteria were outstanding and why without re-deriving them.
type TransportNotReadyError struct {
	Outcome  TransportReadyOutcome
	Snapshot Snapshot
	Elapsed  time.Duration
	// Cause is the pipeline's own error where one exists. A clean EOF leaves this
	// nil, which is itself worth reporting: it distinguishes "the upstream broke"
	// from "the upstream simply stopped".
	Cause error
}

func (e *TransportNotReadyError) Error() string {
	pending := e.Snapshot.PendingCriteria()
	parts := make([]string, 0, len(pending))
	for _, c := range pending {
		parts = append(parts, fmt.Sprintf("%s (%s)", c, e.Snapshot.Pending[c]))
	}
	msg := fmt.Sprintf("transport not ready after %s: %s", e.Elapsed.Round(time.Millisecond), e.Outcome)
	if len(parts) > 0 {
		msg += "; outstanding: " + strings.Join(parts, ", ")
	}
	if e.Cause != nil {
		msg += "; cause: " + e.Cause.Error()
	}
	return msg
}

func (e *TransportNotReadyError) Unwrap() error { return e.Cause }

// PendingCriteria lists the unsatisfied criteria in evaluation order, so a log
// line reads in the order the conditions are expected to become true.
func (s Snapshot) PendingCriteria() []ReadinessCriterion {
	out := make([]ReadinessCriterion, 0, len(s.Pending))
	for _, c := range AllReadinessCriteria {
		if _, unmet := s.Pending[c]; unmet {
			out = append(out, c)
		}
	}
	return out
}

// AwaitTransportReady samples the pipeline until every transport criterion holds,
// the deadline passes, the ingest ends, or the caller cancels.
//
// Transport readiness is client-agnostic on purpose: it asks whether the receiver is
// delivering this service completely and in the clear, not whether any particular
// client can present it. Whether the bytes are of use to the client asking for them
// is a separate question, answered against that client's effective capabilities.
func AwaitTransportReady(ctx context.Context, pipe *SessionPipeline, logger zerolog.Logger, timeout time.Duration) (Snapshot, error) {
	if pipe == nil {
		return Snapshot{}, &TransportNotReadyError{Outcome: OutcomeCancelled}
	}
	master := pipe.MasterRing()
	if master == nil {
		return Snapshot{}, &TransportNotReadyError{Outcome: OutcomeCancelled}
	}

	start := time.Now()
	observer := NewReadinessObserver(start, logger)
	ticker := time.NewTicker(readinessSampleInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	fail := func(outcome TransportReadyOutcome, cause error) (Snapshot, error) {
		snap := observer.Snapshot()
		return snap, &TransportNotReadyError{
			Outcome:  outcome,
			Snapshot: snap,
			Elapsed:  time.Since(start),
			Cause:    cause,
		}
	}

	for {
		observer.Observe(master.ReadinessFacts(), time.Now())
		if snap := observer.Snapshot(); snap.Ready {
			return snap, nil
		}

		select {
		case <-ctx.Done():
			return fail(OutcomeCancelled, ctx.Err())
		case <-pipe.Done():
			// One last look: the ingest may have ended in the same interval that
			// completed the picture, and discarding that would report a failure for
			// a stream that did become presentable.
			observer.Observe(master.ReadinessFacts(), time.Now())
			if snap := observer.Snapshot(); snap.Ready {
				return snap, nil
			}
			return fail(OutcomeIngestEnded, pipe.Err())
		case <-deadline.C:
			return fail(OutcomeTimeout, pipe.Err())
		case <-ticker.C:
		}
	}
}
