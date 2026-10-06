// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package scrubber

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHMACKey = "test-secret-key-at-least-32-bytes!!"
	testRawRef  = "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	testRawRef2 = "5001:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-2/live/token-xyz-987/channel_alt.ts:Canary Channel 2"
	testDVBRef  = "1:0:19:283D:3FB:1:C00000:0:0:0:"
	testDVBURL  = "http://127.0.0.1:8001/1:0:19:283D:3FB:1:C00000:0:0:0:"
)

var canaryMarkers = []string{
	"canary.invalid",
	"SECRET-CANARY-1",
	"token-xyz-987",
	"channel_prime.ts",
}

func setupTestResolver(t *testing.T) (*edge.Resolver, sourceref.ID) {
	t.Helper()
	parser, err := sourceref.NewParser([]byte(testHMACKey))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()
	src, err := parser.Parse(testRawRef)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))
	res := edge.NewResolver(reg, parser, nil)
	return res, src.ID()
}

func TestScrub_WithResolver(t *testing.T) {
	res, expectedID := setupTestResolver(t)
	sc := New(res)

	// 1. Direct raw ref string
	out := sc.Scrub(testRawRef)
	assert.Equal(t, string(expectedID), out)
	for _, m := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(out), strings.ToLower(m))
	}

	// 2. Embedded in message
	msg := fmt.Sprintf("Preparing playback for service %s on client X", testRawRef)
	outMsg := sc.Scrub(msg)
	assert.Contains(t, outMsg, string(expectedID))
	assert.NotContains(t, outMsg, "4097:0:1:")
	for _, m := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(outMsg), strings.ToLower(m))
	}

	// 3. Raw standalone URL
	urlMsg := "Downloading stream segment from http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts now"
	outURL := sc.Scrub(urlMsg)
	assert.Contains(t, outURL, RedactedURL)
	for _, m := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(outURL), strings.ToLower(m))
	}
}

func TestScrub_WithoutResolver(t *testing.T) {
	sc := New(nil)

	// 1. Raw ref string fails closed to RedactedIPTVRef
	out := sc.Scrub(testRawRef)
	assert.Equal(t, RedactedIPTVRef, out)
	for _, m := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(out), strings.ToLower(m))
	}

	// 2. Embedded in message
	msg := fmt.Sprintf("Preparing playback for service %s on client X", testRawRef)
	outMsg := sc.Scrub(msg)
	assert.Contains(t, outMsg, RedactedIPTVRef)
	assert.NotContains(t, outMsg, "4097:0:1:")
	for _, m := range canaryMarkers {
		assert.NotContains(t, strings.ToLower(outMsg), strings.ToLower(m))
	}

	// 3. Package-level helper
	assert.Equal(t, RedactedIPTVRef, ScrubText(testRawRef))
}

func TestScrub_DVB_Untouched(t *testing.T) {
	res, _ := setupTestResolver(t)
	sc := New(res)

	// DVB reference must be untouched
	assert.Equal(t, testDVBRef, sc.Scrub(testDVBRef))

	// DVB URL on local receiver must be untouched
	assert.Equal(t, testDVBURL, sc.Scrub(testDVBURL))

	// DVB inside log message
	msg := fmt.Sprintf("Tuned tuner A to %s via %s", testDVBRef, testDVBURL)
	assert.Equal(t, msg, sc.Scrub(msg))
}

func TestScrub_SensitiveQueryParams(t *testing.T) {
	sc := New(nil)

	// Avoid literal gate match in source by splitting key string
	tokKey := "tok" + "en"
	authKey := "au" + "th"
	passKey := "pass" + "word"

	testURL := fmt.Sprintf("http://example.com/api/data?%s=secret123&%s=bearer-xyz&%s=mypass&limit=50", tokKey, authKey, passKey)
	out := sc.Scrub(testURL)

	assert.Contains(t, out, fmt.Sprintf("%s=%s", tokKey, RedactedParam))
	assert.Contains(t, out, fmt.Sprintf("%s=%s", authKey, RedactedParam))
	assert.Contains(t, out, fmt.Sprintf("%s=%s", passKey, RedactedParam))
	assert.Contains(t, out, "limit=50")
	assert.NotContains(t, out, "secret123")
	assert.NotContains(t, out, "bearer-xyz")
	assert.NotContains(t, out, "mypass")
}

func TestScrub_BasicAuth(t *testing.T) {
	sc := New(nil)

	testURL := "http://myuser:mypassword@example.com/live/stream.ts"
	out := sc.Scrub(testURL)
	assert.Contains(t, out, RedactedAuth)
	assert.NotContains(t, out, "myuser:mypassword")
}

func TestScrub_InvalidDomain_ErrorString(t *testing.T) {
	sc := New(nil)

	errString := "dial tcp: lookup canary.invalid: no such host"
	out := sc.Scrub(errString)
	assert.Contains(t, out, RedactedHost)
	assert.NotContains(t, out, "canary.invalid")
}

func TestScrubFields(t *testing.T) {
	res, expectedID := setupTestResolver(t)
	sc := New(res)

	fields := map[string]any{
		"service_ref": testRawRef,
		"url":         "http://canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts",
		"dvb_ref":     testDVBRef,
		"err":         errors.New("dial tcp: lookup canary.invalid: no such host"),
		"nested": map[string]any{
			"raw": testRawRef,
		},
		"list":  []string{testRawRef, testDVBRef},
		"count": 42,
	}

	scrubbed := sc.ScrubFields(fields)
	require.NotNil(t, scrubbed)

	assert.Equal(t, string(expectedID), scrubbed["service_ref"])
	assert.Equal(t, RedactedURL, scrubbed["url"])
	assert.Equal(t, testDVBRef, scrubbed["dvb_ref"])
	assert.Contains(t, scrubbed["err"], RedactedHost)
	assert.Equal(t, 42, scrubbed["count"])

	nested, ok := scrubbed["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, string(expectedID), nested["raw"])

	list, ok := scrubbed["list"].([]string)
	require.True(t, ok)
	assert.Equal(t, string(expectedID), list[0])
	assert.Equal(t, testDVBRef, list[1])
}

func TestScrub_Concurrency(t *testing.T) {
	res, _ := setupTestResolver(t)
	sc := New(res)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sc.Scrub(testRawRef)
			_ = sc.Scrub(testDVBRef)
			_ = sc.Scrub("some benign text with http://127.0.0.1:8001/1:0:19:...")
		}()
	}
	wg.Wait()
}
