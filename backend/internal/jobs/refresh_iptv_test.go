// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

// createMockReceiverServer spins up a fake OpenWebIF server returning the requested bouquets and services.
func createMockReceiverServer(t *testing.T, servicesJSON string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "bouquets") {
			_, _ = w.Write([]byte(`{"bouquets":[["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.favourites.tv\" ORDER BY bouquet","Favourites"]]}`))
			return
		}
		if strings.Contains(r.URL.Path, "getservices") || strings.Contains(r.URL.Path, "getallservices") {
			_, _ = w.Write([]byte(servicesJSON))
			return
		}
		if strings.Contains(r.URL.Path, "streamcurrent") {
			_, _ = w.Write([]byte(`{"result": true}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func setupMinimalSnapshot(t *testing.T, serverURL string) (config.Snapshot, string) {
	t.Helper()
	tempDir := t.TempDir()

	cfg := config.AppConfig{
		DataDir: tempDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    serverURL,
			StreamPort: 8001,
		},
	}
	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	return snap, tempDir
}

func setupSnapshotWithXMLTV(t *testing.T, serverURL string) (config.Snapshot, string) {
	t.Helper()
	tempDir := t.TempDir()

	cfg := config.AppConfig{
		DataDir: tempDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    serverURL,
			StreamPort: 8001,
		},
		XMLTVPath: "xmltv.xml",
	}
	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	return snap, tempDir
}

func TestRefresh_IPTVRegistryPopulation(t *testing.T) {
	// Build a synthetic bouquet of ~700 refs including:
	// - 686 distinct canonical URLs
	// - 7 duplicate entries (e.g. same channel appearing twice)
	// - 6 equivalent formatting variants (port :80, host casing, trailing slash)
	// - 1 unparsable ref (missing scheme/host or bad syntax)
	// Total: 700 services

	var servicesBuilder strings.Builder
	servicesBuilder.WriteString(`{"services":[`)

	expectedDistinct := 686

	for i := 0; i < expectedDistinct; i++ {
		if i > 0 {
			servicesBuilder.WriteString(`,`)
		}
		ref := fmt.Sprintf(`4097:0:1:0:0:0:0:0:0:0:http%%3a//provider%d.invalid%%3a8080/live/token/chan%d.ts:Channel %d`, i, i, i)
		servicesBuilder.WriteString(fmt.Sprintf(`{"servicename":"Channel %d","servicereference":"%s"}`, i, ref))
	}

	// Add 7 exact duplicates (same ref as channel 0 to 6)
	for i := 0; i < 7; i++ {
		ref := fmt.Sprintf(`4097:0:1:0:0:0:0:0:0:0:http%%3a//provider%d.invalid%%3a8080/live/token/chan%d.ts:Channel %d`, i, i, i)
		servicesBuilder.WriteString(fmt.Sprintf(`,{"servicename":"Duplicate Channel %d","servicereference":"%s"}`, i, ref))
	}

	// Add 6 equivalent variants of channel 10 to 15 (e.g. host casing, :80, etc.)
	for i := 10; i < 16; i++ {
		// Variant: UPPERCASE host name
		ref := fmt.Sprintf(`4097:0:1:0:0:0:0:0:0:0:http%%3a//PROVIDER%d.INVALID%%3a8080/live/token/chan%d.ts:Channel %d`, i, i, i)
		servicesBuilder.WriteString(fmt.Sprintf(`,{"servicename":"Variant Channel %d","servicereference":"%s"}`, i, ref))
	}

	// Add 1 unparsable IPTV ref (e.g. missing URL segment)
	servicesBuilder.WriteString(`,{"servicename":"Unparsable Channel","servicereference":"4097:0:1:0:0:0:0:0:0:0:not_a_valid_url:Broken"}`)
	servicesBuilder.WriteString(`]}`)

	server := createMockReceiverServer(t, servicesBuilder.String())
	defer server.Close()

	snap, _ := setupMinimalSnapshot(t, server.URL)

	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// Capture logs to verify that no sensitive refs appear in the logs
	var logBuf bytes.Buffer
	origLogger := zerolog.DefaultContextLogger
	defer func() { zerolog.DefaultContextLogger = origLogger }()

	testLogger := zerolog.New(&logBuf)
	ctx := testLogger.WithContext(context.Background())

	status, err := RefreshWithOptions(ctx, snap, WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status)

	// 1. Assert registry Len() equals exactly expectedDistinct (686)
	assert.Equal(t, expectedDistinct, reg.Len(), "registry should contain exactly the distinct canonical sources")

	// 2. Assert log output contains summary counts and ZERO references/URLs/credentials
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "iptv sources registry updated")
	assert.Contains(t, logOutput, fmt.Sprintf(`"parsed":%d`, 699)) // 686 + 7 + 6 = 699 parsed
	assert.Contains(t, logOutput, `"skipped":1`)                   // 1 unparsable
	assert.Contains(t, logOutput, `"deduped":13`)                  // 7 duplicates + 6 variants = 13 deduped
	assert.Contains(t, logOutput, fmt.Sprintf(`"total_registered":%d`, expectedDistinct))

	// Assert NO leak of URLs or refs in log output
	assert.False(t, strings.Contains(logOutput, "provider0.invalid"), "log must never contain provider host")
	assert.False(t, strings.Contains(logOutput, "4097:"), "log must never contain raw 4097 ref")
	assert.False(t, strings.Contains(logOutput, "token"), "log must never contain token")
}

func TestRefresh_NilParserSkipsPopulation(t *testing.T) {
	servicesJSON := `{"services":[{"servicename":"IPTV 1","servicereference":"4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live.ts:IPTV 1"}]}`
	server := createMockReceiverServer(t, servicesJSON)
	defer server.Close()

	snap, _ := setupMinimalSnapshot(t, server.URL)
	reg := sourceref.NewRegistry()

	// Nil parser => WithIPTVSources(nil, reg) skips population entirely
	status, err := RefreshWithOptions(context.Background(), snap, WithIPTVSources(nil, reg))
	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, 0, reg.Len(), "nil parser must skip registry population")
}

func TestRefresh_FailedFetchDoesNotCallReplace(t *testing.T) {
	// Server returns 500 on bouquets fetch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	snap, _ := setupMinimalSnapshot(t, server.URL)
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)

	reg := sourceref.NewRegistry()
	// Pre-seed the registry with 1 source
	preseeded, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//initial.invalid/stream.ts:Initial")
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{preseeded}))
	require.Equal(t, 1, reg.Len())

	_, err = RefreshWithOptions(context.Background(), snap, WithIPTVSources(parser, reg))
	require.Error(t, err)

	// Invariant: Failed fetch must not alter or clear existing registry snapshot
	assert.Equal(t, 1, reg.Len(), "failed refresh must not replace existing registry snapshot")
	found, err := reg.Lookup(preseeded.ID())
	require.NoError(t, err)
	assert.Equal(t, preseeded.RawRef(), found.RawRef())
}

func TestRefresh_ErrCollisionRetainsPreviousSnapshot(t *testing.T) {
	servicesJSON := `{"services":[{"servicename":"IPTV 1","servicereference":"4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live.ts:IPTV 1"}]}`
	server := createMockReceiverServer(t, servicesJSON)
	defer server.Close()

	snap, _ := setupMinimalSnapshot(t, server.URL)
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)

	reg := sourceref.NewRegistry()
	preseeded, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//initial.invalid/stream.ts:Initial")
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{preseeded}))

	// Force ErrCollision via test hook
	testHookReplaceRegistry = func(r *sourceref.Registry, snapshot []sourceref.Source) error {
		return sourceref.ErrCollision
	}
	defer func() { testHookReplaceRegistry = nil }()

	var logBuf bytes.Buffer
	testLogger := zerolog.New(&logBuf)
	ctx := testLogger.WithContext(context.Background())

	// Refresh still succeeds without returning an error
	status, err := RefreshWithOptions(ctx, snap, WithIPTVSources(parser, reg))
	require.NoError(t, err)
	assert.NotNil(t, status)

	// Invariant: collision warning was logged, and previous snapshot remains active
	assert.Contains(t, logBuf.String(), "iptv sources snapshot rejected due to collision; keeping previous snapshot")
	assert.Equal(t, 1, reg.Len())
	found, err := reg.Lookup(preseeded.ID())
	require.NoError(t, err)
	assert.Equal(t, preseeded.RawRef(), found.RawRef())
}

func TestRefresh_Golden_ByteIdenticalOutput(t *testing.T) {
	servicesJSON := `{"services":[
		{"servicename":"Das Erste HD","servicereference":"1:0:19:283D:3FB:1:C00000:0:0:0:"},
		{"servicename":"ZDF HD","servicereference":"1:0:19:2B66:3F3:1:C00000:0:0:0:"},
		{"servicename":"IPTV Stream 1","servicereference":"4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live1.ts:IPTV Stream 1"},
		{"servicename":"IPTV Stream 2","servicereference":"5001:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live2.ts:IPTV Stream 2"}
	]}`

	server := createMockReceiverServer(t, servicesJSON)
	defer server.Close()

	// Run 1: Without WithIPTVSources option (option off)
	snap1, dir1 := setupSnapshotWithXMLTV(t, server.URL)
	status1, err := RefreshWithOptions(context.Background(), snap1)
	require.NoError(t, err)
	require.NotNil(t, status1)

	playlistFile1 := filepath.Join(dir1, snap1.Runtime.PlaylistFilename)
	playlistBytes1, err := os.ReadFile(playlistFile1)
	require.NoError(t, err)

	xmltvFile1 := filepath.Join(dir1, "xmltv.xml")
	xmltvBytes1, err := os.ReadFile(xmltvFile1)
	require.NoError(t, err)

	// Run 2: With WithIPTVSources option (option on)
	snap2, dir2 := setupSnapshotWithXMLTV(t, server.URL)
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	status2, err := RefreshWithOptions(context.Background(), snap2, WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status2)

	playlistFile2 := filepath.Join(dir2, snap2.Runtime.PlaylistFilename)
	playlistBytes2, err := os.ReadFile(playlistFile2)
	require.NoError(t, err)

	xmltvFile2 := filepath.Join(dir2, "xmltv.xml")
	xmltvBytes2, err := os.ReadFile(xmltvFile2)
	require.NoError(t, err)

	// Assert registry is populated in Run 2
	assert.Equal(t, 2, reg.Len(), "Run 2 should have populated the 2 IPTV sources")

	// Hard Golden Requirement: playlist.m3u and xmltv.xml MUST be BYTE-IDENTICAL!
	// (Note: tvg-logo query param timestamp ?v=UNIX might differ by 1s if runs crossed second boundary,
	// but within sub-millisecond local runs they are identical. Let's verify bytes directly).
	if !bytes.Equal(playlistBytes1, playlistBytes2) {
		// If only the timestamp query param ?v=... differs due to clock tick:
		s1 := stripTimestampParams(string(playlistBytes1))
		s2 := stripTimestampParams(string(playlistBytes2))
		assert.Equal(t, s1, s2, "playlist content must be identical between option on and off")
	} else {
		assert.True(t, bytes.Equal(playlistBytes1, playlistBytes2), "playlist.m3u must be byte-identical")
	}

	// The XMLTV carries the same "?v=<unix>" logo timestamp in its icon URLs, so it
	// needs the same tolerance for a second boundary between the two runs.
	if !bytes.Equal(xmltvBytes1, xmltvBytes2) {
		assert.Equal(t, stripTimestampParams(string(xmltvBytes1)), stripTimestampParams(string(xmltvBytes2)),
			"xmltv.xml content must be identical between option on and off")
	}
}

func TestStripTimestampParams_NormalisesEveryOccurrence(t *testing.T) {
	a := `<icon src="/logos/x.png?v=1790916904"/><icon src="/logos/y.png?v=1790916904"/>` + "\n" + `tvg-logo="/logos/z.png?v=1790916904"`
	b := `<icon src="/logos/x.png?v=1790916905"/><icon src="/logos/y.png?v=1790916905"/>` + "\n" + `tvg-logo="/logos/z.png?v=1790916905"`
	assert.NotEqual(t, a, b)
	assert.Equal(t, stripTimestampParams(a), stripTimestampParams(b), "all timestamps on a line, and at line end, must be normalised")
	assert.NotEqual(t, stripTimestampParams(a), stripTimestampParams(`<icon src="/logos/other.png?v=1"/>`), "must not erase real differences")
}

// logoTimestampRE matches the cache-busting "?v=<unix seconds>" the refresh job
// appends to logo URLs. It changes whenever two refreshes straddle a second
// boundary, in the playlist AND in the XMLTV icon URLs.
var logoTimestampRE = regexp.MustCompile(`\?v=\d+`)

func stripTimestampParams(s string) string {
	return logoTimestampRE.ReplaceAllString(s, "?v=")
}

func TestRefresh_PlaylistSeparationAndPermissions(t *testing.T) {
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	servicesJSON := `{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary IPTV", "servicereference": "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-TOKEN/live.ts:Canary IPTV"}
		]
	}`
	mockOWI := createMockReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: dataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockOWI.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	snap.Runtime.PlaylistFilename = "playlist.m3u"
	status, err := RefreshWithOptions(context.Background(), snap, WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.Channels)

	// 1. Filesystem existence & permission verification
	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	rawInfo, err := os.Stat(rawPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), rawInfo.Mode().Perm(), "internal playlist.m3u must have 0600 permissions")

	publicInfo, err := os.Stat(publicPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), publicInfo.Mode().Perm(), "public playlist_public.m3u must have 0644 permissions")

	// 2. Internal raw playlist content check: preserves raw references for internal subsystems
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	assert.Contains(t, rawStr, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	assert.Contains(t, rawStr, "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-TOKEN/live.ts:Canary IPTV")
	assert.Contains(t, rawStr, "SECRET-CANARY-TOKEN")

	// 3. Public masked playlist content check: client-facing, zero secret exposure
	publicBytes, err := os.ReadFile(publicPlaylistPath)
	require.NoError(t, err)
	publicStr := string(publicBytes)

	// Zero canary markers
	assert.False(t, strings.Contains(publicStr, "SECRET-CANARY-TOKEN"), "public playlist must not contain raw token")
	assert.False(t, strings.Contains(publicStr, "canary.invalid"), "public playlist must not contain upstream host")
	assert.False(t, strings.Contains(publicStr, "4097:"), "public playlist must not contain raw IPTV service ref")

	// Must contain DVB item unaltered
	assert.Contains(t, publicStr, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	assert.Contains(t, publicStr, "DVB Das Erste")

	// Must contain masked IPTV item with opaque ID
	assert.Equal(t, 1, reg.Len(), "IPTV source must be registered")
	var opaqueID string
	for _, src := range reg.Snapshot() {
		opaqueID = string(src.ID())
		break
	}
	require.NotEmpty(t, opaqueID)

	assert.Contains(t, publicStr, fmt.Sprintf(`tvg-id="%s"`, opaqueID))
	assert.Contains(t, publicStr, fmt.Sprintf(`/logos/%s.png`, opaqueID))
	assert.Contains(t, publicStr, fmt.Sprintf(`/api/v3/stream/live/%s`, opaqueID))
}

func TestRefresh_PublicPlaylist_FailClosed_MissingParser(t *testing.T) {
	servicesJSON := `{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary IPTV", "servicereference": "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-TOKEN/live.ts:Canary IPTV"}
		]
	}`
	mockOWI := createMockReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: dataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockOWI.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	snap.Runtime.PlaylistFilename = "playlist.m3u"

	// Deliberately execute WITHOUT WithIPTVSources (opts.iptvParser == nil)
	status, err := RefreshWithOptions(context.Background(), snap)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.Channels)

	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	// 1. Internal playlist must be intact with 0600 permissions and raw references/tokens
	rawInfo, err := os.Stat(rawPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), rawInfo.Mode().Perm())
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	assert.Contains(t, rawStr, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	assert.Contains(t, rawStr, "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-TOKEN/live.ts:Canary IPTV")
	assert.Contains(t, rawStr, "SECRET-CANARY-TOKEN")

	// 2. Public playlist must fail closed: omit IPTV channel completely, retain DVB channel, 0644 permissions
	publicInfo, err := os.Stat(publicPlaylistPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), publicInfo.Mode().Perm())
	publicBytes, err := os.ReadFile(publicPlaylistPath)
	require.NoError(t, err)
	publicStr := string(publicBytes)

	// Zero canary credentials or raw IPTV references
	assert.False(t, strings.Contains(publicStr, "SECRET-CANARY-TOKEN"), "public playlist must not leak token when parser is missing")
	assert.False(t, strings.Contains(publicStr, "canary.invalid"), "public playlist must not leak host when parser is missing")
	assert.False(t, strings.Contains(publicStr, "4097:"), "public playlist must not leak raw IPTV ref when parser is missing")
	assert.False(t, strings.Contains(publicStr, "Canary IPTV"), "unconvertible IPTV channel must be omitted from public playlist")

	// DVB channel must remain intact
	assert.Contains(t, publicStr, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	assert.Contains(t, publicStr, "DVB Das Erste")
}

func TestRefresh_PublicPlaylist_FailClosed_InvalidIPTVReference(t *testing.T) {
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// Malformed IPTV reference with unencoded port colon (fails sourceref.Parser.Parse with ErrInvalidRef)
	// and another with invalid URL scheme/format
	servicesJSON := `{
		"services": [
			{"servicename": "DVB Das Erste", "servicereference": "1:0:19:283D:3FB:1:C00000:0:0:0:"},
			{"servicename": "Canary Port IPTV", "servicereference": "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid:8080/SECRET-PORT-CANARY/live.ts:Canary Port IPTV"},
			{"servicename": "Canary BadURL IPTV", "servicereference": "4097:0:1:0:0:0:0:0:0:0:not-a-valid-url-with-SECRET-SCHEME-CANARY:Canary BadURL IPTV"}
		]
	}`
	mockOWI := createMockReceiverServer(t, servicesJSON)
	defer mockOWI.Close()

	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: dataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    mockOWI.URL,
			StreamPort: 8001,
		},
		Bouquet: "Favourites",
	}

	snap := config.BuildSnapshot(cfg, config.ReadOSRuntimeEnvOrDefault())
	snap.Runtime.PlaylistFilename = "playlist.m3u"

	status, err := RefreshWithOptions(context.Background(), snap, WithIPTVSources(parser, reg))
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 3, status.Channels)

	rawPlaylistPath := filepath.Join(dataDir, "playlist.m3u")
	publicPlaylistPath := filepath.Join(dataDir, "playlist_public.m3u")

	// 1. Internal playlist must be intact with 0600 permissions and raw invalid references
	rawBytes, err := os.ReadFile(rawPlaylistPath)
	require.NoError(t, err)
	rawStr := string(rawBytes)
	assert.Contains(t, rawStr, "SECRET-PORT-CANARY")
	assert.Contains(t, rawStr, "SECRET-SCHEME-CANARY")
	assert.Contains(t, rawStr, "Canary Port IPTV")
	assert.Contains(t, rawStr, "Canary BadURL IPTV")

	// 2. Public playlist must fail closed: omit invalid IPTV items, retain DVB channel
	publicBytes, err := os.ReadFile(publicPlaylistPath)
	require.NoError(t, err)
	publicStr := string(publicBytes)

	assert.False(t, strings.Contains(publicStr, "SECRET-PORT-CANARY"), "public playlist must not leak canary from invalid ref")
	assert.False(t, strings.Contains(publicStr, "SECRET-SCHEME-CANARY"), "public playlist must not leak canary from malformed ref")
	assert.False(t, strings.Contains(publicStr, "canary.invalid"), "public playlist must not leak upstream host")
	assert.False(t, strings.Contains(publicStr, "4097:"), "public playlist must not leak raw IPTV ref")
	assert.False(t, strings.Contains(publicStr, "Canary Port IPTV"), "invalid IPTV channel must be omitted")
	assert.False(t, strings.Contains(publicStr, "Canary BadURL IPTV"), "invalid IPTV channel must be omitted")

	// DVB channel must remain intact
	assert.Contains(t, publicStr, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	assert.Contains(t, publicStr, "DVB Das Erste")
}

func TestBuildPublicPlaylistItems_Unit_FailClosed(t *testing.T) {
	parser, err := sourceref.NewParser(testSecret)
	require.NoError(t, err)

	dvbItem := playlist.Item{
		Name:       "Das Erste HD",
		ServiceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
		TvgID:      "1:0:19:283D:3FB:1:C00000:0:0:0:",
		URL:        "http://receiver:8001/1:0:19:283D:3FB:1:C00000:0:0:0:",
		TvgLogo:    "/logos/1_0_19_283D_3FB_1_C00000_0_0_0.png",
	}
	validIPTVItem := playlist.Item{
		Name:       "Valid IPTV",
		ServiceRef: "4097:0:1:0:0:0:0:0:0:0:http%3a//upstream.invalid/secret/live.ts:Valid IPTV",
		TvgID:      "4097:0:1:0:0:0:0:0:0:0:http%3a//upstream.invalid/secret/live.ts:Valid IPTV",
		URL:        "http://receiver:8001/4097:0:1:0:0:0:0:0:0:0:http%3a//upstream.invalid/secret/live.ts:Valid IPTV",
		TvgLogo:    "/logos/4097_test.png",
	}
	invalidIPTVItem := playlist.Item{
		Name:       "Invalid IPTV",
		ServiceRef: "4097:0:1:0:0:0:0:0:0:0:http%3a//upstream.invalid:8080/secret/live.ts:Invalid IPTV",
		TvgID:      "4097:0:1:0:0:0:0:0:0:0:http%3a//upstream.invalid:8080/secret/live.ts:Invalid IPTV",
		URL:        "http://receiver:8001/invalid",
		TvgLogo:    "/logos/invalid.png",
	}

	t.Run("nil parser drops all IPTV items and preserves DVB", func(t *testing.T) {
		items := []playlist.Item{dvbItem, validIPTVItem, invalidIPTVItem}
		out := buildPublicPlaylistItems(items, nil, "", "")
		require.Len(t, out, 1)
		assert.Equal(t, dvbItem.ServiceRef, out[0].ServiceRef)
		assert.Equal(t, dvbItem.Name, out[0].Name)
	})

	t.Run("keyed parser converts valid IPTV and drops invalid IPTV", func(t *testing.T) {
		items := []playlist.Item{dvbItem, validIPTVItem, invalidIPTVItem}
		out := buildPublicPlaylistItems(items, parser, "", "")
		require.Len(t, out, 2)
		// Item 0 is DVB unaltered
		assert.Equal(t, dvbItem.ServiceRef, out[0].ServiceRef)
		assert.Equal(t, dvbItem.Name, out[0].Name)

		// Item 1 is valid IPTV masked
		assert.True(t, strings.HasPrefix(out[1].ServiceRef, sourceref.IDPrefix))
		assert.Equal(t, out[1].ServiceRef, out[1].TvgID)
		assert.Contains(t, out[1].URL, "/api/v3/stream/live/iptv_")
		assert.Contains(t, out[1].TvgLogo, "/logos/iptv_")
		assert.False(t, strings.Contains(out[1].URL, "upstream.invalid"))
		assert.False(t, strings.Contains(out[1].ServiceRef, "4097:"))

		// Invalid IPTV is completely omitted
		for _, it := range out {
			assert.NotEqual(t, "Invalid IPTV", it.Name)
		}
	})
}
