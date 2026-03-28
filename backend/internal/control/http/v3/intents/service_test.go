package intents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
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
	hostPressure      playbackprofile.HostPressureAssessment
	verifyAttestation bool
	playbackOperator  config.PlaybackOperatorConfig
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

func (m *mockDeps) PlaybackOperator() config.PlaybackOperatorConfig { return m.playbackOperator }

func (m *mockDeps) SessionStore() SessionStore { return m.store }

func (m *mockDeps) EventBus() EventBus { return m.bus }

func (m *mockDeps) ChannelScanner() ChannelScanner { return m.scanner }

func (m *mockDeps) AdmissionController() AdmissionController { return m.controller }

func (m *mockDeps) AdmissionRuntimeState(context.Context) admission.RuntimeState {
	return m.runtimeState
}

func (m *mockDeps) HostPressure(context.Context) playbackprofile.HostPressureAssessment {
	return m.hostPressure
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
	if deps.store.putSession.PlaybackTrace.VideoQualityRung != "" {
		t.Fatalf("expected copy-compatible live path to omit explicit video rung, got %q", deps.store.putSession.PlaybackTrace.VideoQualityRung)
	}
}

func TestService_ProcessIntent_StartAppliesOperatorOverridesToTraceAndProfile(t *testing.T) {
	deps := newMockDeps()
	deps.playbackOperator = config.PlaybackOperatorConfig{
		ForceIntent:           "repair",
		MaxQualityRung:        "repair_audio_aac_192_stereo",
		DisableClientFallback: true,
	}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-operator",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "direct"},
		CorrelationID: "corr-operator",
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
	if deps.store.putSession.Profile.Name != "repair" {
		t.Fatalf("expected forced repair profile, got %q", deps.store.putSession.Profile.Name)
	}
	operator := deps.store.putSession.PlaybackTrace.Operator
	if operator == nil {
		t.Fatal("expected operator override trace to be persisted")
	}
	if operator.ForcedIntent != "repair" {
		t.Fatalf("expected forced intent repair, got %q", operator.ForcedIntent)
	}
	if operator.MaxQualityRung != "repair_audio_aac_192_stereo" {
		t.Fatalf("expected max quality rung to be persisted, got %q", operator.MaxQualityRung)
	}
	if !operator.ClientFallbackDisabled || !operator.OverrideApplied {
		t.Fatalf("expected operator flags to be applied, got %#v", operator)
	}
	if deps.store.putSession.PlaybackTrace.VideoQualityRung != "repair_video_h264_crf28_veryfast" {
		t.Fatalf("expected repair video rung to be persisted, got %q", deps.store.putSession.PlaybackTrace.VideoQualityRung)
	}
	if deps.store.putSession.PlaybackTrace.QualityRung != "repair_video_h264_crf28_veryfast" {
		t.Fatalf("expected legacy quality rung to follow repair video ladder, got %q", deps.store.putSession.PlaybackTrace.QualityRung)
	}
}

func TestService_ProcessIntent_StartAppliesMatchingSourceRuleOverridesToTraceAndProfile(t *testing.T) {
	deps := newMockDeps()
	disableClientFallback := true
	deps.playbackOperator = config.PlaybackOperatorConfig{
		MaxQualityRung: "compatible_audio_aac_256_stereo",
		SourceRules: []config.PlaybackOperatorRuleConfig{
			{
				Name:                  "problem-channel",
				Mode:                  "live",
				ServiceRef:            "1:0:1:1337:42:99:0:0:0:0:",
				ForceIntent:           "repair",
				DisableClientFallback: &disableClientFallback,
			},
		},
	}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-operator-source",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "direct"},
		CorrelationID: "corr-operator-source",
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
	if deps.store.putSession.Profile.Name != "repair" {
		t.Fatalf("expected source rule to force repair profile, got %q", deps.store.putSession.Profile.Name)
	}
	operator := deps.store.putSession.PlaybackTrace.Operator
	if operator == nil {
		t.Fatal("expected source operator override trace to be persisted")
	}
	if operator.ForcedIntent != "repair" {
		t.Fatalf("expected source rule forced intent repair, got %q", operator.ForcedIntent)
	}
	if operator.MaxQualityRung != "compatible_audio_aac_256_stereo" {
		t.Fatalf("expected inherited global max quality rung, got %q", operator.MaxQualityRung)
	}
	if operator.RuleName != "problem-channel" || operator.RuleScope != "live" {
		t.Fatalf("expected matched source rule metadata, got %#v", operator)
	}
	if !operator.ClientFallbackDisabled || !operator.OverrideApplied {
		t.Fatalf("expected source rule flags to be applied, got %#v", operator)
	}
}

func TestService_ProcessIntent_StartDegradesQualityProfileUnderHostPressure(t *testing.T) {
	deps := newMockDeps()
	deps.hostPressure = playbackprofile.HostPressureAssessment{
		EffectiveBand: playbackprofile.HostPressureConstrained,
	}
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-host-pressure",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "safari_hevc_hw"},
		CorrelationID: "corr-host-pressure",
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
	if deps.store.putSession.Profile.Name != "high" {
		t.Fatalf("expected host pressure to downgrade to high profile, got %q", deps.store.putSession.Profile.Name)
	}
	if deps.store.putSession.PlaybackTrace.ResolvedIntent != "compatible" {
		t.Fatalf("expected compatible resolved intent after host downgrade, got %#v", deps.store.putSession.PlaybackTrace)
	}
	if deps.store.putSession.PlaybackTrace.DegradedFrom != "quality" {
		t.Fatalf("expected degradedFrom=quality after host downgrade, got %#v", deps.store.putSession.PlaybackTrace)
	}
	if deps.store.putSession.PlaybackTrace.HostPressureBand != "constrained" || !deps.store.putSession.PlaybackTrace.HostOverrideApplied {
		t.Fatalf("expected host pressure trace to be persisted, got %#v", deps.store.putSession.PlaybackTrace)
	}
}

func TestService_ProcessIntent_RejectsExplicitHWProfileWithoutVerifiedEncoder(t *testing.T) {
	deps := newMockDeps()
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-hevc-hw-missing",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "safari_hevc_hw"},
		CorrelationID: "corr-hevc-hw-missing",
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		Logger:        zerolog.Nop(),
	})
	if res != nil {
		t.Fatalf("expected nil result, got %#v", res)
	}
	if err == nil || err.Kind != ErrorInvalidInput {
		t.Fatalf("expected ErrorInvalidInput, got %#v", err)
	}
	if deps.store.putSession != nil {
		t.Fatal("expected no session to be persisted when explicit hw profile is unavailable")
	}
}

func TestService_ProcessIntent_RejectsExplicitHWProfileWhenHwaccelOff(t *testing.T) {
	hardware.SetVAAPIEncoderPreflight(map[string]bool{"hevc_vaapi": true})
	hardware.SetVAAPIPreflightResult(true)
	t.Cleanup(func() {
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
	})

	deps := newMockDeps()
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-hevc-hw-off",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "safari_hevc_hw", "hwaccel": "off"},
		CorrelationID: "corr-hevc-hw-off",
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		Logger:        zerolog.Nop(),
	})
	if res != nil {
		t.Fatalf("expected nil result, got %#v", res)
	}
	if err == nil || err.Kind != ErrorInvalidInput {
		t.Fatalf("expected ErrorInvalidInput, got %#v", err)
	}
	if deps.store.putSession != nil {
		t.Fatal("expected no session to be persisted when hwaccel=off conflicts with explicit hw profile")
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
