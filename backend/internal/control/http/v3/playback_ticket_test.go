// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	sessionmodel "github.com/ManuGH/xg2g/internal/domain/session/model"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
)

// A ticket is only as good as the session it names, so every test here has to
// say what state that session is in. Building a server without one would test a
// ticket against nothing.
func serverWithSession(t *testing.T, sessionID string, state sessionmodel.SessionState) (*Server, *sessionstore.MemoryStore) {
	t.Helper()

	store := sessionstore.NewMemoryStore()
	require.NoError(t, store.PutSession(t.Context(), &sessionmodel.SessionRecord{
		SessionID: sessionID,
		State:     state,
	}))

	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.SetDependencies(Dependencies{Store: store})
	return srv, store
}

// A playback ticket is a weaker credential than the API one, so what matters is
// not only that it works but exactly how far it reaches.

func mediaRequest(t *testing.T, path, ticket string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://xg2g.test"+path, nil)
	if ticket != "" {
		req.AddCookie(&http.Cookie{Name: playbackTicketCookieName, Value: ticket})
	}
	return req
}

func TestPlaybackTicket_AuthenticatesItsOwnSession(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
	store := srv.playbackTicketStoreOrDefault()
	id, err := store.issue("sess-a", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	// 1. Via Cookie
	principal, ok := srv.playbackTicketPrincipal(
		mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", id),
	)
	require.True(t, ok)
	require.Equal(t, []string{string(ScopeV3Read)}, principal.Scopes,
		"a ticket must carry read scope and nothing more")

	// 2. Via Query Param ?ticket=
	reqQuery := httptest.NewRequest(http.MethodGet, "https://xg2g.test/api/v3/sessions/sess-a/hls/index.m3u8?ticket="+id, nil)
	principalQ, okQ := srv.playbackTicketPrincipal(reqQuery)
	require.True(t, okQ)
	require.Equal(t, []string{string(ScopeV3Read)}, principalQ.Scopes)

	// 3. Via Header X-Playback-Ticket
	reqHdr := httptest.NewRequest(http.MethodGet, "https://xg2g.test/api/v3/sessions/sess-a/hls/index.m3u8", nil)
	reqHdr.Header.Set("X-Playback-Ticket", id)
	principalH, okH := srv.playbackTicketPrincipal(reqHdr)
	require.True(t, okH)
	require.Equal(t, []string{string(ScopeV3Read)}, principalH.Scopes)

	// 4. Via Bearer Auth
	reqBearer := httptest.NewRequest(http.MethodGet, "https://xg2g.test/api/v3/sessions/sess-a/hls/index.m3u8", nil)
	reqBearer.Header.Set("Authorization", "Bearer "+id)
	principalB, okB := srv.playbackTicketPrincipal(reqBearer)
	require.True(t, okB)
	require.Equal(t, []string{string(ScopeV3Read)}, principalB.Scopes)
}

func TestPlaybackTicket_WorksForVariantPlaylistsAndSegments(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
	id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	// ABR means the player walks into a variant directory on its own. A ticket
	// that only worked for the master playlist would fail the moment the
	// bitrate changed.
	for _, path := range []string{
		"/api/v3/sessions/sess-a/hls/index.m3u8",
		"/api/v3/sessions/sess-a/hls/720p/index.m3u8",
		"/api/v3/sessions/sess-a/hls/720p/segment-00042.m4s",
	} {
		_, ok := srv.playbackTicketPrincipal(mediaRequest(t, path, id))
		require.True(t, ok, "ticket must authenticate %s", path)
	}
}

// The central boundary: one ticket, one session.
func TestPlaybackTicket_IsRefusedForAnotherSession(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
	id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	_, ok := srv.playbackTicketPrincipal(
		mediaRequest(t, "/api/v3/sessions/sess-b/hls/index.m3u8", id),
	)
	require.False(t, ok, "a ticket for one session must not open another")
}

// The other central boundary: a ticket is not an API credential. If this ever
// passes, a stolen ticket stops being a stream and becomes an account.
func TestPlaybackTicket_AuthenticatesNoAPIRoute(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
	id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	for _, path := range []string{
		"/api/v3/services",
		"/api/v3/intents",
		"/api/v3/auth/effective-permissions",
		"/api/v3/sessions/sess-a",
		"/api/v3/sessions/sess-a/heartbeat",
		"/api/v3/system/config",
	} {
		_, ok := srv.playbackTicketPrincipal(mediaRequest(t, path, id))
		require.False(t, ok, "a playback ticket must authenticate nothing at %s", path)
	}
}

func TestPlaybackTicket_ExpiresAndIsForgotten(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	store := srv.playbackTicketStoreOrDefault()
	issuedAt := time.Now().UTC()
	id, err := store.issue("sess-a", "usr_1", issuedAt)
	require.NoError(t, err)

	_, ok := store.resolve(id, issuedAt.Add(playbackTicketTTL-time.Minute))
	require.True(t, ok)

	_, ok = store.resolve(id, issuedAt.Add(playbackTicketTTL+time.Second))
	require.False(t, ok)

	// Reading an expired ticket drops it rather than leaving it to accumulate.
	store.mu.Lock()
	_, present := store.tickets[id]
	store.mu.Unlock()
	require.False(t, present)
}

func TestPlaybackTicket_UnknownAndEmptyValuesAreRefused(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)

	_, ok := srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", ""))
	require.False(t, ok, "no cookie is not authentication")

	_, ok = srv.playbackTicketPrincipal(
		mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", "0000000000000000"),
	)
	require.False(t, ok, "a guessed ticket must not authenticate")
}

// Ending a session must take its tickets with it. Otherwise a stopped stream
// leaves a live credential behind for as long as the TTL runs.
func TestPlaybackTicket_SessionRevocationDropsEveryTicket(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	store := srv.playbackTicketStoreOrDefault()
	now := time.Now().UTC()

	first, err := store.issue("sess-a", "usr_1", now)
	require.NoError(t, err)
	second, err := store.issue("sess-a", "usr_1", now)
	require.NoError(t, err)
	other, err := store.issue("sess-b", "usr_1", now)
	require.NoError(t, err)

	store.revokeSession("sess-a")

	_, ok := store.resolve(first, now)
	require.False(t, ok)
	_, ok = store.resolve(second, now)
	require.False(t, ok)
	_, ok = store.resolve(other, now)
	require.True(t, ok, "another session's tickets must be untouched")
}

func TestSessionIDFromMediaPath(t *testing.T) {
	cases := map[string]string{
		"/api/v3/sessions/abc/hls/index.m3u8":   "abc",
		"/api/v3/sessions/abc/hls/720p/seg.m4s": "abc",
		"/api/v3/sessions/abc":                  "",
		"/api/v3/sessions/abc/heartbeat":        "",
		"/api/v3/sessions//hls/index.m3u8":      "",
		"/api/v3/recordings/abc/playlist.m3u8":  "",
		"/api/v3/sessions/abc/events":           "",
		"/hls/index.m3u8":                       "",
		"/api/v3/sessions/abc/hls":              "",
		// A traversal-shaped path yields no session at all: the segment after
		// the id is "/../admin/..." rather than "/hls/", so nothing matches and
		// no ticket can be spent on it. Chi cleans paths before routing anyway;
		// this is the second lock on the same door.
		"/api/v3/sessions/../../admin/hls/index.m3u8": "",
	}
	for path, expected := range cases {
		require.Equal(t, expected, sessionIDFromMediaPath(path), "path %s", path)
	}
}

// MARK: - The ticket follows the session, not a revocation hook

// A session ends six different ways: client stop, lease expiry, the sweeper, a
// pipeline failure, an idle timeout, preemption. Every one of them lands the
// session in a terminal state, and the ticket has to stop working for all of
// them — not only for the one path that happens to call revokeSession.
func TestPlaybackTicket_DiesWithEveryTerminalState(t *testing.T) {
	for _, state := range []sessionmodel.SessionState{
		sessionmodel.SessionStopped,
		sessionmodel.SessionFailed,
		sessionmodel.SessionCancelled,
	} {
		t.Run(string(state), func(t *testing.T) {
			srv, sessions := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
			id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
			require.NoError(t, err)

			// Works while the session is live.
			_, ok := srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", id))
			require.True(t, ok)

			// The session ends. Nothing revokes the ticket — that is the point.
			require.NoError(t, sessions.PutSession(t.Context(), &sessionmodel.SessionRecord{
				SessionID: "sess-a",
				State:     state,
			}))

			_, ok = srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", id))
			require.False(t, ok, "a ticket must not outlive its session ending as %s", state)
		})
	}
}

// A session that was garbage-collected away is gone in the strongest sense.
func TestPlaybackTicket_DiesWithAVanishedSession(t *testing.T) {
	srv, _ := serverWithSession(t, "sess-a", sessionmodel.SessionReady)
	id, err := srv.playbackTicketStoreOrDefault().issue("sess-gone", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	_, ok := srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-gone/hls/index.m3u8", id))
	require.False(t, ok)
}

// Without a sessions module there is no session to be live, so a media route
// must authenticate nobody rather than trust a ticket against nothing.
func TestPlaybackTicket_FailsClosedWithoutASessionStore(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
	require.NoError(t, err)

	_, ok := srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", id))
	require.False(t, ok)
}

// Non-terminal states are all playable: a stream is watchable while it is still
// draining or stopping, and cutting it off early would be a worse bug than the
// one this guards against.
func TestPlaybackTicket_LivesThroughEveryNonTerminalState(t *testing.T) {
	for _, state := range []sessionmodel.SessionState{
		sessionmodel.SessionStarting,
		sessionmodel.SessionPriming,
		sessionmodel.SessionReady,
		sessionmodel.SessionDraining,
		sessionmodel.SessionStopping,
	} {
		srv, _ := serverWithSession(t, "sess-a", state)
		id, err := srv.playbackTicketStoreOrDefault().issue("sess-a", "usr_1", time.Now().UTC())
		require.NoError(t, err)

		_, ok := srv.playbackTicketPrincipal(mediaRequest(t, "/api/v3/sessions/sess-a/hls/index.m3u8", id))
		require.True(t, ok, "state %s must still play", state)
	}
}
