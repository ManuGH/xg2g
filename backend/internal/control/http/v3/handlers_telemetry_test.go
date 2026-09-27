// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ManuGH/xg2g/internal/control/auth"
	xlog "github.com/ManuGH/xg2g/internal/log"
)

// captureTelemetryLogs routes the global logger into a buffer for one test and
// returns the JSON lines written by the telemetry handler.
func captureTelemetryLogs(t *testing.T) func() []map[string]any {
	t.Helper()

	var buf bytes.Buffer
	xlog.Configure(xlog.Config{Level: "debug", Output: &buf})
	t.Cleanup(func() { xlog.Configure(xlog.Config{Level: "error", Output: io.Discard}) })

	return func() []map[string]any {
		var lines []map[string]any
		scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) != nil {
				continue
			}
			if line["component"] == "client_telemetry" {
				lines = append(lines, line)
			}
		}
		return lines
	}
}

func clientPlaybackEventCount(t *testing.T, platform, kind string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "xg2g_client_playback_events_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if labels["platform"] == platform && labels["kind"] == kind {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func postTelemetry(t *testing.T, body string, principal *auth.Principal) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v3/telemetry/playback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	}
	rr := httptest.NewRecorder()
	(&Server{}).PostPlaybackTelemetry(rr, req)
	return rr
}

// A frozen picture is seen only by the client. The server has to keep what the
// client reports, tied to the stream it delivered, and a degraded window has to
// stand out from the healthy samples around it.
func TestPostPlaybackTelemetry_LogsEachEventCorrelatedAndCountsIt(t *testing.T) {
	readLogs := captureTelemetryLogs(t)
	heartbeatsBefore := clientPlaybackEventCount(t, "ios", "heartbeat")
	degradedBefore := clientPlaybackEventCount(t, "ios", "degraded")

	body := `{
	  "client": {"platform": "ios", "appVersion": "3.0", "build": "412", "device": "iPhone18,1"},
	  "events": [
	    {"kind": "heartbeat", "occurredAt": "2026-09-26T18:29:05Z", "zapId": "ios-test-1",
	     "serviceRef": "1:0:19:1:1:1:C00000:0:0:0", "metrics": {"audioLeadMs": 312, "presentedFieldsPerSec": 50.1}},
	    {"kind": "degraded", "occurredAt": "2026-09-26T18:29:20Z", "zapId": "ios-test-1",
	     "serviceRef": "1:0:19:1:1:1:C00000:0:0:0", "reasons": ["audio_underruns"],
	     "metrics": {"audioUnderrunsDelta": 412, "audioMinLeadMs": -177}}
	  ]
	}`
	rr := postTelemetry(t, body, &auth.Principal{ID: "p1", DeviceID: "dev-telemetry-1"})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rr.Code, rr.Body.String())
	}

	lines := readLogs()
	var heartbeat, degraded map[string]any
	for _, line := range lines {
		if line["zap_id"] != "ios-test-1" {
			continue
		}
		switch line["event"] {
		case "client.playback.heartbeat":
			heartbeat = line
		case "client.playback.degraded":
			degraded = line
		}
	}
	if heartbeat == nil || degraded == nil {
		t.Fatalf("want one heartbeat and one degraded log line for the zap, got %v", lines)
	}

	if heartbeat["level"] != "info" {
		t.Errorf("heartbeat logged at %v, want info", heartbeat["level"])
	}
	if degraded["level"] != "warn" {
		t.Errorf("degraded window logged at %v, want warn", degraded["level"])
	}
	for key, want := range map[string]string{
		"service_ref": "1:0:19:1:1:1:C00000:0:0:0",
		"platform":    "ios",
		"build":       "412",
		"device_id":   "dev-telemetry-1",
	} {
		if degraded[key] != want {
			t.Errorf("degraded %s = %v, want %q", key, degraded[key], want)
		}
	}
	if reasons, _ := degraded["reasons"].([]any); len(reasons) != 1 || reasons[0] != "audio_underruns" {
		t.Errorf("degraded reasons = %v, want [audio_underruns]", degraded["reasons"])
	}
	metrics, _ := degraded["metrics"].(map[string]any)
	if metrics["audioUnderrunsDelta"] != float64(412) || metrics["audioMinLeadMs"] != float64(-177) {
		t.Errorf("degraded metrics = %v, want audioUnderrunsDelta 412 and audioMinLeadMs -177", degraded["metrics"])
	}

	if got := clientPlaybackEventCount(t, "ios", "heartbeat") - heartbeatsBefore; got != 1 {
		t.Errorf("heartbeat counter moved by %v, want 1", got)
	}
	if got := clientPlaybackEventCount(t, "ios", "degraded") - degradedBefore; got != 1 {
		t.Errorf("degraded counter moved by %v, want 1", got)
	}
}

// Everything the schema limits is refused, whole: a malformed event is a client
// bug, and logging the rest of its batch would hide it. A refused batch leaves
// neither a log line nor a count behind.
func TestPostPlaybackTelemetry_RefusesMalformedBatches(t *testing.T) {
	event := func(extra string) string {
		return `{"kind": "heartbeat", "occurredAt": "2026-09-26T18:29:05Z"` + extra + `}`
	}
	batch := func(events ...string) string {
		return `{"client": {"platform": "tvos"}, "events": [` + strings.Join(events, ",") + `]}`
	}
	repeat := func(n int, s string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = s
		}
		return out
	}
	manyMetrics := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`"m%d": 1`, i)
		}
		return `, "metrics": {` + strings.Join(parts, ",") + `}`
	}

	cases := []struct {
		name string
		body string
	}{
		{"not json", `{`},
		{"unknown field", `{"client": {"platform": "tvos"}, "events": [` + event("") + `], "extra": 1}`},
		{"unknown platform", `{"client": {"platform": "vcr"}, "events": [` + event("") + `]}`},
		{"no events", batch()},
		{"too many events", batch(repeat(21, event(""))...)},
		{"unknown kind", batch(`{"kind": "freeze", "occurredAt": "2026-09-26T18:29:05Z"}`)},
		{"missing occurredAt", batch(`{"kind": "heartbeat"}`)},
		{"metric key not camelCase", batch(event(`, "metrics": {"audio-lead": 1}`))},
		{"too many metrics", batch(event(manyMetrics(49)))},
		{"reason not snake_case", batch(event(`, "reasons": ["Audio Underruns"]`))},
		{"too many reasons", batch(event(`, "reasons": ["a","b","c","d","e","f","g","h","i"]`))},
		{"detail too long", batch(event(`, "detail": "` + strings.Repeat("x", 513) + `"`))},
		{"body too large", batch(event(`, "detail": "` + strings.Repeat("x", 70<<10) + `"`))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readLogs := captureTelemetryLogs(t)
			before := clientPlaybackEventCount(t, "tvos", "heartbeat")

			rr := postTelemetry(t, tc.body, nil)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rr.Code, rr.Body.String())
			}
			if lines := readLogs(); len(lines) != 0 {
				t.Errorf("a refused batch was logged: %v", lines)
			}
			if got := clientPlaybackEventCount(t, "tvos", "heartbeat") - before; got != 0 {
				t.Errorf("a refused batch moved the counter by %v", got)
			}
		})
	}
}
