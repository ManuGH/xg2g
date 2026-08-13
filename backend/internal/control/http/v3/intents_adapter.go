package v3

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
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

func (d *serverIntentDeps) CapabilityRegistry() capreg.Store {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.s.capabilityRegistry
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

func (d *serverIntentDeps) HostRuntime(ctx context.Context) playbackprofile.HostRuntimeSnapshot {
	return d.s.currentHostRuntime(ctx)
}

func (d *serverIntentDeps) VerifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode string) bool {
	return d.s.verifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode)
}

func (d *serverIntentDeps) IncLivePlaybackKey(keyLabel, resultLabel string) {
	metrics.IncLiveIntentsPlaybackKey(keyLabel, resultLabel)
}

func (d *serverIntentDeps) HouseholdAdmission() *policy.HouseholdResourceAdmission {
	if d.s.householdAdmission == nil {
		d.s.householdAdmission = policy.NewHouseholdResourceAdmission()
	}
	return d.s.householdAdmission
}

func (d *serverIntentDeps) HouseholdResourcePolicy() *identity.HouseholdResourcePolicy {
	if d.s.householdResourcePolicy == nil {
		return &identity.HouseholdResourcePolicy{
			MaxConcurrentLiveServices: 100,
			MaxConcurrentViewers:      100,
			MaxParallelRecordings:     50,
			MaxParallelTranscodes:     50,
			PreemptionEnabled:         true,
			PreemptionPriorityRanks:   []string{"admin_live", "member_live", "guest_live"},
		}
	}
	return d.s.householdResourcePolicy
}

func (d *serverIntentDeps) ResolveServerIdentity(ctx context.Context, userID, profileID string) (identity.Role, *identity.ProfilePolicy, *identity.AccessPolicy, identity.PolicyDecision, error) {
	idSvc := d.s.getIdentityService()
	if idSvc == nil {
		return identity.RoleAdmin, nil, nil, identity.PolicyDecision{
			Allowed:    true,
			ReasonCode: identity.ReasonCodeAllowed,
		}, nil
	}

	role := identity.RoleMember
	if userID != "" {
		u, err := idSvc.Store().GetUser(ctx, userID)
		if err != nil || u == nil {
			return identity.RoleGuest, nil, nil, identity.PolicyDecision{
				Allowed:    false,
				ReasonCode: identity.ReasonCodeInternalError,
			}, errors.New("user_not_found_or_identity_error")
		}
		role = u.Role
		if mem, mErr := idSvc.Store().GetHouseholdMembership(ctx, "default_household", u.ID); mErr == nil && mem != nil {
			role = mem.Role
		}
	}

	var pol *identity.ProfilePolicy
	if profileID != "" {
		prof, p, pErr := idSvc.Store().GetProfile(ctx, profileID)
		if pErr != nil || prof == nil || prof.HouseholdID != "default_household" {
			return role, nil, nil, identity.PolicyDecision{
				Allowed:    false,
				ReasonCode: "profile_access_denied",
			}, errors.New("profile_access_denied")
		}
		pol = p
	}

	access, _ := idSvc.Store().GetAccessPolicy(ctx, userID)
	resPolicy := d.s.householdResourcePolicy
	if resPolicy == nil {
		resPolicy, _ = idSvc.GetHouseholdResourcePolicy(ctx, "default_household")
	}

	dec := identity.EvaluatePolicyDecision(userID, role, pol, access, resPolicy, time.Now())
	return role, pol, access, dec, nil
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
