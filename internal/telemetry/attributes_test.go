// SPDX-License-Identifier: MIT
package telemetry

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestHTTPAttributes(t *testing.T) {
	attrs := HTTPAttributes("GET", "/api/v1/status", "http://localhost:8080/api/v1/status", 200)

	if len(attrs) != 4 {
		t.Fatalf("Expected 4 attributes, got %d", len(attrs))
	}

	verifyAttribute(t, attrs, HTTPMethodKey, "GET")
	verifyAttribute(t, attrs, HTTPRouteKey, "/api/v1/status")
	verifyAttribute(t, attrs, HTTPURLKey, "http://localhost:8080/api/v1/status")
	verifyIntAttribute(t, attrs, HTTPStatusCodeKey, 200)
}

func TestStreamAttributes(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		serviceID string
		bouquet   string
		wantLen   int
	}{
		{
			name:      "all fields",
			channel:   "BBC One",
			serviceID: "1234",
			bouquet:   "favourites",
			wantLen:   3,
		},
		{
			name:      "only channel",
			channel:   "BBC One",
			serviceID: "",
			bouquet:   "",
			wantLen:   1,
		},
		{
			name:      "empty fields",
			channel:   "",
			serviceID: "",
			bouquet:   "",
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := StreamAttributes(tt.channel, tt.serviceID, tt.bouquet)

			if len(attrs) != tt.wantLen {
				t.Errorf("Expected %d attributes, got %d", tt.wantLen, len(attrs))
			}

			if tt.channel != "" {
				verifyAttribute(t, attrs, StreamChannelKey, tt.channel)
			}
			if tt.serviceID != "" {
				verifyAttribute(t, attrs, StreamServiceIDKey, tt.serviceID)
			}
			if tt.bouquet != "" {
				verifyAttribute(t, attrs, StreamBouquetKey, tt.bouquet)
			}
		})
	}
}

func TestTranscodeAttributes(t *testing.T) {
	attrs := TranscodeAttributes("h264", "hevc", "vaapi", 4000000, true)

	if len(attrs) != 5 {
		t.Fatalf("Expected 5 attributes, got %d", len(attrs))
	}

	verifyAttribute(t, attrs, TranscodeInputCodecKey, "h264")
	verifyAttribute(t, attrs, TranscodeOutputCodecKey, "hevc")
	verifyAttribute(t, attrs, TranscodeDeviceKey, "vaapi")
	verifyIntAttribute(t, attrs, TranscodeBitrateKey, 4000000)
	verifyBoolAttribute(t, attrs, TranscodeGPUEnabledKey, true)
}

func TestEPGAttributes(t *testing.T) {
	attrs := EPGAttributes(7, 100, 1500, 5, 3)

	if len(attrs) != 5 {
		t.Fatalf("Expected 5 attributes, got %d", len(attrs))
	}

	verifyIntAttribute(t, attrs, EPGDaysKey, 7)
	verifyIntAttribute(t, attrs, EPGChannelsKey, 100)
	verifyIntAttribute(t, attrs, EPGEventsKey, 1500)
	verifyIntAttribute(t, attrs, EPGConcurrencyKey, 5)
	verifyIntAttribute(t, attrs, EPGRetriesKey, 3)
}

func TestJobAttributes(t *testing.T) {
	attrs := JobAttributes("epg-refresh", "completed", 45000)

	if len(attrs) != 3 {
		t.Fatalf("Expected 3 attributes, got %d", len(attrs))
	}

	verifyAttribute(t, attrs, JobTypeKey, "epg-refresh")
	verifyAttribute(t, attrs, JobStatusKey, "completed")
	verifyInt64Attribute(t, attrs, JobDurationKey, 45000)
}

func TestErrorAttributes(t *testing.T) {
	err := errors.New("test error")
	attrs := ErrorAttributes(err, "network_error")

	if len(attrs) != 2 {
		t.Fatalf("Expected 2 attributes, got %d", len(attrs))
	}

	verifyBoolAttribute(t, attrs, ErrorKey, true)
	verifyAttribute(t, attrs, ErrorTypeKey, "network_error")
}

func TestAttributeKeys_Consistency(t *testing.T) {
	// Verify attribute keys follow OpenTelemetry conventions
	keys := []string{
		HTTPMethodKey,
		HTTPStatusCodeKey,
		HTTPRouteKey,
		StreamChannelKey,
		TranscodeCodecKey,
		EPGDaysKey,
		JobTypeKey,
		ErrorKey,
	}

	for _, key := range keys {
		if key == "" {
			t.Errorf("Expected non-empty attribute key")
		}
	}
}

// Helper functions for attribute verification

func verifyAttribute(t *testing.T, attrs []attribute.KeyValue, key, expectedValue string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsString() != expectedValue {
				t.Errorf("Expected %s=%s, got %s", key, expectedValue, attr.Value.AsString())
			}
			return
		}
	}
	t.Errorf("Attribute %s not found", key)
}

func verifyIntAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expectedValue int) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsInt64() != int64(expectedValue) {
				t.Errorf("Expected %s=%d, got %d", key, expectedValue, attr.Value.AsInt64())
			}
			return
		}
	}
	t.Errorf("Attribute %s not found", key)
}

func verifyInt64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expectedValue int64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsInt64() != expectedValue {
				t.Errorf("Expected %s=%d, got %d", key, expectedValue, attr.Value.AsInt64())
			}
			return
		}
	}
	t.Errorf("Attribute %s not found", key)
}

func verifyBoolAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expectedValue bool) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsBool() != expectedValue {
				t.Errorf("Expected %s=%t, got %t", key, expectedValue, attr.Value.AsBool())
			}
			return
		}
	}
	t.Errorf("Attribute %s not found", key)
}
