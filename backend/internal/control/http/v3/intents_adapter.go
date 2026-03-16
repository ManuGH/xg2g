package v3

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/metrics"
)

type serverIntentDeps struct {
	s *Server
}

var _ v3intents.Deps = (*serverIntentDeps)(nil)

func (d *serverIntentDeps) DVRWindow() time.Duration {
	return d.s.GetConfig().HLS.DVRWindow
}

func (d *serverIntentDeps) HasTunerSlots() bool {
	return len(d.s.GetConfig().Engine.TunerSlots) > 0
}

func (d *serverIntentDeps) SessionLeaseTTL() time.Duration {
	return d.s.GetConfig().Sessions.LeaseTTL
}

func (d *serverIntentDeps) SessionHeartbeatInterval() time.Duration {
	return d.s.GetConfig().Sessions.HeartbeatInterval
}

func (d *serverIntentDeps) PlaybackOperator() config.PlaybackOperatorConfig {
	return d.s.GetConfig().Playback.Operator
}

func (d *serverIntentDeps) SessionStore() v3intents.SessionStore {
	return d.s.sessionsModuleDeps().store
}

func (d *serverIntentDeps) EventBus() v3intents.EventBus {
	return d.s.sessionsModuleDeps().bus
}

func (d *serverIntentDeps) ChannelScanner() v3intents.ChannelScanner {
	return d.s.sessionsModuleDeps().channelScanner
}

func (d *serverIntentDeps) AdmissionController() v3intents.AdmissionController {
	return d.s.sessionsModuleDeps().admission
}

func (d *serverIntentDeps) AdmissionRuntimeState(ctx context.Context) admission.RuntimeState {
	deps := d.s.sessionsModuleDeps()
	return CollectRuntimeState(ctx, deps.admissionState)
}

func (d *serverIntentDeps) HostPressure(ctx context.Context) playbackprofile.HostPressureAssessment {
	return d.s.currentHostPressure(ctx)
}

func (d *serverIntentDeps) VerifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode string) bool {
	return d.s.verifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode)
}

func (d *serverIntentDeps) IncLivePlaybackKey(keyLabel, resultLabel string) {
	metrics.IncLiveIntentsPlaybackKey(keyLabel, resultLabel)
}

func (d *serverIntentDeps) RecordReject(code string) {
	metrics.RecordReject(code, "live")
}

func (d *serverIntentDeps) RecordAdmit() {
	metrics.RecordAdmit("live")
}

func (d *serverIntentDeps) RecordIntent(intentType, mode, outcome string) {
	RecordV3Intent(intentType, mode, outcome)
}

func (d *serverIntentDeps) RecordPublish(eventType, outcome string) {
	RecordV3Publish(eventType, outcome)
}

func (d *serverIntentDeps) RecordReplay(intentType string) {
	RecordV3Replay(intentType)
}
