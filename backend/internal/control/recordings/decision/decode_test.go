package decision

import (
	"encoding/json"
	"testing"
)

// TestDecodeDecisionInput_FailClosed_UnknownRootKey tests fail-closed behavior.
// Invariant INV-001: Unknown root keys must be rejected with 400.
func TestDecodeDecisionInput_FailClosed_UnknownRootKey(t *testing.T) {
	input := `{
		"source": {"c": "mp4"},
		"caps": {"c": ["mp4"], "vc": ["h264"], "ac": ["aac"]},
		"policy": {},
		"unknown_key": "value"
	}`

	_, problem := DecodeDecisionInput([]byte(input))
	if problem == nil {
		t.Fatal("DecodeDecisionInput() expected error but got none")
	}
	if problem.Status != 400 {
		t.Errorf("DecodeDecisionInput() expected 400 status, got %d", problem.Status)
	}
	if problem.Code != string(ProblemCapabilitiesInvalid) {
		t.Errorf("DecodeDecisionInput() expected code %q, got %q", ProblemCapabilitiesInvalid, problem.Code)
	}
}

// TestDecodeDecisionInput_RootObjectsAreObjects tests INV-009.
// Invariant: Root objects must be JSON objects, not primitives/null/arrays.
func TestDecodeDecisionInput_RootObjectsAreObjects(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name: "caps_is_string",
			input: `{
				"source": {"c": "mp4"},
				"caps": "not an object",
				"policy": {}
			}`,
			expectError: true,
		},
		{
			name: "caps_is_number",
			input: `{
				"source": {"c": "mp4"},
				"caps": 42,
				"policy": {}
			}`,
			expectError: true,
		},
		{
			name: "policy_is_null",
			input: `{
				"source": {"c": "mp4"},
				"caps": {"c": ["mp4"], "vc": ["h264"], "ac": ["aac"]},
				"policy": null
			}`,
			expectError: true,
		},
		{
			name: "policy_is_array",
			input: `{
				"source": {"c": "mp4"},
				"caps": {"c": ["mp4"], "vc": ["h264"], "ac": ["aac"]},
				"policy": []
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, problem := DecodeDecisionInput([]byte(tt.input))
			if tt.expectError && problem == nil {
				t.Errorf("DecodeDecisionInput() expected error but got none")
			}
			if !tt.expectError && problem != nil {
				t.Errorf("DecodeDecisionInput() unexpected error: %+v", problem)
			}
			if problem != nil && problem.Status != 400 {
				t.Errorf("DecodeDecisionInput() expected 400 status, got %d", problem.Status)
			}
		})
	}
}

// TestGetSchemaType tests schema type detection.
// Invariant: Correct schema type must be detected for compact, legacy, and mixed inputs.
func TestGetSchemaType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "compact_schema",
			input:    `{"source": {}, "caps": {}}`,
			expected: "compact",
		},
		{
			name:     "legacy_schema",
			input:    `{"Source": {}, "Capabilities": {}}`,
			expected: "legacy",
		},
		{
			name:     "mixed_schema",
			input:    `{"source": {}, "Source": {}, "caps": {}}`,
			expected: "mixed",
		},
		{
			name:     "unknown_keys",
			input:    `{"other": "value"}`,
			expected: "unknown",
		},
		{
			name:     "malformed",
			input:    `not json`,
			expected: "unknown",
		},
		{
			name:     "empty",
			input:    `{}`,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSchemaType([]byte(tt.input))
			if result != tt.expected {
				t.Errorf("GetSchemaType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestIsJSONObject tests the internal isJSONObject helper.
// Documents behavior for structure validation.
func TestIsJSONObject(t *testing.T) {
	tests := []struct {
		name     string
		input    json.RawMessage
		expected bool
	}{
		{name: "object", input: json.RawMessage(`{}`), expected: true},
		{name: "object_with_whitespace", input: json.RawMessage(`  {  }`), expected: true},
		{name: "array", input: json.RawMessage(`[]`), expected: false},
		{name: "string", input: json.RawMessage(`"test"`), expected: false},
		{name: "number", input: json.RawMessage(`42`), expected: false},
		{name: "null", input: json.RawMessage(`null`), expected: false},
		{name: "boolean", input: json.RawMessage(`true`), expected: false},
		{name: "empty", input: json.RawMessage(``), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJSONObject(tt.input)
			if result != tt.expected {
				t.Errorf("isJSONObject(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
