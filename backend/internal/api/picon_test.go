// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

var piconTestSecret = []byte("0123456789abcdef0123456789abcdef")

func TestServePiconLogo_DVB(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	piconData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRdvb_test_picon")
	filename := "1_0_19_283D_3FB_1_C00000_0_0_0.png"
	require.NoError(t, os.WriteFile(filepath.Join(piconsDir, filename), piconData, 0644))

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	handler := server.Handler()

	req := httptest.NewRequest(http.MethodGet, "/logos/"+filename, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "image/png", rr.Header().Get("Content-Type"))
	assert.Equal(t, piconData, rr.Body.Bytes())
}

func TestServePiconLogo_IPTV_OpaqueResolution(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	parser, err := sourceref.NewParser(piconTestSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	rawCanary := "4097:0:19:1:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-TOKEN/live.ts:Canary Channel"
	src, err := parser.Parse(rawCanary)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)

	// OpenWebif / picon download stores the file using the service reference with colons and slashes sanitized
	storeRef := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(rawCanary, ":", "_"), "/", "_"), "_")
	piconData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRiptv_opaque_picon")
	require.NoError(t, os.WriteFile(filepath.Join(piconsDir, storeRef+".png"), piconData, 0644))

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	server.WireV3Runtime(v3.Dependencies{IPTVResolver: res}, nil)
	handler := server.Handler()

	opaqueID := string(src.ID())
	reqURL := "/logos/" + opaqueID + ".png"
	req := httptest.NewRequest(http.MethodGet, reqURL, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "image/png", rr.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=86400", rr.Header().Get("Cache-Control"))
	assert.Equal(t, piconData, rr.Body.Bytes())

	// Crucial security invariant: raw reference or token must NOT leak in any header
	for k, vals := range rr.Header() {
		for _, v := range vals {
			assert.False(t, strings.Contains(v, "SECRET-CANARY-TOKEN"), "header %s leaked canary token: %s", k, v)
			assert.False(t, strings.Contains(v, "4097:"), "header %s leaked raw reference: %s", k, v)
		}
	}
}

func TestServePiconLogo_IPTV_NormalizedFallback(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	parser, err := sourceref.NewParser(piconTestSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// HD service reference (type 19)
	rawHD := "4097:0:19:1:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY/live.ts:HD Channel"
	src, err := parser.Parse(rawHD)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)

	// Normalized SD service reference (type 1)
	normRef := openwebif.NormalizeServiceRefForPicon(rawHD)
	require.NotEqual(t, rawHD, normRef)
	normStoreRef := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(normRef, ":", "_"), "/", "_"), "_")

	piconData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRnormalized_sd_picon")
	// Save ONLY under normalized name, do NOT create the HD storeRef file
	require.NoError(t, os.WriteFile(filepath.Join(piconsDir, normStoreRef+".png"), piconData, 0644))

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	server.WireV3Runtime(v3.Dependencies{IPTVResolver: res}, nil)
	handler := server.Handler()

	opaqueID := string(src.ID())
	req := httptest.NewRequest(http.MethodGet, "/logos/"+opaqueID+".png", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "image/png", rr.Header().Get("Content-Type"))
	assert.Equal(t, piconData, rr.Body.Bytes())
}

func TestServePiconLogo_IPTV_DVBTripletPiconOnDisk(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	parser, err := sourceref.NewParser(piconTestSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// IPTV service reference with DVB triplet prefix and stream URL
	rawRef := "4097:0:19:132F:3EF:1:C00000:0:0:0:http%3a//canary.invalid/live.ts:Astra HD"
	src, err := parser.Parse(rawRef)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)

	// Picon file on disk is named purely by DVB triplet prefix: 4097_0_19_132F_3EF_1_C00000_0_0_0.png
	tripletFilename := "4097_0_19_132F_3EF_1_C00000_0_0_0.png"
	piconData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRdvb_triplet_picon")
	require.NoError(t, os.WriteFile(filepath.Join(piconsDir, tripletFilename), piconData, 0644))

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	server.WireV3Runtime(v3.Dependencies{IPTVResolver: res}, nil)
	handler := server.Handler()

	opaqueID := string(src.ID())
	req := httptest.NewRequest(http.MethodGet, "/logos/"+opaqueID+".png", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "image/png", rr.Header().Get("Content-Type"))
	assert.Equal(t, piconData, rr.Body.Bytes())
}

func TestServePiconLogo_IPTV_NotFound(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	parser, err := sourceref.NewParser(piconTestSecret)
	require.NoError(t, err)
	reg := sourceref.NewRegistry()

	// Registered channel but without picon file on disk
	rawRef := "4097:0:1:1:0:0:0:0:0:0:http%3a//canary.invalid/no-picon/live.ts:No Picon Channel"
	src, err := parser.Parse(rawRef)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	server.WireV3Runtime(v3.Dependencies{IPTVResolver: res}, nil)
	handler := server.Handler()

	// 1. Registered channel with missing file -> 404
	reqRegistered := httptest.NewRequest(http.MethodGet, "/logos/"+string(src.ID())+".png", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, reqRegistered)
	assert.Equal(t, http.StatusNotFound, rr1.Code)

	// 2. Unknown opaque ID (valid format, not in registry) -> 404
	validUnknown := "iptv_abcdefghijklmnopqrstuvwx23"
	reqUnknown := httptest.NewRequest(http.MethodGet, "/logos/"+validUnknown+".png", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, reqUnknown)
	assert.Equal(t, http.StatusNotFound, rr2.Code)
}

func TestServePiconLogo_ValidationAndSecurity(t *testing.T) {
	dataDir := t.TempDir()
	piconsDir := filepath.Join(dataDir, "picons")
	require.NoError(t, os.MkdirAll(piconsDir, 0755))

	server := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	handler := server.Handler()

	invalidPaths := []string{
		"/logos/invalid..png",
		"/logos/iptv_short.png",
		"/logos/iptv_invalid_base32!!!.png",
		"/logos/iptv_00000000000000000000000000.png", // base32 only uses 2-7
		"/logos/1_0_19_test.jpg",
		"/logos/1_0_19_test.gif",
		"/logos/../secret.png",
		"/logos/%2e%2e%2fsecret.png",
	}

	for _, p := range invalidPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code, "path %s should return 404", p)
	}
}
