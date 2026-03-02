package v3

import (
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
)

const (
	playbackSchemaRecordingLabel = "recording"
	playbackSchemaLiveLabel      = "live"

	playbackModeHLSLabel       = "hls"
	playbackModeNativeHLSLabel = "native_hls"
	playbackModeHLSJSLabel     = "hlsjs"
	playbackModeMP4Label       = "mp4"
	playbackModeUnknownLabel   = "unknown"

	playbackStagePlaybackInfoLabel = "playback_info"
	playbackStageIntentLabel       = "intent"
	playbackStagePlaylistLabel     = "playlist"
	playbackStageSegmentLabel      = "segment"
	playbackStageStreamLabel       = "stream"
)

const (
	defaultPlaybackSLOSessionTTL = 20 * time.Minute
	// SSOT rebuffer thresholds used for metric severity classification.
	// Intentional: same thresholds for live and recording.
	// Reason: the proxy signal is request-gap based (server-side only) and must stay
	// directly comparable across schemas; schema-specific tuning is deferred until we
	// have reliable per-session target-duration telemetry.
	playbackRebufferMinorGap = 12 * time.Second
	playbackRebufferMajorGap = 24 * time.Second
)

type playbackSessionMeta struct {
	SessionID   string
	Schema      string
	Mode        string
	RecordingID string
	ServiceRef  string
}

type playbackMediaObservation struct {
	Schema           string
	Mode             string
	RecordingID      string
	ServiceRef       string
	TTFFObserved     bool
	TTFFSeconds      float64
	RebufferSeverity string
}

type playbackOutcomeObservation struct {
	Schema       string
	Mode         string
	RecordingID  string
	ServiceRef   string
	TTFFObserved bool
	TTFFSeconds  float64
	Outcome      string
}

type playbackSessionState struct {
	Schema      string
	Mode        string
	RecordingID string
	ServiceRef  string
	StartedAt   time.Time
	FirstMedia  time.Time
	LastMedia   time.Time
}

type playbackSessionTracker struct {
	mu       sync.Mutex
	ttl      time.Duration
	nowFn    func() time.Time
	sessions map[string]playbackSessionState
}

func newPlaybackSessionTracker(ttl time.Duration) *playbackSessionTracker {
	if ttl <= 0 {
		ttl = defaultPlaybackSLOSessionTTL
	}
	return &playbackSessionTracker{
		ttl:      ttl,
		nowFn:    time.Now,
		sessions: make(map[string]playbackSessionState),
	}
}

func (t *playbackSessionTracker) Start(meta playbackSessionMeta) {
	sessionID := strings.TrimSpace(meta.SessionID)
	if sessionID == "" {
		return
	}
	now := t.nowFn()
	t.mu.Lock()
	t.pruneLocked(now)
	if existing, ok := t.sessions[sessionID]; ok {
		t.sessions[sessionID] = t.mergeStateMeta(existing, meta)
		t.mu.Unlock()
		return
	}
	state := playbackSessionState{
		Schema:      normalizePlaybackSchema(meta.Schema),
		Mode:        normalizePlaybackMode(meta.Mode),
		RecordingID: strings.TrimSpace(meta.RecordingID),
		ServiceRef:  strings.TrimSpace(meta.ServiceRef),
		StartedAt:   now,
	}
	t.sessions[sessionID] = state
	t.mu.Unlock()

	metrics.IncPlaybackStart(state.Schema, state.Mode)
}

func (t *playbackSessionTracker) MarkMediaSuccess(meta playbackSessionMeta) playbackMediaObservation {
	sessionID := strings.TrimSpace(meta.SessionID)
	if sessionID == "" {
		return playbackMediaObservation{
			Schema:      normalizePlaybackSchema(meta.Schema),
			Mode:        normalizePlaybackMode(meta.Mode),
			RecordingID: strings.TrimSpace(meta.RecordingID),
			ServiceRef:  strings.TrimSpace(meta.ServiceRef),
		}
	}

	now := t.nowFn()
	t.mu.Lock()
	t.pruneLocked(now)
	state, ok := t.sessions[sessionID]
	if !ok {
		state = playbackSessionState{
			Schema:      normalizePlaybackSchema(meta.Schema),
			Mode:        normalizePlaybackMode(meta.Mode),
			RecordingID: strings.TrimSpace(meta.RecordingID),
			ServiceRef:  strings.TrimSpace(meta.ServiceRef),
			StartedAt:   now,
			FirstMedia:  now,
			LastMedia:   now,
		}
		t.sessions[sessionID] = state
		t.mu.Unlock()
		return playbackMediaObservation{
			Schema:      state.Schema,
			Mode:        state.Mode,
			RecordingID: state.RecordingID,
			ServiceRef:  state.ServiceRef,
		}
	}

	state = t.mergeStateMeta(state, meta)
	out := playbackMediaObservation{
		Schema:      state.Schema,
		Mode:        state.Mode,
		RecordingID: state.RecordingID,
		ServiceRef:  state.ServiceRef,
	}
	if state.FirstMedia.IsZero() {
		state.FirstMedia = now
		ttff := now.Sub(state.StartedAt).Seconds()
		if ttff < 0 {
			ttff = 0
		}
		out.TTFFObserved = true
		out.TTFFSeconds = ttff
		metrics.ObservePlaybackTTFF(state.Schema, state.Mode, "ok", ttff)
	}

	if !state.LastMedia.IsZero() {
		gap := now.Sub(state.LastMedia)
		if severity := classifyPlaybackRebufferSeverity(gap); severity != "" {
			out.RebufferSeverity = severity
			metrics.IncPlaybackRebuffer(state.Schema, state.Mode, severity)
		}
	}
	state.LastMedia = now
	t.sessions[sessionID] = state
	t.mu.Unlock()
	return out
}

func (t *playbackSessionTracker) MarkOutcome(meta playbackSessionMeta, outcome string) playbackOutcomeObservation {
	sessionID := strings.TrimSpace(meta.SessionID)
	if sessionID == "" {
		return playbackOutcomeObservation{
			Schema:      normalizePlaybackSchema(meta.Schema),
			Mode:        normalizePlaybackMode(meta.Mode),
			RecordingID: strings.TrimSpace(meta.RecordingID),
			ServiceRef:  strings.TrimSpace(meta.ServiceRef),
			Outcome:     strings.ToLower(strings.TrimSpace(outcome)),
		}
	}

	now := t.nowFn()
	t.mu.Lock()
	t.pruneLocked(now)
	state, ok := t.sessions[sessionID]
	if !ok {
		t.mu.Unlock()
		return playbackOutcomeObservation{
			Schema:      normalizePlaybackSchema(meta.Schema),
			Mode:        normalizePlaybackMode(meta.Mode),
			RecordingID: strings.TrimSpace(meta.RecordingID),
			ServiceRef:  strings.TrimSpace(meta.ServiceRef),
			Outcome:     strings.ToLower(strings.TrimSpace(outcome)),
		}
	}
	state = t.mergeStateMeta(state, meta)
	delete(t.sessions, sessionID)
	t.mu.Unlock()

	out := playbackOutcomeObservation{
		Schema:      state.Schema,
		Mode:        state.Mode,
		RecordingID: state.RecordingID,
		ServiceRef:  state.ServiceRef,
		Outcome:     strings.ToLower(strings.TrimSpace(outcome)),
	}
	if state.FirstMedia.IsZero() {
		ttff := now.Sub(state.StartedAt).Seconds()
		if ttff < 0 {
			ttff = 0
		}
		out.TTFFObserved = true
		out.TTFFSeconds = ttff
		metrics.ObservePlaybackTTFF(state.Schema, state.Mode, out.Outcome, ttff)
	}
	return out
}

func (t *playbackSessionTracker) mergeStateMeta(state playbackSessionState, meta playbackSessionMeta) playbackSessionState {
	if state.Schema == "unknown" {
		state.Schema = normalizePlaybackSchema(meta.Schema)
	}
	if state.Mode == playbackModeUnknownLabel {
		state.Mode = normalizePlaybackMode(meta.Mode)
	}
	if state.RecordingID == "" {
		state.RecordingID = strings.TrimSpace(meta.RecordingID)
	}
	if state.ServiceRef == "" {
		state.ServiceRef = strings.TrimSpace(meta.ServiceRef)
	}
	return state
}

func (t *playbackSessionTracker) pruneLocked(now time.Time) {
	for id, st := range t.sessions {
		latest := st.StartedAt
		if st.LastMedia.After(latest) {
			latest = st.LastMedia
		}
		if st.FirstMedia.After(latest) {
			latest = st.FirstMedia
		}
		if now.Sub(latest) > t.ttl {
			delete(t.sessions, id)
		}
	}
}

func normalizePlaybackSchema(schema string) string {
	switch strings.ToLower(strings.TrimSpace(schema)) {
	case playbackSchemaRecordingLabel, playbackSchemaLiveLabel:
		return strings.ToLower(strings.TrimSpace(schema))
	default:
		return "unknown"
	}
}

func normalizePlaybackMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case playbackModeHLSLabel, playbackModeNativeHLSLabel, playbackModeHLSJSLabel, playbackModeMP4Label:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return playbackModeUnknownLabel
	}
}

func playbackModeLabelFromPlaybackInfoMode(mode PlaybackInfoMode) string {
	switch mode {
	case PlaybackInfoModeDirectMp4:
		return playbackModeMP4Label
	case PlaybackInfoModeNativeHls:
		return playbackModeNativeHLSLabel
	case PlaybackInfoModeHlsjs:
		return playbackModeHLSJSLabel
	case PlaybackInfoModeTranscode:
		return playbackModeHLSLabel
	default:
		return playbackModeUnknownLabel
	}
}

func playbackModeLabelFromIntentPlaybackMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "direct_mp4":
		return playbackModeMP4Label
	case "native_hls":
		return playbackModeNativeHLSLabel
	case "hlsjs":
		return playbackModeHLSJSLabel
	case "transcode":
		return playbackModeHLSLabel
	default:
		return playbackModeUnknownLabel
	}
}

func playbackStageLabelFromLiveFilename(filename string) string {
	name := strings.ToLower(strings.TrimSpace(filename))
	if strings.HasSuffix(name, ".m3u8") {
		return playbackStagePlaylistLabel
	}
	return playbackStageSegmentLabel
}

func playbackErrorCodeFromStatus(status int) string {
	switch status {
	case 400:
		return "INVALID_INPUT"
	case 401:
		return "UNAUTHORIZED"
	case 403:
		return "FORBIDDEN"
	case 404:
		return "NOT_FOUND"
	case 410:
		return "SESSION_GONE"
	case 416:
		return "INVALID_INPUT"
	case 503:
		return "SERVICE_UNAVAILABLE"
	default:
		if status >= 500 {
			return "INTERNAL_ERROR"
		}
		return "UNKNOWN"
	}
}

func classifyPlaybackRebufferSeverity(gap time.Duration) string {
	switch {
	case gap >= playbackRebufferMajorGap:
		return "major"
	case gap >= playbackRebufferMinorGap:
		return "minor"
	default:
		return ""
	}
}
