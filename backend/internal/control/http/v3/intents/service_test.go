package intents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/rs/zerolog"
)

type mockSessionStore struct {
	putExistingID string
	putExists     bool
	putErr        error
	putSequence   []putSessionResult
	putCalls      int
	putSession    *model.SessionRecord
	putIdemKey    string
	putTTL        time.Duration
	deleteCalls   int
	deleteKey     string
	deleteSID     string
	deleteOk      bool
	deleteErr     error
	sessions      map[string]*model.SessionRecord
}

type putSessionResult struct {
	existingID string
	exists     bool
	err        error
}

func (m *mockSessionStore) GetSession(_ context.Context, id string) (*model.SessionRecord, error) {
	if m.sessions == nil {
		return nil, nil
	}
	return m.sessions[id], nil
}

func (m *mockSessionStore) PutSessionWithIdempotency(_ context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) (existingID string, exists bool, err error) {
	m.putCalls++
	m.putSession = s
	m.putIdemKey = idemKey
	m.putTTL = ttl
	if len(m.putSequence) > 0 {
		next := m.putSequence[0]
		m.putSequence = m.putSequence[1:]
		return next.existingID, next.exists, next.err
	}
	if m.putErr != nil {
		return "", false, m.putErr
	}
	return m.putExistingID, m.putExists, nil
}

func (m *mockSessionStore) DeleteIdempotencyIfMatch(_ context.Context, idemKey, sessionID string) (bool, error) {
	m.deleteCalls++
	m.deleteKey = idemKey
	m.deleteSID = sessionID
	if m.deleteErr != nil {
		return false, m.deleteErr
	}
	return m.deleteOk, nil
}

type publishCall struct {
	topic string
	event any
}

type mockEventBus struct {
	err   error
	calls []publishCall
}

func (m *mockEventBus) Publish(_ context.Context, topic string, evt any) error {
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, publishCall{topic: topic, event: evt})
	return nil
}

type mockChannelScanner struct {
	capability scan.Capability
	found      bool
}

func (m *mockChannelScanner) GetCapability(_ string) (scan.Capability, bool) {
	return m.capability, m.found
}

type mockAdmissionController struct {
	decision admission.Decision
}

func (m *mockAdmissionController) Check(context.Context, admission.Request, admission.RuntimeState) admission.Decision {
	return m.decision
}

type mockDeps struct {
	dvrWindow         time.Duration
	hasTunerSlots     bool
	sessionLeaseTTL   time.Duration
	heartbeatInterval time.Duration
	store             *mockSessionStore
	bus               *mockEventBus
	scanner           ChannelScanner
	controller        AdmissionController
	runtimeState      admission.RuntimeState
	verifyAttestation bool
	playbackKeyCalls  int
	rejectCodes       []string
	admitCalls        int
	intentCalls       []string
	publishCalls      []string
	replayCalls       []string
}

func newMockDeps() *mockDeps {
	return &mockDeps{
		dvrWindow:         30 * time.Second,
		hasTunerSlots:     true,
		sessionLeaseTTL:   30 * time.Second,
		heartbeatInterval: 5 * time.Second,
		store:             &mockSessionStore{},
		bus:               &mockEventBus{},
		controller:        &mockAdmissionController{decision: admission.Decision{Allow: true}},
		runtimeState:      admission.RuntimeState{TunerSlots: 2, SessionsActive: 0, TranscodesActive: 0},
		verifyAttestation: true,
	}
}

func (m *mockDeps) DVRWindow() time.Duration { return m.dvrWindow }

func (m *mockDeps) HasTunerSlots() bool { return m.hasTunerSlots }

func (m *mockDeps) SessionLeaseTTL() time.Duration { return m.sessionLeaseTTL }

func (m *mockDeps) SessionHeartbeatInterval() time.Duration { return m.heartbeatInterval }

func (m *mockDeps) SessionStore() SessionStore { return m.store }

func (m *mockDeps) EventBus() EventBus { return m.bus }

func (m *mockDeps) ChannelScanner() ChannelScanner { return m.scanner }

func (m *mockDeps) AdmissionController() AdmissionController { return m.controller }

func (m *mockDeps) AdmissionRuntimeState(context.Context) admission.RuntimeState {
	return m.runtimeState
}

func (m *mockDeps) VerifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode string) bool {
	_ = token
	_ = principalID
	_ = serviceRef
	_ = playbackMode
	return m.verifyAttestation
}

func (m *mockDeps) IncLivePlaybackKey(keyLabel, resultLabel string) {
	_ = keyLabel
	_ = resultLabel
	m.playbackKeyCalls++
}

func (m *mockDeps) RecordReject(code string) {
	m.rejectCodes = append(m.rejectCodes, code)
}

func (m *mockDeps) RecordAdmit() {
	m.admitCalls++
}

func (m *mockDeps) RecordIntent(intentType, mode, outcome string) {
	m.intentCalls = append(m.intentCalls, string(intentType)+":"+mode+":"+outcome)
}

func (m *mockDeps) RecordPublish(eventType, outcome string) {
	m.publishCalls = append(m.publishCalls, eventType+":"+outcome)
}

func (m *mockDeps) RecordReplay(intentType string) {
	m.replayCalls = append(m.replayCalls, intentType)
}

func TestService_ProcessIntent_InvalidType(t *testing.T) {
	svc := NewService(newMockDeps())

	res, err := svc.ProcessIntent(context.Background(), Intent{Type: model.IntentType("unknown.intent"), Logger: zerolog.Nop()})
	if res != nil {
		t.Fatalf("expected nil result, got %#v", res)
	}
	if err == nil || err.Kind != ErrorInvalidInput {
		t.Fatalf("expected ErrorInvalidInput, got %#v", err)
	}
}

func TestService_ProcessIntent_StartAdmissionRejected(t *testing.T) {
	deps := newMockDeps()
	deps.controller = &mockAdmissionController{decision: admission.Decision{
		Allow:   false,
		Problem: admission.NewSessionsFull(10, 10),
	}}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-1",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{},
		CorrelationID: "corr-1",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	})
	if res != nil {
		t.Fatalf("expected nil result, got %#v", res)
	}
	if err == nil || err.Kind != ErrorAdmissionRejected {
		t.Fatalf("expected ErrorAdmissionRejected, got %#v", err)
	}
	if err.AdmissionProblem == nil || err.AdmissionProblem.Code != admission.CodeSessionsFull {
		t.Fatalf("expected sessions-full problem, got %#v", err.AdmissionProblem)
	}
	if err.RetryAfter != "5" {
		t.Fatalf("expected RetryAfter=5, got %q", err.RetryAfter)
	}
	if deps.store.putCalls != 0 {
		t.Fatalf("expected no store writes, got %d", deps.store.putCalls)
	}
	if len(deps.bus.calls) != 0 {
		t.Fatalf("expected no bus publish, got %d", len(deps.bus.calls))
	}
}

func TestService_ProcessIntent_StartAcceptedPublishesEvent(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-1",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "high"},
		CorrelationID: "corr-1",
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %#v", err)
	}
	if res == nil || res.Status != "accepted" {
		t.Fatalf("expected accepted result, got %#v", res)
	}
	if deps.store.putCalls != 1 {
		t.Fatalf("expected 1 store write, got %d", deps.store.putCalls)
	}
	if deps.store.putIdemKey == "" {
		t.Fatal("expected idempotency key to be computed")
	}
	if len(deps.bus.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(deps.bus.calls))
	}
	if deps.bus.calls[0].topic != string(model.EventStartSession) {
		t.Fatalf("unexpected publish topic: %q", deps.bus.calls[0].topic)
	}
	if deps.admitCalls != 1 {
		t.Fatalf("expected admit metric to be recorded, got %d", deps.admitCalls)
	}
	if deps.store.putSession == nil || deps.store.putSession.PlaybackTrace == nil {
		t.Fatal("expected playback trace to be persisted")
	}
	if deps.store.putSession.PlaybackTrace.RequestProfile != "compatible" {
		t.Fatalf("expected compatible public request profile, got %q", deps.store.putSession.PlaybackTrace.RequestProfile)
	}
}

func TestService_ProcessIntent_StartPreservesExplicitQualityIntentInTrace(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-quality",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "quality"},
		CorrelationID: "corr-quality",
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %#v", err)
	}
	if res == nil || res.Status != "accepted" {
		t.Fatalf("expected accepted result, got %#v", res)
	}
	if deps.store.putSession == nil || deps.store.putSession.PlaybackTrace == nil {
		t.Fatal("expected playback trace to be persisted")
	}
	if deps.store.putSession.PlaybackTrace.RequestProfile != "quality" {
		t.Fatalf("expected quality public request profile, got %q", deps.store.putSession.PlaybackTrace.RequestProfile)
	}
	if deps.store.putSession.Profile.Name != "high" {
		t.Fatalf("expected legacy internal high profile bridge, got %q", deps.store.putSession.Profile.Name)
	}
}

func TestService_ProcessIntent_StartReplayReturnsExistingSession(t *testing.T) {
	deps := newMockDeps()
	deps.store.putExistingID = "existing-sid"
	deps.store.putExists = true
	deps.store.sessions = map[string]*model.SessionRecord{
		"existing-sid": {CorrelationID: "corr-existing"},
	}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "new-sid",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "high"},
		CorrelationID: "corr-new",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %#v", err)
	}
	if res == nil || res.Status != "idempotent_replay" {
		t.Fatalf("expected idempotent_replay result, got %#v", res)
	}
	if res.SessionID != "existing-sid" {
		t.Fatalf("expected existing session ID, got %q", res.SessionID)
	}
	if res.CorrelationID != "corr-existing" {
		t.Fatalf("expected replay correlation from existing session, got %q", res.CorrelationID)
	}
	if len(deps.bus.calls) != 0 {
		t.Fatalf("expected no event publish on replay, got %d", len(deps.bus.calls))
	}
}

func TestService_ProcessIntent_StartTerminalReplayCreatesFreshSession(t *testing.T) {
	deps := newMockDeps()
	deps.store.putSequence = []putSessionResult{
		{existingID: "stale-sid", exists: true},
		{exists: false},
	}
	deps.store.deleteOk = true
	deps.store.sessions = map[string]*model.SessionRecord{
		"stale-sid": {
			SessionID:     "stale-sid",
			State:         model.SessionFailed,
			CorrelationID: "corr-stale",
		},
	}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "fresh-sid",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "high"},
		CorrelationID: "corr-new",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %#v", err)
	}
	if res == nil || res.Status != "accepted" {
		t.Fatalf("expected accepted result, got %#v", res)
	}
	if res.SessionID != "fresh-sid" {
		t.Fatalf("expected fresh session ID, got %q", res.SessionID)
	}
	if deps.store.deleteCalls != 1 {
		t.Fatalf("expected stale idempotency cleanup once, got %d", deps.store.deleteCalls)
	}
	if deps.store.deleteSID != "stale-sid" {
		t.Fatalf("expected stale session ID cleanup, got %q", deps.store.deleteSID)
	}
	if deps.store.putCalls != 2 {
		t.Fatalf("expected retry after stale replay, got %d store calls", deps.store.putCalls)
	}
	if len(deps.bus.calls) != 1 {
		t.Fatalf("expected one publish after stale replay cleanup, got %d", len(deps.bus.calls))
	}
}

func TestService_ProcessIntent_StopAcceptedPublishesEvent(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStop,
		SessionID:     "sid-1",
		CorrelationID: "corr-1",
		Logger:        zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %#v", err)
	}
	if res == nil || res.Status != "accepted" {
		t.Fatalf("expected accepted result, got %#v", res)
	}
	if len(deps.bus.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(deps.bus.calls))
	}
	if deps.bus.calls[0].topic != string(model.EventStopSession) {
		t.Fatalf("unexpected publish topic: %q", deps.bus.calls[0].topic)
	}
}

func TestService_ProcessIntent_StartStoreError(t *testing.T) {
	deps := newMockDeps()
	deps.store.putErr = errors.New("boom")
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-1",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "high"},
		CorrelationID: "corr-1",
		Mode:          model.ModeLive,
		Logger:        zerolog.Nop(),
	})
	if res != nil {
		t.Fatalf("expected nil result, got %#v", res)
	}
	if err == nil || err.Kind != ErrorStoreUnavailable {
		t.Fatalf("expected ErrorStoreUnavailable, got %#v", err)
	}
}
