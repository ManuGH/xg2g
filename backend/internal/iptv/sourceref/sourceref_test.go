package sourceref_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
)

var (
	testHMACKey1 = []byte("0123456789abcdef0123456789abcdef")
	testHMACKey2 = []byte("fedcba9876543210fedcba9876543210")
)

// A1: Parsing matrix
func TestParser_A1_Matrix(t *testing.T) {
	parser, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}

	tests := []struct {
		name        string
		ref         string
		wantErr     error
		wantType    string
		wantName    string
		wantURLFrag string // Expected literal in RevealURL()
	}{
		{
			name:        "valid 4097 http",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live/stream.m3u8:Test Channel",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Test Channel",
			wantURLFrag: "http://example.invalid/live/stream.m3u8",
		},
		{
			name:        "valid 5001 https",
			ref:         "5001:0:1:0:0:0:0:0:0:0:https%3a//example.invalid/live/stream.ts:Exteplayer Stream",
			wantErr:     nil,
			wantType:    "5001",
			wantName:    "Exteplayer Stream",
			wantURLFrag: "https://example.invalid/live/stream.ts",
		},
		{
			name:        "valid 5002 https",
			ref:         "5002:0:1:0:0:0:0:0:0:0:https%3a//example.invalid/live/gst.m3u8:Gst Stream",
			wantErr:     nil,
			wantType:    "5002",
			wantName:    "Gst Stream",
			wantURLFrag: "https://example.invalid/live/gst.m3u8",
		},
		{
			name:     "DVB service reference returns ErrNotIPTV",
			ref:      "1:0:19:283D:3FB:1:C00000:0:0:0:",
			wantErr:  sourceref.ErrNotIPTV,
			wantType: "",
		},
		{
			name:     "DVB radio reference returns ErrNotIPTV",
			ref:      "2:0:1:0:0:0:0:0:0:0:",
			wantErr:  sourceref.ErrNotIPTV,
			wantType: "",
		},
		{
			name:     "empty reference",
			ref:      "",
			wantErr:  sourceref.ErrEmptyRef,
			wantType: "",
		},
		{
			name:     "whitespace only reference",
			ref:      "   \t\n  ",
			wantErr:  sourceref.ErrEmptyRef,
			wantType: "",
		},
		{
			name:     "wrong field count (too few fields)",
			ref:      "4097:0:1:0:0:0",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "missing URL field (empty url field with trailing colon)",
			ref:      "4097:0:1:0:0:0:0:0:0:0:",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "empty URL field before name",
			ref:      "4097:0:1:0:0:0:0:0:0:0::Channel Name",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:        "URL field double-encoded (%2520) decoded exactly once, preserves %20",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/path%2520with%2520percent:Double Encoded",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Double Encoded",
			wantURLFrag: "http://example.invalid/path%20with%20percent",
		},
		{
			name:     "non-http scheme: file",
			ref:      "4097:0:1:0:0:0:0:0:0:0:file%3a///etc/passwd:Exploit Attempt",
			wantErr:  sourceref.ErrInvalidScheme,
			wantType: "",
		},
		{
			name:     "non-http scheme: rtsp",
			ref:      "4097:0:1:0:0:0:0:0:0:0:rtsp%3a//example.invalid/live.sdp:RTSP Stream",
			wantErr:  sourceref.ErrInvalidScheme,
			wantType: "",
		},
		{
			name:     "non-http scheme: rtmp",
			ref:      "4097:0:1:0:0:0:0:0:0:0:rtmp%3a//example.invalid/live/stream:RTMP Stream",
			wantErr:  sourceref.ErrInvalidScheme,
			wantType: "",
		},
		{
			name:     "non-http scheme: javascript",
			ref:      "4097:0:1:0:0:0:0:0:0:0:javascript%3aalert(1):XSS Attempt",
			wantErr:  sourceref.ErrInvalidScheme,
			wantType: "",
		},
		{
			name:     "empty scheme",
			ref:      "4097:0:1:0:0:0:0:0:0:0://example.invalid/live.m3u8:No Scheme",
			wantErr:  sourceref.ErrInvalidScheme,
			wantType: "",
		},
		{
			name:        "URL with userinfo accepted",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//user-fake%3apass-fake@example.invalid/live/1.ts:Authenticated",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Authenticated",
			wantURLFrag: "http://user-fake:pass-fake@example.invalid/live/1.ts",
		},
		{
			name:     "control characters: newline in URL rejected",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live%0a.m3u8:Bad Newline",
			wantErr:  sourceref.ErrInvalidURL,
			wantType: "",
		},
		{
			name:     "control characters: NUL in URL rejected",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live%00.m3u8:Bad NUL",
			wantErr:  sourceref.ErrInvalidURL,
			wantType: "",
		},
		{
			name:     "control characters: carriage return in URL rejected",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live%0d.m3u8:Bad CR",
			wantErr:  sourceref.ErrInvalidURL,
			wantType: "",
		},
		{
			name:     "oversize URL (> 4096 bytes) rejected",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/" + strings.Repeat("a", 4100) + ":Too Long",
			wantErr:  sourceref.ErrOversizeURL,
			wantType: "",
		},
		{
			name:        "name field containing : and % does not corrupt URL extraction",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8:Kanal%201: Special: 100% HD",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Kanal%201: Special: 100% HD",
			wantURLFrag: "http://example.invalid/live.m3u8",
		},
		// D3 / E2 tests: unencoded port colon handling and host-only detection
		{
			name:        "E2: path + numeric channel name 101 succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/live.m3u8:101",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "101",
			wantURLFrag: "http://h.invalid/live.m3u8",
		},
		{
			name:        "E2: path + channel name 5/6 Kanal succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/live.m3u8:5/6 Kanal",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "5/6 Kanal",
			wantURLFrag: "http://h.invalid/live.m3u8",
		},
		{
			name:        "E2: query + numeric channel name 101 succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x.ts%3fa=1:101",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "101",
			wantURLFrag: "http://h.invalid/x.ts?a=1",
		},
		{
			name:     "E2: host-only unencoded port with path fails closed with ErrInvalidRef",
			ref:      "4097:0:1:1:1:1:1:0:0:0:http%3a//h.invalid:8080/live/x.ts:Chan",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "E2: host-only unencoded port without path fails closed with ErrInvalidRef",
			ref:      "4097:0:1:1:1:1:1:0:0:0:http%3a//h.invalid:8080:Chan",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "E2: host-only URL + numeric name fails closed with ErrInvalidRef (documented residual)",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid:101",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:        "D3/E2: encoded port colon succeeds with port preserved",
			ref:         "4097:0:1:1:1:1:1:0:0:0:http%3a//h.invalid%3a8080/live/x.ts:Chan",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Chan",
			wantURLFrag: "http://h.invalid:8080/live/x.ts",
		},
		{
			name:        "D3/E2: channel name purely digits after fully-encoded URL with explicit port succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid%3a8080/live.m3u8:101",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "101",
			wantURLFrag: "http://example.invalid:8080/live.m3u8",
		},
		{
			name:     "Unencoded http: port with path fails closed with ErrInvalidRef",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http://h.invalid:8080/live.ts:Name",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "Unencoded http: port without path fails closed with ErrInvalidRef",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http://h.invalid:8080:Name",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "Unencoded http: userinfo with password fails closed with ErrInvalidRef",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http://user:pass@h.invalid/x.ts:Name",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:     "Encoded http%3a: unencoded userinfo password fails closed with ErrInvalidRef",
			ref:      "4097:0:1:0:0:0:0:0:0:0:http%3a//user:pass@h.invalid/x.ts:Name",
			wantErr:  sourceref.ErrInvalidRef,
			wantType: "",
		},
		{
			name:        "Unencoded http: valid path without colons in authority succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http://h.invalid/live.ts:MyChannel",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "MyChannel",
			wantURLFrag: "http://h.invalid/live.ts",
		},
		{
			name:        "Unencoded http: valid path with colons in channel name succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http://h.invalid/live.ts:Sky Cinema: Action HD",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Sky Cinema: Action HD",
			wantURLFrag: "http://h.invalid/live.ts",
		},
		{
			name:        "Encoded URL: properly encoded userinfo and port succeeds",
			ref:         "4097:0:1:0:0:0:0:0:0:0:http%3a//user%3apass@h.invalid%3a8080/x.ts:Name",
			wantErr:     nil,
			wantType:    "4097",
			wantName:    "Name",
			wantURLFrag: "http://user:pass@h.invalid:8080/x.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := parser.Parse(tt.ref)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				// Verify error string does not leak secret fragments or raw reference
				errStr := err.Error()
				if strings.Contains(errStr, "example.invalid") || strings.Contains(errStr, "user-fake") || strings.Contains(errStr, "pass-fake") || strings.Contains(errStr, "h.invalid") {
					t.Fatalf("error message leaked URL/credentials: %s", errStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if src.ServiceType() != tt.wantType {
				t.Errorf("ServiceType() = %q, want %q", src.ServiceType(), tt.wantType)
			}
			if src.ServiceName() != tt.wantName {
				t.Errorf("ServiceName() = %q, want %q", src.ServiceName(), tt.wantName)
			}
			if tt.wantURLFrag != "" && src.RevealURL() != tt.wantURLFrag {
				t.Errorf("RevealURL() = %q, want %q", src.RevealURL(), tt.wantURLFrag)
			}
			if !src.ID().IsValid() {
				t.Errorf("ID() %q is not valid", src.ID())
			}
		})
	}
}

// Structs for D2 reflection testing
type unexportedHolder struct {
	src sourceref.Source
}

type exportedOuter struct {
	Inner unexportedHolder
	S     sourceref.Source
}

// A2: Redaction tests (extended for D2 reflection and nesting)
func TestSource_A2_Redaction(t *testing.T) {
	markerToken := "SECRET-MARKER-7f3a"
	secretUser := "secret-user-99"
	secretPass := "secret-pass-88"
	secretHost := "secret-host-" + markerToken + ".invalid"
	rawURL := fmt.Sprintf("http://%s:%s@%s/live/%s/stream.m3u8", secretUser, secretPass, secretHost, markerToken)
	ref := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Secret Channel", strings.ReplaceAll(rawURL, ":", "%3a"))

	parser, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}

	src, err := parser.Parse(ref)
	if err != nil {
		t.Fatalf("unexpected error parsing ref: %v", err)
	}

	assertNotContains := func(t *testing.T, label, val string) {
		t.Helper()
		for _, forbidden := range []string{markerToken, secretUser, secretPass, secretHost, ref, "4097:0:1:0:0:0:0:0:0:0:http", "http%3a//"} {
			if strings.Contains(val, forbidden) {
				t.Fatalf("%s leaked sensitive data %q in output: %s", label, forbidden, val)
			}
		}
	}

	// 1. Direct Source formatting
	assertNotContains(t, `fmt.Sprintf("%v")`, fmt.Sprintf("%v", src))
	assertNotContains(t, `fmt.Sprintf("%+v")`, fmt.Sprintf("%+v", src))
	assertNotContains(t, `fmt.Sprintf("%#v")`, fmt.Sprintf("%#v", src))
	assertNotContains(t, `fmt.Sprintf("%s")`, fmt.Sprintf("%s", src))
	assertNotContains(t, `fmt.Sprintf("%q")`, fmt.Sprintf("%q", src))
	assertNotContains(t, `fmt.Sprintf("%x")`, fmt.Sprintf("%x", src))
	assertNotContains(t, `fmt.Sprintf("%d")`, fmt.Sprintf("%d", src))

	// Pointers
	assertNotContains(t, `fmt.Sprintf("%v pointer")`, fmt.Sprintf("%v", &src))
	assertNotContains(t, `fmt.Sprintf("%+v pointer")`, fmt.Sprintf("%+v", &src))
	assertNotContains(t, `fmt.Sprintf("%#v pointer")`, fmt.Sprintf("%#v", &src))
	assertNotContains(t, `fmt.Sprintf("%s pointer")`, fmt.Sprintf("%s", &src))
	assertNotContains(t, "src.String()", src.String())
	assertNotContains(t, "src.GoString()", src.GoString())

	// 2. D2: Unexported-field struct holder
	holderVal := unexportedHolder{src: src}
	holderPtr := &unexportedHolder{src: src}
	mapVal := map[string]sourceref.Source{"channel": src}
	sliceVal := []sourceref.Source{src}
	outerVal := exportedOuter{Inner: holderVal, S: src}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		assertNotContains(t, fmt.Sprintf("holderVal %s", verb), fmt.Sprintf(verb, holderVal))
		assertNotContains(t, fmt.Sprintf("holderPtr %s", verb), fmt.Sprintf(verb, holderPtr))
		assertNotContains(t, fmt.Sprintf("mapVal %s", verb), fmt.Sprintf(verb, mapVal))
		assertNotContains(t, fmt.Sprintf("sliceVal %s", verb), fmt.Sprintf(verb, sliceVal))
		assertNotContains(t, fmt.Sprintf("outerVal %s", verb), fmt.Sprintf(verb, outerVal))
	}

	// 3. JSON marshaling
	jsonBytes, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("json.Marshal(src) failed: %v", err)
	}
	assertNotContains(t, "json.Marshal(src)", string(jsonBytes))

	outerJSON, err := json.Marshal(outerVal)
	if err != nil {
		t.Fatalf("json.Marshal(outerVal) failed: %v", err)
	}
	assertNotContains(t, "json.Marshal(outerVal)", string(outerJSON))

	// Text marshaling
	textBytes, err := src.MarshalText()
	if err != nil {
		t.Fatalf("src.MarshalText() failed: %v", err)
	}
	assertNotContains(t, "src.MarshalText()", string(textBytes))

	// 4. slog TextHandler on nested structures
	var textBuf bytes.Buffer
	textLogger := slog.New(slog.NewTextHandler(&textBuf, nil))
	textLogger.Info("test log entry",
		"source", src,
		"holderVal", holderVal,
		"holderPtr", holderPtr,
		"mapVal", mapVal,
		"sliceVal", sliceVal,
		"outerVal", outerVal,
	)
	assertNotContains(t, "slog TextHandler", textBuf.String())

	// 5. slog JSONHandler on nested structures
	var jsonBuf bytes.Buffer
	jsonLogger := slog.New(slog.NewJSONHandler(&jsonBuf, nil))
	jsonLogger.Info("test log entry",
		"source", src,
		"holderVal", holderVal,
		"holderPtr", holderPtr,
		"mapVal", mapVal,
		"sliceVal", sliceVal,
		"outerVal", outerVal,
	)
	assertNotContains(t, "slog JSONHandler", jsonBuf.String())

	// 6. errors.New(fmt.Sprint(...))-style wrapping
	errSprint := errors.New(fmt.Sprint(holderVal))
	errSprintf := errors.New(fmt.Sprintf("%+v", holderVal))
	assertNotContains(t, "errors.New sprint", errSprint.Error())
	assertNotContains(t, "errors.New sprintf", errSprintf.Error())

	// 7. Zero-value Source safety
	zero := sourceref.Source{}
	if !zero.IsZero() {
		t.Fatalf("expected zero.IsZero() to be true")
	}
	if zero.RevealURL() != "" {
		t.Fatalf("expected zero.RevealURL() to be empty, got %q", zero.RevealURL())
	}
	assertNotContains(t, "zero.String()", zero.String())
	assertNotContains(t, "zero.GoString()", zero.GoString())
	assertNotContains(t, `fmt.Sprintf("%+v", zero)`, fmt.Sprintf("%+v", zero))
	zeroJSON, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("json.Marshal(zero) failed: %v", err)
	}
	assertNotContains(t, "json.Marshal(zero)", string(zeroJSON))

	// 8. RevealURL should be the ONLY place where rawURL is returned
	if !strings.Contains(src.RevealURL(), markerToken) {
		t.Fatalf("RevealURL() expected to contain marker, got %s", src.RevealURL())
	}
}

// A3: ID properties
func TestSource_A3_IDProperties(t *testing.T) {
	parser1, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("failed to create parser1: %v", err)
	}
	parser2, err := sourceref.NewParser(testHMACKey2)
	if err != nil {
		t.Fatalf("failed to create parser2: %v", err)
	}

	ref1 := "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live/stream1.m3u8:Chan 1"

	src1a, err := parser1.Parse(ref1)
	if err != nil {
		t.Fatalf("Parse ref1 failed: %v", err)
	}
	src1b, err := parser1.Parse(ref1)
	if err != nil {
		t.Fatalf("Parse ref1 duplicate failed: %v", err)
	}

	// 1. Determinism
	if src1a.ID() != src1b.ID() {
		t.Fatalf("determinism violation: %q != %q", src1a.ID(), src1b.ID())
	}

	// 2. Key sensitivity
	src2, err := parser2.Parse(ref1)
	if err != nil {
		t.Fatalf("Parse ref1 with key2 failed: %v", err)
	}
	if src1a.ID() == src2.ID() {
		t.Fatalf("key sensitivity violation: expected different IDs for different keys, got identical %q", src1a.ID())
	}

	// 3. No URL substring in ID
	idStr := src1a.ID().String()
	if strings.Contains(idStr, "example") || strings.Contains(idStr, "stream1") || strings.Contains(idStr, "m3u8") {
		t.Fatalf("ID leaks URL substring: %s", idStr)
	}

	// 4. Fixed length and charset
	if !strings.HasPrefix(idStr, sourceref.IDPrefix) {
		t.Fatalf("ID does not have expected prefix %q: %s", sourceref.IDPrefix, idStr)
	}
	if len(idStr) != len(sourceref.IDPrefix)+26 {
		t.Fatalf("ID length %d != expected %d", len(idStr), len(sourceref.IDPrefix)+26)
	}
	if !src1a.ID().IsValid() {
		t.Fatalf("ID.IsValid() returned false for %q", idStr)
	}

	// 5. Canonicalisation tests
	canonTests := []struct {
		name      string
		refA      string
		refB      string
		wantEqual bool
	}{
		{
			name:      "scheme case insensitive",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:HTTP%3a//example.invalid/live.m3u8:Chan",
			wantEqual: true,
		},
		{
			name:      "host case insensitive",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//EXAMPLE.INVALID/live.m3u8:Chan",
			wantEqual: true,
		},
		{
			name:      "http default port 80 stripped",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid%3a80/live.m3u8:Chan",
			wantEqual: true,
		},
		{
			name:      "https default port 443 stripped",
			refA:      "4097:0:1:0:0:0:0:0:0:0:https%3a//example.invalid/live.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:https%3a//example.invalid%3a443/live.m3u8:Chan",
			wantEqual: true,
		},
		{
			name:      "non-default port preserved",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid%3a8080/live.m3u8:Chan",
			wantEqual: false,
		},
		{
			name:      "D1: query parameter order preserved (different IDs)",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8%3fa=1%26b=2:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8%3fb=2%26a=1:Chan",
			wantEqual: false,
		},
		{
			name:      "different path yields different ID",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live1.m3u8:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live2.m3u8:Chan",
			wantEqual: false,
		},
		{
			name:      "different query value yields different ID",
			refA:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8%3ftoken=alpha:Chan",
			refB:      "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live.m3u8%3ftoken=beta:Chan",
			wantEqual: false,
		},
	}

	for _, tt := range canonTests {
		t.Run(tt.name, func(t *testing.T) {
			sA, err := parser1.Parse(tt.refA)
			if err != nil {
				t.Fatalf("Parse refA failed: %v", err)
			}
			sB, err := parser1.Parse(tt.refB)
			if err != nil {
				t.Fatalf("Parse refB failed: %v", err)
			}

			if tt.wantEqual && sA.ID() != sB.ID() {
				t.Fatalf("expected equal IDs, got %q != %q", sA.ID(), sB.ID())
			}
			if !tt.wantEqual && sA.ID() == sB.ID() {
				t.Fatalf("expected different IDs, got equal %q", sA.ID())
			}
		})
	}

	// 6. D1 table: query pairs with semicolon and no-query must yield 3 distinct IDs
	t.Run("D1: distinct IDs for semicolon queries and empty query", func(t *testing.T) {
		refA := "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/p%3ftoken=abc;exp=1:Chan A"
		refB := "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/p%3ftoken=xyz;exp=2:Chan B"
		refC := "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/p:Chan C"

		sA, err := parser1.Parse(refA)
		if err != nil {
			t.Fatalf("Parse refA failed: %v", err)
		}
		sB, err := parser1.Parse(refB)
		if err != nil {
			t.Fatalf("Parse refB failed: %v", err)
		}
		sC, err := parser1.Parse(refC)
		if err != nil {
			t.Fatalf("Parse refC failed: %v", err)
		}

		if sA.ID() == sB.ID() {
			t.Fatalf("ID collision: sA and sB produced identical ID %q", sA.ID())
		}
		if sB.ID() == sC.ID() {
			t.Fatalf("ID collision: sB and sC produced identical ID %q", sB.ID())
		}
		if sA.ID() == sC.ID() {
			t.Fatalf("ID collision: sA and sC produced identical ID %q", sA.ID())
		}
	})
}

// A4: Registry tests & race conditions
func TestRegistry_A4_ConcurrentAndAtomic(t *testing.T) {
	parser, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("NewParser failed: %v", err)
	}

	srcA, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamA.m3u8:Stream A")
	if err != nil {
		t.Fatalf("Parse srcA failed: %v", err)
	}
	srcB, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamB.m3u8:Stream B")
	if err != nil {
		t.Fatalf("Parse srcB failed: %v", err)
	}
	srcC, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamC.m3u8:Stream C")
	if err != nil {
		t.Fatalf("Parse srcC failed: %v", err)
	}

	reg := sourceref.NewRegistry()

	// Initial empty lookup
	_, err = reg.Lookup(srcA.ID())
	if !errors.Is(err, sourceref.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on empty registry, got %v", err)
	}
	if strings.Contains(err.Error(), string(srcA.ID())) {
		t.Fatalf("ErrNotFound must not echo input ID: %s", err.Error())
	}

	// Test Replace and Lookup
	if err := reg.Replace([]sourceref.Source{srcA, srcB}); err != nil {
		t.Fatalf("Replace failed: %v", err)
	}
	if reg.Len() != 2 {
		t.Fatalf("expected Len() == 2, got %d", reg.Len())
	}

	gotA, err := reg.Lookup(srcA.ID())
	if err != nil {
		t.Fatalf("Lookup(srcA) failed: %v", err)
	}
	if gotA.RevealURL() != srcA.RevealURL() {
		t.Fatalf("Lookup returned wrong source: %s != %s", gotA.RevealURL(), srcA.RevealURL())
	}

	// Test Replace(nil) empties registry
	if err := reg.Replace(nil); err != nil {
		t.Fatalf("Replace(nil) failed: %v", err)
	}
	if reg.Len() != 0 {
		t.Fatalf("expected Len() == 0 after Replace(nil), got %d", reg.Len())
	}
	_, err = reg.Lookup(srcA.ID())
	if !errors.Is(err, sourceref.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Replace(nil), got %v", err)
	}

	// Test duplicate same URL under different names (allowed)
	srcB_dup, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamB.m3u8:Different Bouquet Name")
	if err != nil {
		t.Fatalf("Parse srcB_dup failed: %v", err)
	}
	if err := reg.Replace([]sourceref.Source{srcB, srcB_dup}); err != nil {
		t.Fatalf("Replace with identical URL under different name should be legal, got: %v", err)
	}
	if reg.Len() != 1 {
		t.Fatalf("expected Len() == 1 after deduplication, got %d", reg.Len())
	}

	// Test zero source in snapshot (safely skipped)
	if err := reg.Replace([]sourceref.Source{srcA, sourceref.Source{}}); err != nil {
		t.Fatalf("Replace with zero source failed: %v", err)
	}
	if reg.Len() != 1 {
		t.Fatalf("expected Len() == 1 after skipping zero source, got %d", reg.Len())
	}

	// E1: Test equivalent URL variants (harmless duplicates across bouquets)
	t.Run("E1: harmless equivalent duplicate pairs succeed and keep first", func(t *testing.T) {
		pairs := []struct {
			name string
			ref1 string
			ref2 string
		}{
			{
				name: "default port 80 stripped",
				ref1: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x:First",
				ref2: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid%3a80/x:Second",
			},
			{
				name: "empty path vs root slash",
				ref1: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid:First",
				ref2: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/:Second",
			},
			{
				name: "scheme and host casing",
				ref1: "4097:0:1:0:0:0:0:0:0:0:http%3a//H.INVALID/x:First",
				ref2: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x:Second",
			},
			{
				name: "URL fragment ignored in canonical form",
				ref1: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x:First",
				ref2: "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x%23frag:Second",
			},
		}

		for _, tc := range pairs {
			t.Run(tc.name, func(t *testing.T) {
				s1, err := parser.Parse(tc.ref1)
				if err != nil {
					t.Fatalf("Parse ref1 failed: %v", err)
				}
				s2, err := parser.Parse(tc.ref2)
				if err != nil {
					t.Fatalf("Parse ref2 failed: %v", err)
				}

				if s1.ID() != s2.ID() {
					t.Fatalf("expected identical IDs for equivalent variants, got %q != %q", s1.ID(), s2.ID())
				}

				r := sourceref.NewRegistry()
				if err := r.Replace([]sourceref.Source{s1, s2}); err != nil {
					t.Fatalf("Replace failed on equivalent pair: %v", err)
				}
				if r.Len() != 1 {
					t.Fatalf("expected Len() == 1, got %d", r.Len())
				}
				lookup, err := r.Lookup(s1.ID())
				if err != nil {
					t.Fatalf("Lookup failed: %v", err)
				}
				if lookup.RevealURL() != s1.RevealURL() {
					t.Fatalf("expected first entry to win, got RevealURL() = %q, want %q", lookup.RevealURL(), s1.RevealURL())
				}
			})
		}
	})

	// E1: Forced real collision (same ID, different canonical URL) must fail
	t.Run("E1: forced true HMAC collision returns ErrCollision", func(t *testing.T) {
		r := sourceref.NewRegistry()
		s1 := sourceref.NewSourceForTest(srcA.ID(), "http://h.invalid/real1", "http://h.invalid/canonical1", "ref1")
		s2 := sourceref.NewSourceForTest(srcA.ID(), "http://h.invalid/real2", "http://h.invalid/canonical2", "ref2")
		err := r.Replace([]sourceref.Source{s1, s2})
		if !errors.Is(err, sourceref.ErrCollision) {
			t.Fatalf("expected ErrCollision, got %v", err)
		}
	})

	// Concurrency test under -race
	srcB_v1, _ := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamB.m3u8:Version 1")
	srcB_v2, _ := parser.Parse("5001:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/streamB.m3u8:Version 2")

	snap1 := []sourceref.Source{srcA, srcB_v1}
	snap2 := []sourceref.Source{srcB_v2, srcC}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					var rErr error
					if id%2 == 0 {
						rErr = reg.Replace(snap1)
					} else {
						rErr = reg.Replace(snap2)
					}
					if rErr != nil {
						t.Errorf("Replace failed during concurrent execution: %v", rErr)
					}
					time.Sleep(100 * time.Microsecond)
				}
			}
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// 1. Verify single entry consistency: lookup returns either complete v1 or complete v2
					b, err := reg.Lookup(srcB_v1.ID())
					if err == nil {
						isV1 := b.ServiceName() == "Version 1" && b.ServiceType() == "4097"
						isV2 := b.ServiceName() == "Version 2" && b.ServiceType() == "5001"
						if !isV1 && !isV2 {
							t.Errorf("torn read on srcB entry: saw mixed fields serviceName=%q serviceType=%q", b.ServiceName(), b.ServiceType())
						}
					}

					// 2. Verify snapshot atomicity: snapshot contains either snap1 items or snap2 items, never both srcA and srcC
					snap := reg.Snapshot()
					hasA := false
					hasC := false
					for _, s := range snap {
						if s.ID() == srcA.ID() {
							hasA = true
						}
						if s.ID() == srcC.ID() {
							hasC = true
						}
					}
					if hasA && hasC {
						t.Errorf("torn snapshot: saw both srcA (from snap1) and srcC (from snap2) in same snapshot!")
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestSource_RawRef(t *testing.T) {
	parser, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}
	const body = "4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/live/x.ts:Chan Name"

	t.Run("returns the complete original reference, trimmed", func(t *testing.T) {
		src, err := parser.Parse("  " + body + "\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := src.RawRef(); got != body {
			t.Errorf("RawRef() = %q, want %q", got, body)
		}
		if got := src.RevealURL(); got != "http://h.invalid/live/x.ts" {
			t.Errorf("RevealURL() = %q", got)
		}
	})

	t.Run("NewSourceForTest honours its rawRef argument", func(t *testing.T) {
		src := sourceref.NewSourceForTest("iptv_aaaaaaaaaaaaaaaaaaaaaaaaaa", "http://h.invalid/x", "http://h.invalid/x", "the-raw-ref")
		if got := src.RawRef(); got != "the-raw-ref" {
			t.Errorf("RawRef() = %q, want %q", got, "the-raw-ref")
		}
	})

	t.Run("zero value returns empty string", func(t *testing.T) {
		var zero sourceref.Source
		if got := zero.RawRef(); got != "" {
			t.Errorf("zero RawRef() = %q, want empty", got)
		}
	})

	t.Run("dedupe keeps the first entry's raw reference", func(t *testing.T) {
		a, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//h.invalid/x:First")
		if err != nil {
			t.Fatalf("parse a: %v", err)
		}
		b, err := parser.Parse("4097:0:1:0:0:0:0:0:0:0:http%3a//H.INVALID%3a80/x:Second")
		if err != nil {
			t.Fatalf("parse b: %v", err)
		}
		if a.ID() != b.ID() {
			t.Fatalf("test premise broken: IDs differ (%s vs %s)", a.ID(), b.ID())
		}
		reg := sourceref.NewRegistry()
		if err := reg.Replace([]sourceref.Source{a, b}); err != nil {
			t.Fatalf("Replace: %v", err)
		}
		got, err := reg.Lookup(a.ID())
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if got.RawRef() != a.RawRef() {
			t.Errorf("RawRef() of winner = %q, want first entry %q", got.RawRef(), a.RawRef())
		}
	})
}

func TestClassifyReference(t *testing.T) {
	parser, err := sourceref.NewParser(testHMACKey1)
	if err != nil {
		t.Fatalf("failed to create parser: %v", err)
	}

	t.Run("DVB reference classified as non-IPTV", func(t *testing.T) {
		dvbRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
		src, isIPTV, err := sourceref.ClassifyReference(parser, dvbRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if isIPTV {
			t.Errorf("expected isIPTV=false for DVB ref, got true")
		}
		if !src.IsZero() {
			t.Errorf("expected zero Source, got %+v", src)
		}
	})

	t.Run("DVB reference with colliding service ID (PULS 4 Austria 4E27) classified as non-IPTV", func(t *testing.T) {
		collidingRef := "1:0:1:4E27:43A:1:C00000:0:0:0:"
		src, isIPTV, err := sourceref.ClassifyReference(nil, collidingRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if isIPTV {
			t.Errorf("expected isIPTV=false for colliding DVB ref, got true")
		}
		if !src.IsZero() {
			t.Errorf("expected zero Source, got %+v", src)
		}
	})

	t.Run("Valid 4097 IPTV reference classified as IPTV with nil parser", func(t *testing.T) {
		iptvRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//example.invalid/live/stream.m3u8:Test Channel"
		src, isIPTV, err := sourceref.ClassifyReference(nil, iptvRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !isIPTV {
			t.Fatalf("expected isIPTV=true for 4097 ref, got false")
		}
		if src.RevealURL() != "http://example.invalid/live/stream.m3u8" {
			t.Errorf("RevealURL() = %q, want %q", src.RevealURL(), "http://example.invalid/live/stream.m3u8")
		}
		if src.RawRef() != iptvRef {
			t.Errorf("RawRef() = %q, want %q", src.RawRef(), iptvRef)
		}
		if src.ServiceType() != "4097" {
			t.Errorf("ServiceType() = %q, want 4097", src.ServiceType())
		}
	})

	t.Run("Valid 5001 and 5002 classified as IPTV with keyed parser", func(t *testing.T) {
		for _, serviceType := range []string{"5001", "5002"} {
			ref := fmt.Sprintf("%s:0:1:0:0:0:0:0:0:0:https%%3a//example.invalid/stream.ts:Chan", serviceType)
			src, isIPTV, err := sourceref.ClassifyReference(parser, ref)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", serviceType, err)
			}
			if !isIPTV {
				t.Fatalf("expected isIPTV=true for %s, got false", serviceType)
			}
			if src.RevealURL() != "https://example.invalid/stream.ts" {
				t.Errorf("RevealURL() = %q, want https://example.invalid/stream.ts", src.RevealURL())
			}
			if src.ID() == "" {
				t.Errorf("expected non-empty ID for keyed parser")
			}
		}
	})

	t.Run("Malformed 4097 reference returns error", func(t *testing.T) {
		malformedRef := "4097:0:1:bad"
		src, isIPTV, err := sourceref.ClassifyReference(nil, malformedRef)
		if err == nil {
			t.Fatalf("expected error for malformed 4097 ref, got nil")
		}
		if isIPTV {
			t.Errorf("expected isIPTV=false on error, got true")
		}
		if !src.IsZero() {
			t.Errorf("expected zero Source on error, got %+v", src)
		}
	})

	t.Run("Empty reference classified as non-IPTV with nil error", func(t *testing.T) {
		src, isIPTV, err := sourceref.ClassifyReference(nil, "   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if isIPTV {
			t.Errorf("expected isIPTV=false for empty ref, got true")
		}
		if !src.IsZero() {
			t.Errorf("expected zero Source, got %+v", src)
		}
	})
}
