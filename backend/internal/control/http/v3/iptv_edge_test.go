// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func newTestResolverWithCanary(t *testing.T) (*edge.Resolver, string, string) {
	t.Helper()
	secret := "secret-test-key-of-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	reg := sourceref.NewRegistry()

	rawCanary := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	src, err := parser.Parse(rawCanary)
	if err != nil {
		t.Fatalf("Parse canary: %v", err)
	}
	if err := reg.Replace([]sourceref.Source{src}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	res := edge.NewResolver(reg, parser, metrics.IncIPTVLegacyIngress)
	return res, string(src.ID()), rawCanary
}

func TestResolveClientServiceRef_PassThroughDVB(t *testing.T) {
	res, _, _ := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, dvbRef)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if raw != dvbRef {
		t.Fatalf("expected %q, got %q", dvbRef, raw)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected response written: %d", rec.Code)
	}
}

func TestResolveClientServiceRef_LegacyRaw(t *testing.T) {
	res, _, _ := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	legacyRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//legacy.invalid/live.ts:Legacy"
	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, legacyRef)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if raw != legacyRef {
		t.Fatalf("expected %q, got %q", legacyRef, raw)
	}
}

func TestResolveClientServiceRef_OpaqueKnown(t *testing.T) {
	res, opaqueID, rawCanary := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, opaqueID)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if raw != rawCanary {
		t.Fatalf("expected %q, got %q", rawCanary, raw)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected code: %d", rec.Code)
	}
}

func TestResolveClientServiceRef_OpaqueUnknown_NotFound_ZeroEcho(t *testing.T) {
	res, _, _ := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, unknownID)
	if ok {
		t.Fatalf("expected ok=false for unknown id, got true (raw=%q)", raw)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, unknownID) {
		t.Fatalf("response body leaked client input %q: %s", unknownID, body)
	}

	var prob map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("failed to decode ProblemDetails JSON: %v", err)
	}
	if prob["code"] != problemcode.CodeNotFound {
		t.Fatalf("expected code %q, got %v", problemcode.CodeNotFound, prob["code"])
	}
}

func TestResolveClientServiceRef_MalformedID_BadRequest_ZeroEcho(t *testing.T) {
	res, _, _ := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	malformedID := "iptv_not_valid_hex_or_base32!!!"
	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, malformedID)
	if ok {
		t.Fatalf("expected ok=false for malformed id, got true (raw=%q)", raw)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, malformedID) {
		t.Fatalf("response body leaked client input %q: %s", malformedID, body)
	}

	var prob map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("failed to decode ProblemDetails JSON: %v", err)
	}
	if prob["code"] != problemcode.CodeInvalidInput {
		t.Fatalf("expected code %q, got %v", problemcode.CodeInvalidInput, prob["code"])
	}
}

func TestResolveClientServiceRef_NilResolver_OpaqueFails_RawPasses(t *testing.T) {
	srv := &Server{iptvResolver: nil}

	// 1. Opaque ID fails with 404
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	_, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, "iptv_abcdefghijklmnopqrstuvwxyz")
	if ok {
		t.Fatalf("expected ok=false with nil resolver")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// 2. DVB ref succeeds
	dvbRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	rec2 := httptest.NewRecorder()
	raw, ok := srv.resolveClientServiceRef(rec2, req, metrics.EndpointIntents, dvbRef)
	if !ok || raw != dvbRef {
		t.Fatalf("expected DVB to pass through with nil resolver, got ok=%v, raw=%q", ok, raw)
	}

	// 3. Legacy raw IPTV ref succeeds
	rawIPTV := "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.ts:Chan"
	rec3 := httptest.NewRecorder()
	raw3, ok := srv.resolveClientServiceRef(rec3, req, metrics.EndpointIntents, rawIPTV)
	if !ok || raw3 != rawIPTV {
		t.Fatalf("expected raw IPTV to pass through with nil resolver, got ok=%v, raw=%q", ok, raw3)
	}
}

func TestResolveClientServiceRef_WhitespaceHandling(t *testing.T) {
	res, opaqueID, rawCanary := newTestResolverWithCanary(t)
	srv := &Server{iptvResolver: res}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	raw, ok := srv.resolveClientServiceRef(rec, req, metrics.EndpointIntents, "  "+opaqueID+"  \n")
	if !ok {
		t.Fatalf("expected ok=true with padded whitespace")
	}
	if raw != rawCanary {
		t.Fatalf("expected %q, got %q", rawCanary, raw)
	}
}
