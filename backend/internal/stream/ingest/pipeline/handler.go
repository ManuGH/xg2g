// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ingeststats"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/ingest/variant"
)

// primedAttachTimeout bounds the wait for the first indexable keyframe after a cold session
// start. Measured against the production receiver (VU+ Uno4K, Astra 19.2E, ORF1 HD): 2.23s
// time-to-first-byte cold, of which roughly 1.05s is the OSCam ECM round-trip; a warm shared
// session attaches in 0.025s. 5s leaves margin for tuner contention without turning a
// permanently unattachable stream into a long stall - terminal conditions return early.
const primedAttachTimeout = 5 * time.Second

// zapIDHeader carries the client's identifier for one channel change.
const zapIDHeader = "X-Xg2g-Zap-Id"

// clientAudioCodecsHeader carries the client's supported audio codecs (e.g. "aac,ac3,eac3").
const clientAudioCodecsHeader = "X-Client-Audio-Codecs"

// clientVideoCodecsHeader carries the client's supported video codecs (e.g. "h264,hevc").
const clientVideoCodecsHeader = "X-Client-Video-Codecs"

func selectAudioTrack(tracks []ring.AudioTrackInfo, prefPID uint16, prefLang string) ring.AudioTrackInfo {
	if len(tracks) == 0 {
		return ring.AudioTrackInfo{}
	}
	if prefPID > 0 {
		for _, t := range tracks {
			if t.PID == prefPID {
				return t
			}
		}
	}
	if prefLang != "" {
		for _, t := range tracks {
			if strings.EqualFold(t.Language, prefLang) {
				return t
			}
		}
	}
	return tracks[0]
}

func clientSupportsCodec(supportedHeader string, codec string) bool {
	if supportedHeader == "" {
		return true // Default direct if client reports no capability constraint
	}
	parts := strings.Split(supportedHeader, ",")
	for _, p := range parts {
		if strings.EqualFold(strings.TrimSpace(p), codec) {
			return true
		}
	}
	return false
}

// readinessObservationBudget bounds how long one ingest is watched. Comfortably past
// the slowest tune measured against the reference receiver (2.3 s to PAT, 3.0 s to a
// usable entry point) so a slow but successful channel is still recorded as ready.
const readinessObservationBudget = 20 * time.Second

// maxZapIDLength caps what is accepted from the client. The identifier only has to
// be unique within a client session; anything longer is not one.
const maxZapIDLength = 64

// sanitizeZapID keeps the client's identifier to characters that are safe to put in
// a log field, and substitutes a marker when the client sent none. Untrusted input
// never reaches a metric label, so cardinality is not at stake - legibility is.
func sanitizeZapID(raw string) string {
	if raw == "" {
		return "unset"
	}
	if len(raw) > maxZapIDLength {
		raw = raw[:maxZapIDLength]
	}
	cleaned := make([]rune, 0, len(raw))
	for _, c := range raw {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			cleaned = append(cleaned, c)
		case c == '-', c == '_', c == '.', c == ':':
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) == 0 {
		return "invalid"
	}
	return string(cleaned)
}

// Handler serves live, paced MPEG-TS streams via HTTP using the unified Live Ingest Pipeline.
type Handler struct {
	manager      *session.Manager
	receiverHost string
	streamPort   int
}

// NewHandler creates an HTTP handler bound to the session.Manager.
func NewHandler(manager *session.Manager) *Handler {
	return NewHandlerWithReceiver(manager, "10.10.55.64", 8001)
}

// NewHandlerWithReceiver creates an HTTP handler with explicit receiver host/port defaults.
func NewHandlerWithReceiver(manager *session.Manager, receiverHost string, streamPort int) *Handler {
	if receiverHost == "" {
		receiverHost = "10.10.55.64"
	}
	if streamPort <= 0 {
		streamPort = 8001
	}
	return &Handler{
		manager:      manager,
		receiverHost: receiverHost,
		streamPort:   streamPort,
	}
}

// ServeHTTP handles GET /api/v3/stream/live/* requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	const prefix = "/api/v3/stream/live/"
	serviceRef := strings.TrimPrefix(path, prefix)
	if serviceRef == "" || serviceRef == path {
		serviceRef = r.URL.Query().Get("sref")
	}

	if serviceRef == "" {
		http.Error(w, "missing serviceRef in stream path", http.StatusBadRequest)
		return
	}

	if unescaped, err := url.PathUnescape(serviceRef); err == nil {
		serviceRef = unescaped
	}

	key := session.NewSessionKey(h.receiverHost, h.streamPort, serviceRef)
	parts := strings.Split(serviceRef, ":")
	if len(parts) >= 4 {
		if val, err := strconv.ParseUint(parts[3], 16, 16); err == nil && val > 0 {
			key.TargetProgram = uint16(val)
		}
	}
	if err := key.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid serviceRef: %v", err), http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by response writer", http.StatusInternalServerError)
		return
	}

	// The client stamps each channel change with an identifier so one zap can be
	// followed across both sides of the wire. It is a log field only, never a metric
	// label: one series per zap would blow up cardinality for no gain.
	zapID := sanitizeZapID(r.Header.Get(zapIDHeader))

	logger := log.L().With().
		Str("serviceRef", key.ServiceRef).
		Uint16("targetProgram", key.TargetProgram).
		Str("zap_id", zapID).
		Logger()

	requestedAt := time.Now()
	logger.Info().Str("event", "zap.request").Msg("live stream requested")

	// Acquire or coalesce live session lease
	lease, err := h.manager.Acquire(r.Context(), key)
	if err != nil {
		logger.Warn().Err(err).
			Dur("after", time.Since(requestedAt)).
			Str("event", "zap.failed").
			Str("stage", "acquire").
			Msg("failed to acquire live ingest lease")
		http.Error(w, fmt.Sprintf("upstream stream unavailable: %v", err), http.StatusBadGateway)
		return
	}
	defer lease.Release()

	logger.Info().
		Dur("after", time.Since(requestedAt)).
		Str("event", "zap.session").
		Str("state", string(lease.State())).
		Msg("live ingest session acquired")

	pipelinePayload := lease.Session().Payload()
	if pipelinePayload == nil {
		logger.Error().Msg("active session holds nil pipeline payload")
		http.Error(w, "internal pipeline error", http.StatusInternalServerError)
		return
	}

	pipe, ok := pipelinePayload.(*SessionPipeline)
	if !ok {
		logger.Error().Msg("invalid pipeline payload type in session")
		http.Error(w, "internal pipeline type error", http.StatusInternalServerError)
		return
	}

	// Observation only: this watches the same ingest the request is being served
	// from and records when each readiness criterion becomes true. Nothing below
	// waits on it, and the stream is served on exactly the schedule it was before.
	pipe.ObserveOnce(func() {
		observerLogger := logger
		go ObserveReadiness(context.WithoutCancel(r.Context()), pipe, observerLogger, readinessObservationBudget)
	})

	// Capture atomic PrimedAttachPoint and dedicated subscriber reader (waits for first keyframe if stream just started)
	attach, reader, err := pipe.PrimedAttachWithTimeout(r.Context(), primedAttachTimeout)
	if err != nil {
		runErr := pipe.Err()
		if errors.Is(err, ring.ErrScrambledStream) {
			scrambled, clear := pipe.MasterRing().ScramblingObservation()
			logger.Error().
				Uint64("scrambledPackets", scrambled).
				Uint64("clearPackets", clear).
				Msg("upstream video is scrambled: receiver is not descrambling this service (check softcam/CI on the receiver)")
			http.Error(w, "upstream stream is scrambled: the receiver is not descrambling this service", http.StatusBadGateway)
			return
		}
		logger.Warn().Err(err).AnErr("runErr", runErr).
			Dur("after", time.Since(requestedAt)).
			Str("event", "zap.failed").
			Str("stage", "attach").
			Msg("failed to perform primed attach to stream")
		http.Error(w, fmt.Sprintf("failed to attach to live stream: %v (runErr: %v)", err, runErr), http.StatusBadGateway)
		return
	}

	// Evaluate Client Video & Audio Capabilities
	clientVideoCodecs := r.Header.Get(clientVideoCodecsHeader)
	clientAudioCodecs := r.Header.Get(clientAudioCodecsHeader)
	if clientVideoCodecs == "" && clientAudioCodecs != "" {
		// Sterling/iOS client evidence: supports hardware H.264 & HEVC
		clientVideoCodecs = "h264,hevc"
	}

	audioOverride := r.URL.Query().Get("audio")
	prefLang := r.URL.Query().Get("lang")
	var prefPID uint16
	if pidStr := r.URL.Query().Get("audio_pid"); pidStr != "" {
		if val, err := strconv.ParseUint(pidStr, 10, 16); err == nil {
			prefPID = uint16(val)
		}
	}

	facts := pipe.MasterRing().ReadinessFacts()
	var releaseVariant func()
	defer func() {
		if releaseVariant != nil {
			releaseVariant()
		}
	}()

	// 1. Video Compatibility Evaluation
	needsVideoTranscode := false
	targetVideoCodec := "copy"
	scanPolicy := "passthrough"
	srcVideoCodec := string(facts.VideoCodec)
	if srcVideoCodec != "" && clientVideoCodecs != "" {
		if !clientSupportsCodec(clientVideoCodecs, srcVideoCodec) {
			needsVideoTranscode = true
			targetVideoCodec = "h264"
			scanPolicy = "deinterlace_50p"
		}
	}

	// 2. Audio Compatibility Evaluation
	needsAudioTranscode := false
	targetAudioCodec := "copy"
	var selectedTrack ring.AudioTrackInfo
	if len(facts.AudioTracks) > 0 {
		selectedTrack = selectAudioTrack(facts.AudioTracks, prefPID, prefLang)
		if audioOverride == "aac" {
			needsAudioTranscode = true
			targetAudioCodec = "aac"
		} else if clientAudioCodecs != "" && selectedTrack.Codec != "" {
			if !clientSupportsCodec(clientAudioCodecs, selectedTrack.Codec) {
				needsAudioTranscode = true
				targetAudioCodec = "aac"
			}
		}
	}

	if needsVideoTranscode || needsAudioTranscode {
		logger.Info().
			Str("srcVideo", srcVideoCodec).
			Str("targetVideo", targetVideoCodec).
			Str("scanPolicy", scanPolicy).
			Uint16("srcAudioPID", selectedTrack.PID).
			Str("srcAudioCodec", selectedTrack.Codec).
			Str("targetAudio", targetAudioCodec).
			Str("lang", selectedTrack.Language).
			Msg("client capability gap detected: attaching to shared stream variant worker")

		variantKey := variant.StreamVariantKey{
			StreamFingerprint: fmt.Sprintf("prog%d-gen%d-v%d", key.TargetProgram, facts.Generation, facts.PMTVersion),
			ProgramNumber:     key.TargetProgram,
			SourceVideoCodec:  srcVideoCodec,
			TargetVideoCodec:  targetVideoCodec,
			ScanPolicy:        scanPolicy,
			AudioPID:          selectedTrack.PID,
			SourceAudioCodec:  selectedTrack.Codec,
			TargetAudioCodec:  targetAudioCodec,
			Language:          selectedTrack.Language,
			Role:              "main",
			SampleRate:        48000,
			Channels:          2,
			BitrateKbps:       192,
		}

		varAttach, varReader, release, varErr := pipe.AttachAudioVariantWithTimeout(r.Context(), variantKey, primedAttachTimeout)
		if varErr == nil {
			_ = reader.Close()
			reader = varReader
			attach = varAttach
			releaseVariant = release
		} else {
			logger.Warn().Err(varErr).Msg("failed to attach to stream variant; falling back to direct master stream")
		}
	}
	defer func() { _ = reader.Close() }()

	// Which ring this client ended up on decides how its lag has to be read: on the
	// master it is the receiver's input backing up, on a variant ring it is the
	// transcoder's output. The two share no series.
	subscriberRole := ingeststats.RoleNativeClient
	if releaseVariant != nil {
		subscriberRole = ingeststats.RoleVariantClient
	}
	sampler := ingeststats.NewSubscriberSampler(subscriberRole, reader)
	defer sampler.Flush()

	// Runtime Plan Transparency Headers
	//
	// They describe what is actually being sent, not what was decided. Attaching to
	// a variant can fail - no transcoder, a worker that cannot start - and this
	// request then falls back to the master stream unchanged. A header still
	// claiming "transcode" would leave the client expecting AAC on a stream that is
	// still carrying MP2, which is the one thing these headers exist to prevent.
	servedFromVariant := releaseVariant != nil

	videoMode := "direct"
	effectiveVideoCodec := srcVideoCodec
	if needsVideoTranscode && servedFromVariant {
		videoMode = "transcode"
		effectiveVideoCodec = targetVideoCodec
	}
	if effectiveVideoCodec == "" {
		effectiveVideoCodec = "unknown"
	}

	// The scan policy is a property of the transcode, so a passthrough stream has
	// none to report either.
	effectiveScanPolicy := scanPolicy
	if !servedFromVariant {
		effectiveScanPolicy = "passthrough"
	}

	audioMode := "direct"
	effectiveAudioCodec := selectedTrack.Codec
	if needsAudioTranscode && servedFromVariant {
		audioMode = "transcode"
		effectiveAudioCodec = targetAudioCodec
	}
	if effectiveAudioCodec == "" {
		effectiveAudioCodec = "unknown"
	}

	w.Header().Set("X-Xg2g-Video-Mode", videoMode)
	w.Header().Set("X-Xg2g-Video-Source", srcVideoCodec)
	w.Header().Set("X-Xg2g-Video-Effective", effectiveVideoCodec)
	w.Header().Set("X-Xg2g-Scan-Policy", effectiveScanPolicy)
	w.Header().Set("X-Xg2g-Audio-Mode", audioMode)
	w.Header().Set("X-Xg2g-Audio-Source", selectedTrack.Codec)
	w.Header().Set("X-Xg2g-Audio-Effective", effectiveAudioCodec)
	if selectedTrack.Language != "" {
		w.Header().Set("X-Xg2g-Audio-Language", selectedTrack.Language)
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	logger.Info().
		Dur("after", time.Since(requestedAt)).
		Uint64("generation", attach.Generation).
		Int("preamble_bytes", len(attach.Preamble)).
		Str("event", "zap.attached").
		Msg("attached to live stream and answered")

	// 1. Deliver authoritative PAT/PMT preamble first
	if len(attach.Preamble) > 0 {
		if _, err := w.Write(attach.Preamble); err != nil {
			return
		}
		flusher.Flush()
	}

	// 2. Stream time-regulated packets from subscriber reader
	chunk := make([]byte, 32*1024)
	for {
		if err := r.Context().Err(); err != nil {
			return
		}

		n, err := reader.Read(chunk)
		sampler.Sample()
		if n > 0 {
			if _, writeErr := w.Write(chunk[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}

		if err != nil {
			if err == io.EOF {
				return
			}
			logger.Debug().Err(err).Msg("subscriber reader exited")
			return
		}
	}
}
