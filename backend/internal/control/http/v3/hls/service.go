package hls

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/config"
	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/hls/llhls"
	"github.com/ManuGH/xg2g/internal/metrics"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/ManuGH/xg2g/internal/telemetry"
)

var safeHLSSessionIDRouteRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var safeHLSFilenameRouteRe = regexp.MustCompile(`^(?:index\.m3u8|stream(?:_[A-Za-z0-9_-]+)?\.m3u8|init(?:_[A-Za-z0-9_-]+)?\.mp4|seg_[A-Za-z0-9_-]+\.(?:ts|m4s)|stream[A-Za-z0-9_-]*\.ts)$`)

const livePreviewFilename = "preview.jpg"

var llhlsRegistry = sync.OnceValue(func() *llhls.Registry {
	return llhls.NewRegistry(llhls.DefaultPartTargetMs)
})

const llhlsBlockingReloadTimeout = 6 * time.Second

type ProblemWriter func(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any)
type ResponseWrapper func(w http.ResponseWriter) (http.ResponseWriter, any)
type LivePreviewHandler func(w http.ResponseWriter, r *http.Request, root string, segmentSeconds int, ffmpegBin, sessionID string)
type LeaseRenewerFunc func(ctx context.Context, sessionID string)
type SLOTracker interface {
	MarkOutcome(ctx context.Context, sessionID, schema, mode, outcome string)
	MarkMediaSuccess(ctx context.Context, sessionID, schema, mode string)
}

type Service struct {
	cfg                   config.AppConfig
	store                 v3sessions.SessionStore
	storeRegistry         store.StoreRegistry
	writeProblem          ProblemWriter
	wrapResponse          ResponseWrapper
	serveLivePreview      LivePreviewHandler
	renewLease            LeaseRenewerFunc
	slo                   SLOTracker
	playbackStageResolver func(filename string) string
	playbackErrorCode     func(status int) string
}

func NewService(
	cfg config.AppConfig,
	store v3sessions.SessionStore,
	storeRegistry store.StoreRegistry,
	writeProblem ProblemWriter,
	wrapResponse ResponseWrapper,
	serveLivePreview LivePreviewHandler,
	renewLease LeaseRenewerFunc,
	slo SLOTracker,
	playbackStageResolver func(filename string) string,
	playbackErrorCode func(status int) string,
) *Service {
	return &Service{
		cfg:                   cfg,
		store:                 store,
		storeRegistry:         storeRegistry,
		writeProblem:          writeProblem,
		wrapResponse:          wrapResponse,
		serveLivePreview:      serveLivePreview,
		renewLease:            renewLease,
		slo:                   slo,
		playbackStageResolver: playbackStageResolver,
		playbackErrorCode:     playbackErrorCode,
	}
}

func (s *Service) HandleV3HLS(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		if s != nil && s.writeProblem != nil {
			s.writeProblem(w, r, http.StatusServiceUnavailable, "sessions/unavailable", "Service Unavailable", problemcode.CodeV3Unavailable, "The v3 control service is unavailable.", nil)
		}
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")
	if !safeHLSSessionIDRouteRe.MatchString(sessionID) {
		s.writeProblem(w, r, http.StatusBadRequest, "sessions/invalid_id", "Invalid Session ID", problemcode.CodeInvalidSessionID, "The provided session ID contains unsafe characters", nil)
		return
	}

	if filename == livePreviewFilename {
		rec, storeErr := s.store.GetSession(r.Context(), sessionID)
		if storeErr != nil || rec == nil {
			s.writeProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", problemcode.CodeSessionNotFound, "The session could not be located.", nil)
			return
		}
		if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
			s.writeProblem(w, r, http.StatusGone, "sessions/expired", "Session Expired", problemcode.CodeSessionGone, "This session has expired.", nil)
			return
		}
		validState := rec.State == model.SessionReady ||
			rec.State == model.SessionDraining ||
			rec.State == model.SessionStarting ||
			rec.State == model.SessionNew ||
			rec.State == model.SessionPriming
		if !validState {
			if rec.State.IsTerminal() {
				s.writeProblem(w, r, http.StatusGone, "sessions/expired", "Session Ended", problemcode.CodeSessionGone, "stream ended", nil)
				return
			}
			s.writeProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", problemcode.CodeSessionNotFound, "session not ready", nil)
			return
		}
		s.serveLivePreview(w, r, s.cfg.HLS.Root, s.cfg.HLS.SegmentSeconds, s.cfg.FFmpeg.Bin, sessionID)
		return
	}

	if !safeHLSFilenameRouteRe.MatchString(filename) {
		s.writeProblem(w, r, http.StatusForbidden, "sessions/hls_forbidden_artifact", "Access Denied", problemcode.CodeForbidden, "The requested HLS artifact is not allowed", nil)
		return
	}

	if filename == "index.m3u8" || filename == "stream.m3u8" || (strings.HasPrefix(filename, "stream_") && strings.HasSuffix(filename, ".m3u8")) {
		if s.renewLease != nil {
			s.renewLease(r.Context(), sessionID)
		}
	}

	if s.cfg.HLS.LowLatency && filename == "index.m3u8" {
		if s.serveLLHLSPlaylist(w, r, sessionID) {
			return
		}
	}

	stage := "stream"
	if s.playbackStageResolver != nil {
		stage = s.playbackStageResolver(filename)
	}

	if strings.HasPrefix(filename, "seg_") && (strings.HasSuffix(filename, ".m4s") || strings.HasSuffix(filename, ".ts")) {
		telemetry.GetStartupTracer(sessionID).MarkOnce(telemetry.MilestoneH3Req, "first_segment_requested")
	}

	wrapped, tracker := s.wrapResponse(w)
	v3api.ServeHLS(wrapped, r, s.store, s.storeRegistry, s.cfg.HLS.Root, sessionID, filename)

	status := http.StatusOK
	bytesWritten := int64(0)
	type statusCodeGetter interface {
		StatusCode() int
		BytesWritten() int64
	}
	if st, ok := tracker.(statusCodeGetter); ok {
		status = st.StatusCode()
		bytesWritten = st.BytesWritten()
	}

	if r.Method == http.MethodGet && (status == http.StatusOK || status == http.StatusPartialContent) && bytesWritten > 0 {
		if strings.HasPrefix(filename, "seg_") && (strings.HasSuffix(filename, ".m4s") || strings.HasSuffix(filename, ".ts")) {
			telemetry.GetStartupTracer(sessionID).MarkOnce(telemetry.MilestoneH3, "first_segment_served")
		}
	}

	if status >= 400 {
		code := "500"
		if s.playbackErrorCode != nil {
			code = s.playbackErrorCode(status)
		}
		metrics.IncPlaybackError("live", stage, code)
		if s.slo != nil {
			s.slo.MarkOutcome(r.Context(), sessionID, "live", "hls", "failed")
		}
		return
	}

	if s.slo != nil {
		s.slo.MarkMediaSuccess(r.Context(), sessionID, "live", "hls")
	}
}

func (s *Service) serveLLHLSPlaylist(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	rec, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil || rec == nil {
		return false
	}
	if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
		return false
	}
	validState := rec.State == model.SessionReady ||
		rec.State == model.SessionDraining ||
		rec.State == model.SessionStarting ||
		rec.State == model.SessionNew ||
		rec.State == model.SessionPriming
	if !validState {
		return false
	}

	dir := paths.LiveSessionDir(s.cfg.HLS.Root, sessionID)
	tracker := llhlsRegistry().Get(dir, sessionID)

	if !tracker.HasParts() {
		return false
	}

	msn, part := parseBlockingReloadParams(r)
	out, err := tracker.AwaitAndRender(r.Context(), msn, part, time.Now().Add(llhlsBlockingReloadTimeout))
	if err != nil {
		return false
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("X-XG2G-Source", "disk")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Encoding", "identity")
	_, _ = w.Write([]byte(out))
	return true
}

func parseBlockingReloadParams(r *http.Request) (msn, part int) {
	msn, part = -1, -1
	q := r.URL.Query()
	if v := q.Get("_HLS_msn"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			msn = n
		}
	}
	if msn >= 0 {
		if v := q.Get("_HLS_part"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				part = n
			}
		}
	}
	return msn, part
}
