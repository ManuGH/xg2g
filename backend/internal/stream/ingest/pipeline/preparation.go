// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// Zap preparation.
//
// A channel change costs what the receiver costs: measured against this hardware, a
// cold tune reaches its first usable picture in 1.5 to 3.4 seconds, and no amount of
// software removes that. What software can do is stop showing the user a frozen
// picture while it happens. The stream being left is kept running and untouched; the
// stream being joined is prepared beside it; and only once the new one is provably
// presentable does anything visible change.
//
// The division of ownership matters and is deliberate. The server owns preparation
// and transport readiness — it is the only side that can see whether the receiver is
// delivering a complete, descrambled, joinable stream. The client owns the commit —
// it is the only side that knows whether its decoder, its audio renderer and its
// display are ready to replace one generation with another. Collapsing the two, by
// having the server hold a stream request open until it decides to switch, would put
// the presentation decision on the side that cannot observe presentation.
//
// A preparation is a held session lease plus an observation. Holding the lease is
// what keeps the ingest alive between preparing and committing, and it is also what
// makes preparation count against the tuner budget: leases are acquired through the
// same topology admission every live request uses, so a preparation occupies a tuner
// exactly as a viewer does, and is refused the same way when none is free. There is
// deliberately no second budget.

// PreparationState is the lifecycle of one preparation.
type PreparationState string

const (
	// PreparationPending means the ingest is running and readiness is being awaited.
	PreparationPending PreparationState = "pending"
	// PreparationReady means transport readiness was proven. The client may commit.
	PreparationReady PreparationState = "ready"
	// PreparationFailed means it will never become ready; Outcome says why.
	PreparationFailed PreparationState = "failed"
	// PreparationCancelled means it was abandoned — superseded by a newer zap from
	// the same client, dropped by the client, or expired without a commit.
	PreparationCancelled PreparationState = "cancelled"
	// PreparationCommitted means the client took it. The lease is handed over to the
	// warm hold so the imminent stream request coalesces onto the same ingest.
	PreparationCommitted PreparationState = "committed"
)

// Terminal reports whether no further transition is possible.
func (s PreparationState) Terminal() bool {
	switch s {
	case PreparationFailed, PreparationCancelled, PreparationCommitted:
		return true
	default:
		return false
	}
}

// Failure reasons that are not readiness outcomes. Readiness contributes its own
// (timeout, ingest_ended, cancelled) so the two vocabularies stay one set.
const (
	// OutcomeAdmissionDenied means no tuner was available for this transponder.
	OutcomeAdmissionDenied TransportReadyOutcome = "admission_denied"
	// OutcomeUnpresentable means the stream is provably unusable — a service the
	// receiver never descrambles is the measured case, and no waiting repairs it.
	OutcomeUnpresentable TransportReadyOutcome = "unpresentable"
)

var (
	// ErrNoSuchPreparation is returned for an unknown or already forgotten id.
	ErrNoSuchPreparation = errors.New("no such preparation")
	// ErrPreparationNotReady is returned when a commit arrives before readiness.
	ErrPreparationNotReady = errors.New("preparation is not ready")
	// ErrGenerationChanged is returned when the stream re-identified itself between
	// readiness and the commit. The client would be committing to a stream that no
	// longer exists, so it has to look again rather than be handed the wrong one.
	ErrGenerationChanged = errors.New("stream generation changed since readiness")
)

// PreparationStatus is the observable state of one preparation.
type PreparationStatus struct {
	ID         string
	ZapID      string
	ServiceRef string
	State      PreparationState
	// Outcome is set once the preparation leaves pending.
	Outcome TransportReadyOutcome
	// Generation identifies the stream that was found ready. A commit must name it.
	Generation uint64
	// ReadyAfter is how long readiness took, measured from the preparation starting.
	ReadyAfter time.Duration
	// Pending lists the criteria still outstanding, with the reason for each.
	Pending map[ReadinessCriterion]string
	// Detail carries the failure message where there is one.
	Detail string
}

// Preparation is one in-flight channel change.
type Preparation struct {
	id         string
	zapID      string
	clientID   string
	serviceRef string

	mu     sync.Mutex
	state  PreparationState
	status PreparationStatus
	lease  *session.Lease

	cancel context.CancelFunc
	done   chan struct{}
}

// ID returns the preparation identifier.
func (p *Preparation) ID() string { return p.id }

// Done is closed once the preparation reaches a terminal state.
func (p *Preparation) Done() <-chan struct{} { return p.done }

// Status returns a snapshot of the preparation.
func (p *Preparation) Status() PreparationStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.State = p.state
	return s
}

// PreparationConfig configures the manager.
type PreparationConfig struct {
	// ReadyTimeout bounds how long transport readiness is awaited. Beyond this the
	// preparation fails and releases its tuner rather than holding one indefinitely
	// for a channel that is not coming.
	ReadyTimeout time.Duration
	// CommitTimeout bounds how long a ready preparation waits to be taken. A client
	// that disappears after preparing must not pin a tuner, so an uncommitted
	// preparation expires on its own.
	CommitTimeout time.Duration
}

// DefaultPreparationConfig returns timeouts sized to the measured hardware: cold
// tunes were measured at 1.5 to 3.4 seconds and longer under load, so the readiness
// budget allows for the slow end without waiting on a channel that will not arrive.
func DefaultPreparationConfig() PreparationConfig {
	return PreparationConfig{
		ReadyTimeout:  8 * time.Second,
		CommitTimeout: 15 * time.Second,
	}
}

// SessionAcquirer is the part of the session manager a preparation needs. Narrow on
// purpose: a preparation acquires and releases leases and does nothing else to it.
type SessionAcquirer interface {
	Acquire(ctx context.Context, key session.SessionKey) (*session.Lease, error)
}

// PreparationManager owns in-flight preparations.
//
// One per client at a time. A newer zap supersedes the one in flight, and the older
// preparation is cancelled and its lease released *before* the new one is acquired,
// so a client zapping repeatedly never holds two tuners at once.
type PreparationManager struct {
	cfg      PreparationConfig
	sessions SessionAcquirer
	logger   zerolog.Logger

	mu       sync.Mutex
	byID     map[string]*Preparation
	byClient map[string]*Preparation
	seq      uint64
}

// NewPreparationManager creates a manager.
func NewPreparationManager(sessions SessionAcquirer, cfg PreparationConfig, logger zerolog.Logger) *PreparationManager {
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = DefaultPreparationConfig().ReadyTimeout
	}
	if cfg.CommitTimeout <= 0 {
		cfg.CommitTimeout = DefaultPreparationConfig().CommitTimeout
	}
	return &PreparationManager{
		cfg:      cfg,
		sessions: sessions,
		logger:   logger,
		byID:     make(map[string]*Preparation),
		byClient: make(map[string]*Preparation),
	}
}

// PrepareRequest names what to prepare and for whom.
//
// It carries no channel name, provider or bouquet position — a service reference, a
// programme, and who is asking. What the client can present is a separate question,
// answered against its effective capabilities rather than against any list here.
type PrepareRequest struct {
	ClientID      string
	ZapID         string
	Key           session.SessionKey
	TargetProgram uint16
}

// Prepare starts preparing a channel change and returns immediately.
//
// It never disturbs what the client is watching: the current stream has its own
// lease, held by its own request, and nothing here touches it. A preparation that
// fails leaves that stream exactly as it was — the client simply never commits.
func (m *PreparationManager) Prepare(req PrepareRequest) (*Preparation, error) {
	if req.ClientID == "" {
		return nil, errors.New("preparation requires a client identity")
	}

	// Supersede first, acquire second. Releasing the previous lease before taking a
	// new one is what keeps a client zapping down a channel list from occupying two
	// tuners at a time, which the receiver was measured to punish.
	m.supersede(req.ClientID, "superseded by a newer preparation")

	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("prep-%d-%d", m.seq, len(m.byID))
	ctx, cancel := context.WithCancel(context.Background())
	p := &Preparation{
		id:         id,
		zapID:      req.ZapID,
		clientID:   req.ClientID,
		serviceRef: req.Key.ServiceRef,
		state:      PreparationPending,
		cancel:     cancel,
		done:       make(chan struct{}),
		status: PreparationStatus{
			ID:         id,
			ZapID:      req.ZapID,
			ServiceRef: req.Key.ServiceRef,
			State:      PreparationPending,
		},
	}
	m.byID[id] = p
	m.byClient[req.ClientID] = p
	m.mu.Unlock()

	go m.run(ctx, p, req)
	return p, nil
}

// run acquires the session, awaits transport readiness, and records the outcome.
func (m *PreparationManager) run(ctx context.Context, p *Preparation, req PrepareRequest) {
	logger := m.logger.With().
		Str("preparation_id", p.id).
		Str("zap_id", p.zapID).
		Str("serviceRef", req.Key.ServiceRef).
		Logger()

	lease, err := m.sessions.Acquire(ctx, req.Key)
	if err != nil {
		// Admission refusal is a normal answer, not a fault: the tuners are all in
		// use and this zap cannot have one. It is terminal, and it is fast.
		outcome := OutcomeAdmissionDenied
		if errors.Is(err, context.Canceled) {
			outcome = OutcomeCancelled
		}
		m.finish(p, PreparationFailed, outcome, 0, 0, nil, err.Error())
		logger.Info().Err(err).Str("event", "zap.prepare.failed").Str("outcome", string(outcome)).Msg("preparation could not acquire an ingest")
		return
	}

	p.mu.Lock()
	if p.state.Terminal() {
		// Cancelled while the upstream was being dialled. Give the tuner straight
		// back rather than holding it for a preparation nobody is waiting on.
		p.mu.Unlock()
		lease.Release()
		return
	}
	p.lease = lease
	p.mu.Unlock()

	pipe, ok := lease.Session().Payload().(*SessionPipeline)
	if !ok || pipe == nil {
		m.finish(p, PreparationFailed, OutcomeUnpresentable, 0, 0, nil, "session holds no pipeline")
		m.release(p)
		return
	}

	snap, err := AwaitTransportReady(ctx, pipe, logger, m.cfg.ReadyTimeout)
	if err != nil {
		var notReady *TransportNotReadyError
		outcome := OutcomeTimeout
		detail := err.Error()
		if errors.As(err, &notReady) {
			outcome = notReady.Outcome
		}
		state := PreparationFailed
		if outcome == OutcomeCancelled {
			state = PreparationCancelled
		}
		m.finish(p, state, outcome, 0, 0, snap.Pending, detail)
		m.release(p)
		logger.Info().
			Str("event", "zap.prepare.failed").
			Str("outcome", string(outcome)).
			Dur("after", snap.ReadyAfterIngest).
			Msg("preparation did not become presentable")
		return
	}

	m.setReady(p, snap)
	logger.Info().
		Str("event", "zap.prepare.ready").
		Uint64("generation", snap.Generation).
		Dur("readyAfter", snap.ReadyAfter).
		Msg("preparation is presentable; awaiting client commit")

	// A ready preparation holds a tuner. If the client never commits — it moved on,
	// it crashed, the app was backgrounded — that tuner has to come back on its own.
	select {
	case <-ctx.Done():
	case <-time.After(m.cfg.CommitTimeout):
		if m.expire(p) {
			logger.Info().Str("event", "zap.prepare.expired").Msg("preparation expired without a commit")
		}
	}
}

// Commit hands a ready preparation to the client.
//
// Generation-bound: the caller states which stream it observed ready, and a commit
// naming a generation the stream has since left is refused. A PMT version bump or a
// codec change replaces the stream underneath a preparation, and committing to it
// then would switch the client onto something it never evaluated.
//
// Idempotent: committing twice with the same generation succeeds twice. A client
// retrying after a lost response must not be told its channel change failed.
func (m *PreparationManager) Commit(id string, generation uint64) (PreparationStatus, error) {
	m.mu.Lock()
	p, ok := m.byID[id]
	m.mu.Unlock()
	if !ok {
		return PreparationStatus{}, ErrNoSuchPreparation
	}

	p.mu.Lock()
	switch p.state {
	case PreparationCommitted:
		if p.status.Generation != generation {
			p.mu.Unlock()
			return p.Status(), ErrGenerationChanged
		}
		st := p.status
		st.State = p.state
		p.mu.Unlock()
		return st, nil
	case PreparationReady:
		if p.status.Generation != generation {
			p.mu.Unlock()
			return p.Status(), ErrGenerationChanged
		}
		p.state = PreparationCommitted
		p.status.State = PreparationCommitted
		st := p.status
		lease := p.lease
		p.lease = nil
		p.mu.Unlock()

		// The lease is released here rather than held: the warm hold keeps the
		// ingest alive across the gap between commit and the client's stream
		// request, which then coalesces onto the very same session. Holding it
		// instead would double-count this tuner for as long as the client took.
		if lease != nil {
			lease.Release()
		}
		p.cancel()
		p.closeDone()
		return st, nil
	default:
		st := p.status
		st.State = p.state
		p.mu.Unlock()
		return st, fmt.Errorf("%w: %s", ErrPreparationNotReady, st.State)
	}
}

// Cancel abandons a preparation and releases its tuner.
func (m *PreparationManager) Cancel(id, reason string) bool {
	m.mu.Lock()
	p, ok := m.byID[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	return m.cancelPreparation(p, reason)
}

// Status returns the current state of a preparation.
func (m *PreparationManager) Status(id string) (PreparationStatus, error) {
	m.mu.Lock()
	p, ok := m.byID[id]
	m.mu.Unlock()
	if !ok {
		return PreparationStatus{}, ErrNoSuchPreparation
	}
	return p.Status(), nil
}

// Owner returns which client started a preparation.
//
// Answered from the recorded owner rather than from the client's current
// preparation, so a superseded one can still be inspected and cancelled by the
// client that started it.
func (m *PreparationManager) Owner(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byID[id]
	if !ok {
		return "", false
	}
	return p.clientID, true
}

// ActiveForClient returns the client's in-flight preparation, if any.
func (m *PreparationManager) ActiveForClient(clientID string) (*Preparation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byClient[clientID]
	return p, ok
}

// Close cancels every preparation and releases every lease.
func (m *PreparationManager) Close() {
	m.mu.Lock()
	preps := make([]*Preparation, 0, len(m.byID))
	for _, p := range m.byID {
		preps = append(preps, p)
	}
	m.mu.Unlock()

	for _, p := range preps {
		m.cancelPreparation(p, "manager closed")
	}
}

func (m *PreparationManager) supersede(clientID, reason string) {
	m.mu.Lock()
	prev, ok := m.byClient[clientID]
	m.mu.Unlock()
	if ok {
		m.cancelPreparation(prev, reason)
	}
}

func (m *PreparationManager) cancelPreparation(p *Preparation, reason string) bool {
	p.mu.Lock()
	if p.state.Terminal() {
		p.mu.Unlock()
		return false
	}
	p.state = PreparationCancelled
	p.status.State = PreparationCancelled
	p.status.Outcome = OutcomeCancelled
	p.status.Detail = reason
	lease := p.lease
	p.lease = nil
	p.mu.Unlock()

	if lease != nil {
		lease.Release()
	}
	p.cancel()
	p.closeDone()
	m.forget(p)
	return true
}

func (m *PreparationManager) expire(p *Preparation) bool {
	p.mu.Lock()
	if p.state != PreparationReady {
		p.mu.Unlock()
		return false
	}
	p.state = PreparationCancelled
	p.status.State = PreparationCancelled
	p.status.Outcome = OutcomeCancelled
	p.status.Detail = "expired without a commit"
	lease := p.lease
	p.lease = nil
	p.mu.Unlock()

	if lease != nil {
		lease.Release()
	}
	p.cancel()
	p.closeDone()
	m.forget(p)
	return true
}

func (m *PreparationManager) setReady(p *Preparation, snap Snapshot) {
	p.mu.Lock()
	if p.state.Terminal() {
		p.mu.Unlock()
		return
	}
	p.state = PreparationReady
	p.status.State = PreparationReady
	p.status.Outcome = OutcomeReady
	p.status.Generation = snap.Generation
	p.status.ReadyAfter = snap.ReadyAfter
	p.status.Pending = nil
	p.mu.Unlock()
}

func (m *PreparationManager) finish(p *Preparation, state PreparationState, outcome TransportReadyOutcome,
	generation uint64, readyAfter time.Duration, pending map[ReadinessCriterion]string, detail string) {
	p.mu.Lock()
	if p.state.Terminal() {
		p.mu.Unlock()
		return
	}
	p.state = state
	p.status.State = state
	p.status.Outcome = outcome
	p.status.Generation = generation
	p.status.ReadyAfter = readyAfter
	p.status.Pending = pending
	p.status.Detail = detail
	p.mu.Unlock()

	p.cancel()
	p.closeDone()
	m.forget(p)
}

// release returns a preparation's lease without changing its recorded outcome.
func (m *PreparationManager) release(p *Preparation) {
	p.mu.Lock()
	lease := p.lease
	p.lease = nil
	p.mu.Unlock()
	if lease != nil {
		lease.Release()
	}
}

// forget drops the client mapping so the next zap is not treated as superseding a
// preparation that is already over. The id mapping is kept so a late status request
// still gets its answer rather than "no such preparation".
func (m *PreparationManager) forget(p *Preparation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.byClient[p.clientID]; ok && cur == p {
		delete(m.byClient, p.clientID)
	}
}

func (p *Preparation) closeDone() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}
