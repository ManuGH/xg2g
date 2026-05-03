package intents

import (
	"context"
	"reflect"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
)

func resolveReusableLiveStart(ctx context.Context, store SessionStore, intent Intent, session *model.SessionRecord) (*Result, error) {
	if !strings.EqualFold(strings.TrimSpace(intent.Mode), model.ModeLive) {
		return nil, nil
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	match := findReusableLiveSession(intent, session, sessions)
	if match == nil {
		return nil, nil
	}

	correlationID := strings.TrimSpace(match.CorrelationID)
	if correlationID == "" && match.ContextData != nil {
		correlationID = strings.TrimSpace(match.ContextData["correlationId"])
	}
	if correlationID == "" {
		correlationID = intent.CorrelationID
	}

	return &Result{
		SessionID:     match.SessionID,
		Status:        "idempotent_replay",
		CorrelationID: correlationID,
	}, nil
}

func findReusableLiveSession(intent Intent, session *model.SessionRecord, sessions []*model.SessionRecord) *model.SessionRecord {
	var best *model.SessionRecord
	for _, candidate := range sessions {
		if !isReusableLiveSessionCandidate(intent, session, candidate) {
			continue
		}
		if best == nil || reusableLiveSessionPriority(candidate) > reusableLiveSessionPriority(best) {
			best = candidate
		}
	}
	return best
}

func isReusableLiveSessionCandidate(intent Intent, session, candidate *model.SessionRecord) bool {
	if candidate == nil || candidate.SessionID == "" {
		return false
	}
	if candidate.SessionID == intent.SessionID {
		return true
	}
	if candidate.State.IsTerminal() {
		return false
	}
	switch candidate.State {
	case model.SessionDraining, model.SessionStopping:
		return false
	}
	if normalize.ServiceRef(candidate.ServiceRef) != normalize.ServiceRef(intent.ServiceRef) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(sessionContextValue(candidate, model.CtxKeyMode)), model.ModeLive) {
		return false
	}
	if !reusableLiveClientPathCompatible(session, candidate) {
		return false
	}
	if !matchSessionIdentity(intent, session, candidate) {
		return false
	}
	return true
}

func reusableLiveClientPathCompatible(session, candidate *model.SessionRecord) bool {
	requestPath := normalize.Token(sessionContextValue(session, model.CtxKeyClientPath))
	candidatePath := normalize.Token(sessionContextValue(candidate, model.CtxKeyClientPath))
	if requestPath == candidatePath {
		return true
	}
	if requestPath == "" || candidatePath == "" {
		return false
	}
	return reusableLiveProfilesEquivalent(session, candidate)
}

func reusableLiveProfilesEquivalent(session, candidate *model.SessionRecord) bool {
	if session == nil || candidate == nil {
		return false
	}
	return reflect.DeepEqual(session.Profile, candidate.Profile)
}

func matchSessionIdentity(intent Intent, session, candidate *model.SessionRecord) bool {
	intentPrincipal := normalize.Token(intent.PrincipalID)
	candidatePrincipal := normalize.Token(sessionContextValue(candidate, model.CtxKeyPrincipalID))
	principalMatched := false
	if intentPrincipal != "" || candidatePrincipal != "" {
		if intentPrincipal == "" || candidatePrincipal == "" || intentPrincipal != candidatePrincipal {
			return false
		}
		principalMatched = true
	}

	intentCapHash := normalize.Token(clientCapHashForIntent(intent))
	candidateCapHash := normalize.Token(candidateCapHash(candidate))
	if intentCapHash != "" || candidateCapHash != "" {
		if intentCapHash == "" || candidateCapHash == "" || intentCapHash != candidateCapHash {
			if principalMatched && reusableLiveProfilesEquivalent(session, candidate) {
				return true
			}
			return false
		}
	}

	if intentPrincipal == "" && candidatePrincipal == "" && intentCapHash == "" && candidateCapHash == "" {
		intentFamily := normalize.Token(clientFamilyForIntent(intent))
		candidateFamily := normalize.Token(candidateClientFamily(candidate))
		if intentFamily != "" || candidateFamily != "" {
			if intentFamily == "" || candidateFamily == "" || intentFamily != candidateFamily {
				return false
			}
		}
		intentDeviceType := normalize.Token(deviceTypeForIntent(intent))
		candidateDeviceType := normalize.Token(candidateDeviceType(candidate))
		if intentDeviceType != "" || candidateDeviceType != "" {
			if intentDeviceType == "" || candidateDeviceType == "" || intentDeviceType != candidateDeviceType {
				return false
			}
		}
	}

	return true
}

func sessionContextValue(session *model.SessionRecord, key string) string {
	if session == nil || session.ContextData == nil {
		return ""
	}
	return strings.TrimSpace(session.ContextData[key])
}

func candidateCapHash(session *model.SessionRecord) string {
	if capHash := sessionContextValue(session, "capHash"); capHash != "" {
		return capHash
	}
	if session != nil && session.PlaybackTrace != nil && session.PlaybackTrace.Client != nil {
		return strings.TrimSpace(session.PlaybackTrace.Client.CapHash)
	}
	return ""
}

func candidateClientFamily(session *model.SessionRecord) string {
	if family := sessionContextValue(session, model.CtxKeyClientFamily); family != "" {
		return family
	}
	if session != nil && session.PlaybackTrace != nil && session.PlaybackTrace.Client != nil {
		return strings.TrimSpace(session.PlaybackTrace.Client.ClientFamily)
	}
	return ""
}

func candidateDeviceType(session *model.SessionRecord) string {
	if deviceType := sessionContextValue(session, model.CtxKeyDeviceType); deviceType != "" {
		return deviceType
	}
	if session != nil && session.PlaybackTrace != nil && session.PlaybackTrace.Client != nil {
		return strings.TrimSpace(session.PlaybackTrace.Client.DeviceType)
	}
	return ""
}

func reusableLiveSessionPriority(session *model.SessionRecord) int64 {
	if session == nil {
		return -1
	}
	score := int64(session.UpdatedAtUnix)
	switch session.State {
	case model.SessionReady:
		score += 1_000_000_000_000
	case model.SessionPriming:
		score += 900_000_000_000
	case model.SessionStarting:
		score += 800_000_000_000
	case model.SessionNew:
		score += 700_000_000_000
	}
	return score
}
