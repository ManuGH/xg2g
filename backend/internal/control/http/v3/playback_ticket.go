// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// A playback ticket is the credential a *media player* uses, and nothing else.
//
// # Why this exists at all
//
// A native player is a separate HTTP client that fetches the playlist and every
// segment on its own. It cannot participate in proof of possession: there is no
// point at which application code could attach a fresh DPoP proof to a segment
// request AVFoundation issues internally. Treating the player as if it were the
// API client leads to one of two bad ends — the API credential ends up in a URL
// (and therefore in access logs, referrers and player caches), or the app
// intercepts every segment through a resource loader, which is fragile across
// ABR, AirPlay and PiP and would push one replay-cache entry per segment.
//
// So playback gets its own credential class, deliberately weaker and
// deliberately narrower than the API credential:
//
//   - It names exactly one session. A ticket for session A is refused for
//     session B, and it dies when that session does.
//   - It carries read scope only, and it is accepted **solely** on media routes.
//     Presenting it to any API endpoint authenticates nothing.
//   - It travels as a cookie scoped to that one session's HLS path, so it is
//     never attached to another request — and never appears in a URL.
//
// This does not weaken the existing rule that media requires a cookie. It
// extends who may mint one: previously only a browser login could, which is why
// the Android client ended up borrowing its WebView's cookie, and why a native
// client with no WebView could not play at all.
const playbackTicketCookieName = "xg2g_playback"

// Long enough for a feature film without a mid-playback interruption. The real
// bound is the session: when it ends, its HLS directory goes with it, and the
// ticket names nothing that exists.
const playbackTicketTTL = 4 * time.Hour

type playbackTicket struct {
	sessionID string
	principal string
	expiresAt time.Time
}

// playbackTicketStore keeps tickets in process memory.
//
// Not persisted, on purpose: a ticket is only meaningful while its session is
// streaming, and a restart ends every session anyway. Persisting them would
// create credentials that outlive the thing they authorise.
type playbackTicketStore struct {
	mu      sync.Mutex
	tickets map[string]playbackTicket
}

func newPlaybackTicketStore() *playbackTicketStore {
	return &playbackTicketStore{tickets: make(map[string]playbackTicket)}
}

func (p *playbackTicketStore) issue(sessionID, principal string, now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Opportunistic sweep. The map is bounded by concurrent sessions, but a
	// long-lived server should not accumulate dead tickets either.
	for existing, ticket := range p.tickets {
		if now.After(ticket.expiresAt) {
			delete(p.tickets, existing)
		}
	}

	p.tickets[id] = playbackTicket{
		sessionID: sessionID,
		principal: principal,
		expiresAt: now.Add(playbackTicketTTL),
	}
	return id, nil
}

// resolve returns the ticket for id if it is live.
//
// The lookup is by map key, so it is not constant time; the ticket id is a
// 256-bit random value, which is what makes guessing infeasible rather than the
// comparison. The session binding below *is* compared in constant time, because
// that value is attacker-chosen.
func (p *playbackTicketStore) resolve(id string, now time.Time) (playbackTicket, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ticket, ok := p.tickets[id]
	if !ok {
		return playbackTicket{}, false
	}
	if now.After(ticket.expiresAt) {
		delete(p.tickets, id)
		return playbackTicket{}, false
	}
	return ticket, true
}

// revokeSession drops a session's tickets eagerly.
//
// Hygiene, not the security boundary. Correctness does not depend on this being
// called: `playbackTicketPrincipal` reads the session's live state on every
// request, so a ticket stops working the moment its session becomes terminal
// whether or not anyone remembered to call this. That matters because a session
// can end six different ways — client stop, lease expiry, the sweeper, a
// pipeline failure, an idle timeout, preemption — and a design that had to hook
// each of them would be one new teardown path away from leaving a live
// credential behind.
func (p *playbackTicketStore) revokeSession(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, ticket := range p.tickets {
		if ticket.sessionID == sessionID {
			delete(p.tickets, id)
		}
	}
}

func (s *Server) playbackTicketStoreOrDefault() *playbackTicketStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playbackTickets == nil {
		s.playbackTickets = newPlaybackTicketStore()
	}
	return s.playbackTickets
}

// IssuePlaybackTicket handles POST /api/v3/sessions/{sessionID}/playback-ticket
//
// Authenticated with the caller's ordinary API credential — for a native client
// that means a DPoP-bound request, so the device proves possession of its key
// once, here, rather than never.
func (s *Server) IssuePlaybackTicket(w http.ResponseWriter, r *http.Request) {
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "A playback ticket requires an authenticated request", nil)
		return
	}

	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionId"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(chi.URLParam(r, "sessionID"))
	}
	if sessionID == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Session", problemcode.CodeInvalidInput, "A session id is required", nil)
		return
	}

	effectiveHTTPS := s.requestIsHTTPS(r)
	allowLocalHTTP := s.GetConfig().Connectivity.AllowLocalHTTP || s.GetConfig().Connectivity.Profile == "lan" || s.GetConfig().Connectivity.Profile == "development" || requestRemoteIsPrivateOrLoopback(r)
	if !effectiveHTTPS && !allowLocalHTTP {
		// A media credential must not be handed out in clear text over public internet. Loopback, private LAN/VPN and local profiles are exempt.
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "HTTPS Required", problemcode.CodeInvalidInput, "A playback ticket is only issued over HTTPS", nil)
		return
	}

	id, err := s.playbackTicketStoreOrDefault().issue(sessionID, principal.ID, time.Now().UTC())
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to issue playback ticket")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Ticket Issue Failed", problemcode.CodeInternalError, "The playback ticket could not be issued", nil)
		return
	}

	// Path-scoped to this session's media directory. A browser will never
	// attach it to anything else, and a native client passes it explicitly to
	// its player. Either way it cannot leak sideways onto an API call.
	setServerCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly+SameSite=Strict set; Secure is request-derived
		Name:     playbackTicketCookieName,
		Value:    id,
		Path:     playbackTicketPath(sessionID),
		HttpOnly: true,
		Secure:   effectiveHTTPS,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(playbackTicketTTL / time.Second),
	})

	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	writeJSON(w, http.StatusCreated, PlaybackTicketResponse{
		SessionId: sessionID,
		Ticket:    id,
		Cookie:    playbackTicketCookieName,
		Path:      playbackTicketPath(sessionID),
		ExpiresIn: int(playbackTicketTTL / time.Second),
	})
}

// PostSessionPlaybackTicket implements ServerInterface
func (s *Server) PostSessionPlaybackTicket(w http.ResponseWriter, r *http.Request, sessionId string) {
	s.IssuePlaybackTicket(w, r)
}

func playbackTicketPath(sessionID string) string {
	return "/api/v3/sessions/" + sessionID + "/hls/"
}

func extractPlaybackTicket(r *http.Request) string {
	if r == nil {
		return ""
	}
	if cookie, err := r.Cookie(playbackTicketCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	if qTicket := r.URL.Query().Get("ticket"); qTicket != "" {
		return qTicket
	}
	if qTicket := r.URL.Query().Get("t"); qTicket != "" {
		return qTicket
	}
	if hTicket := r.Header.Get("X-Playback-Ticket"); hTicket != "" {
		return hTicket
	}
	if authHdr := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(authHdr), "bearer ") {
		return strings.TrimSpace(authHdr[7:])
	}
	return ""
}

// playbackTicketPrincipal authenticates a media request carrying a ticket.
//
// Returns false for everything else, including a syntactically fine ticket
// presented for a different session or on a non-media route. The caller then
// falls through to ordinary authentication, so a browser session keeps working
// exactly as before.
func (s *Server) playbackTicketPrincipal(r *http.Request) (*auth.Principal, bool) {
	if r == nil || !isMediaRequest(r) {
		return nil, false
	}

	ticketVal := extractPlaybackTicket(r)
	if ticketVal == "" {
		return nil, false
	}

	ticket, ok := s.playbackTicketStoreOrDefault().resolve(ticketVal, time.Now().UTC())
	if !ok {
		return nil, false
	}

	// The session in the path is attacker-controlled, so this comparison is the
	// one that has to be constant time.
	requested := sessionIDFromMediaPath(r.URL.Path)
	if requested == "" || subtle.ConstantTimeCompare([]byte(requested), []byte(ticket.sessionID)) != 1 {
		return nil, false
	}

	// Liveness is read from the session itself rather than mirrored into the
	// ticket. A ticket that merely remembered "not revoked yet" would outlive
	// every way a session can end that nobody wired a hook into.
	if !s.sessionIsLive(r.Context(), ticket.sessionID) {
		return nil, false
	}

	// Read only, always. A ticket must never be able to start a stream, change
	// a timer, or read household state — it exists to fetch segments.
	return auth.NewPrincipal(ticketVal, ticket.principal, []string{string(ScopeV3Read)}), true
}

// sessionIsLive answers whether the session still exists and has not reached a
// terminal state.
//
// Fails closed when the sessions module is absent: a media route that
// authenticated while the control plane was missing would be authenticating
// against nothing at all.
//
// Deliberately does not form its own opinion about the lease. Lease expiry is
// the session manager's business and it transitions the session accordingly; a
// second interpretation here would be a competing policy that disagrees with
// the first one during every brief heartbeat gap.
func (s *Server) sessionIsLive(ctx context.Context, sessionID string) bool {
	store := s.sessionsModuleDeps().store
	if store == nil {
		return false
	}

	record, err := store.GetSession(ctx, sessionID)
	if err != nil || record == nil {
		return false
	}
	return !record.State.IsTerminal()
}

// sessionIDFromMediaPath extracts the session id from
// /api/v3/sessions/{id}/hls/... and returns "" for anything else.
func sessionIDFromMediaPath(path string) string {
	const prefix = "/api/v3/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	slash := strings.Index(rest, "/")
	if slash <= 0 {
		return ""
	}
	if !strings.HasPrefix(rest[slash:], "/hls/") {
		return ""
	}
	return rest[:slash]
}
