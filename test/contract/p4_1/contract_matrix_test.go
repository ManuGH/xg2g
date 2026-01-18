// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package p4_1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ContractTestCase represents a P4-1 playback decision contract test case
type ContractTestCase struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Expected    json.RawMessage `json:"expected"`
}

// TestContractMatrix_P4_1 validates playback decision contract using golden snapshots
func TestContractMatrix_P4_1(t *testing.T) {
	casesDir := "../../../testdata/contract/p4_1/cases"
	goldenDir := "../../../testdata/contract/p4_1/golden"

	// Discover all test case files
	entries, err := os.ReadDir(casesDir)
	require.NoError(t, err, "Failed to read test cases directory")

	var caseFiles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".input.json") {
			caseFiles = append(caseFiles, entry.Name())
		}
	}

	require.GreaterOrEqual(t, len(caseFiles), 8, "Contract matrix requires minimum 8 test cases")

	for _, caseFile := range caseFiles {
		t.Run(caseFile, func(t *testing.T) {
			// Load test case
			casePath := filepath.Join(casesDir, caseFile)
			caseData, err := os.ReadFile(casePath)
			require.NoError(t, err, "Failed to load test case")

			var testCase ContractTestCase
			err = json.Unmarshal(caseData, &testCase)
			require.NoError(t, err, "Failed to parse test case JSON")

			// Golden snapshot path
			baseName := strings.TrimSuffix(caseFile, ".input.json")
			goldenPath := filepath.Join(goldenDir, baseName+".expected.json")

			// Call decision engine (STUB for P4-1 contract phase)
			// TODO: Replace with actual handler when engine is implemented
			actualJSON := stubDecisionResponse(t, testCase.Input)

			// Verify golden snapshot
			if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
				// Create golden snapshot
				t.Logf("Creating golden snapshot: %s", goldenPath)
				err = os.WriteFile(goldenPath, actualJSON, 0644)
				require.NoError(t, err, "Failed to write golden snapshot")
			} else {
				// Verify against golden
				expectedData, err := os.ReadFile(goldenPath)
				require.NoError(t, err, "Failed to read golden snapshot")

				// Normalize JSON for comparison
				var expected, actual interface{}
				require.NoError(t, json.Unmarshal(expectedData, &expected))
				require.NoError(t, json.Unmarshal(actualJSON, &actual))

				require.Equal(t, expected, actual, "Response does not match golden snapshot")
			}

			// Contract: verify expected structure matches test case expectation
			var expectedSpec interface{}
			err = json.Unmarshal(testCase.Expected, &expectedSpec)
			require.NoError(t, err, "Failed to parse expected spec")

			var actualResp interface{}
			err = json.Unmarshal(actualJSON, &actualResp)
			require.NoError(t, err, "Failed to parse actual response")

			// Structural validation (status, decision/problem shape)
			verifyContractStructure(t, expectedSpec, actualResp)
		})
	}
}

// stubDecisionResponse returns deterministic stub response for P4-1 contract testing
// TODO: Replace with actual decision engine handler when implemented
func stubDecisionResponse(t *testing.T, inputJSON json.RawMessage) []byte {
	t.Helper()

	var input map[string]interface{}
	err := json.Unmarshal(inputJSON, &input)
	require.NoError(t, err)

	caps, hasCaps := input["capabilities"]
	apiVersion := input["api_version"]
	source, _ := input["source"].(map[string]interface{})

	// Fail-closed: unknown/missing media truth (indeterminate decision)
	if source != nil {
		srcVideo, _ := source["video_codec"].(string)
		srcAudio, _ := source["audio_codec"].(string)
		if srcVideo == "unknown" || srcAudio == "unknown" || srcVideo == "" || srcAudio == "" {
			return []byte(`{
				"status": 422,
				"problem": {
					"type": "about:blank",
					"title": "Unprocessable Entity",
					"status": 422,
					"code": "decision_ambiguous",
					"detail": "Media truth unavailable or unknown (cannot make deterministic decision)"
				}
			}`)
		}
	}

	// Fail-closed: missing capabilities
	if !hasCaps || caps == nil {
		if apiVersion == "v3.1" {
			return []byte(`{
				"status": 412,
				"problem": {
					"type": "about:blank",
					"title": "Precondition Failed",
					"status": 412,
					"code": "capabilities_missing",
					"detail": "Client must provide capabilities (capabilities_version required)"
				}
			}`)
		}
	}

	// Fail-closed: invalid capabilities version
	if capsMap, ok := caps.(map[string]interface{}); ok {
		if version, ok := capsMap["capabilities_version"].(float64); ok && version != 1 {
			return []byte(`{
				"status": 400,
				"problem": {
					"type": "about:blank",
					"title": "Bad Request",
					"status": 400,
					"code": "capabilities_invalid",
					"detail": "capabilities_version 999 not supported (current: 1)"
				}
			}`)
		}

		// Deterministic deny: incompatible source + transcode disabled = policy block (not error)
		source, _ := input["source"].(map[string]interface{})
		policy, _ := input["policy"].(map[string]interface{})
		allowTranscode, _ := policy["allow_transcode"].(bool)

		if source != nil && !allowTranscode {
			srcVideo, _ := source["video_codec"].(string)
			srcAudio, _ := source["audio_codec"].(string)

			capsVideoCodecs, _ := capsMap["video_codecs"].([]interface{})
			capsAudioCodecs, _ := capsMap["audio_codecs"].([]interface{})

			videoCompatible := false
			audioCompatible := false

			for _, codec := range capsVideoCodecs {
				if codecStr, ok := codec.(string); ok && codecStr == srcVideo {
					videoCompatible = true
					break
				}
			}

			for _, codec := range capsAudioCodecs {
				if codecStr, ok := codec.(string); ok && codecStr == srcAudio {
					audioCompatible = true
					break
				}
			}

			// Deterministic decision: policy blocks transcode, no compatible direct path
			if !videoCompatible || !audioCompatible {
				return []byte(`{
					"status": 200,
					"decision": {
						"mode": "deny",
						"selected": {
							"container": "none",
							"video_codec": "none",
							"audio_codec": "none"
						},
						"outputs": [],
						"constraints": [],
						"reasons": ["policy_denies_transcode"],
						"trace": {
							"request_id": "stub-request-id"
						}
					}
				}`)
			}
		}
	}

	// Stub: return NOT_IMPLEMENTED for valid inputs (engine not yet implemented)
	return []byte(`{
		"status": 200,
		"decision": {
			"mode": "deny",
			"selected": {
				"container": null,
				"video_codec": null,
				"audio_codec": null
			},
			"outputs": [],
			"constraints": [],
			"reasons": ["NOT_IMPLEMENTED_YET"],
			"trace": {
				"request_id": "stub-request-id"
			}
		}
	}`)
}

// verifyContractStructure validates response matches expected contract structure
func verifyContractStructure(t *testing.T, expected, actual interface{}) {
	t.Helper()

	expectedMap, ok := expected.(map[string]interface{})
	if !ok {
		return
	}

	actualMap, ok := actual.(map[string]interface{})
	require.True(t, ok, "Actual response must be object")

	// Verify HTTP status
	if expectedStatus, ok := expectedMap["status"].(float64); ok {
		actualStatus, ok := actualMap["status"].(float64)
		require.True(t, ok, "Response must have status")
		require.Equal(t, expectedStatus, actualStatus, "HTTP status mismatch")
	}

	// Verify decision or problem structure
	if expectedDecision, ok := expectedMap["decision"].(map[string]interface{}); ok {
		actualDecision, ok := actualMap["decision"].(map[string]interface{})
		require.True(t, ok, "Response must have decision block")

		// Required decision fields
		require.Contains(t, actualDecision, "mode", "Decision must have mode")
		require.Contains(t, actualDecision, "selected", "Decision must have selected")
		require.Contains(t, actualDecision, "outputs", "Decision must have outputs")
		require.Contains(t, actualDecision, "constraints", "Decision must have constraints")
		require.Contains(t, actualDecision, "reasons", "Decision must have reasons")
		require.Contains(t, actualDecision, "trace", "Decision must have trace")

		// Verify trace.request_id_present if specified
		if trace, ok := expectedDecision["trace"].(map[string]interface{}); ok {
			if reqIDPresent, ok := trace["request_id_present"].(bool); ok && reqIDPresent {
				actualTrace, ok := actualDecision["trace"].(map[string]interface{})
				require.True(t, ok, "Trace must be object")
				_, hasReqID := actualTrace["request_id"]
				require.True(t, hasReqID, "Trace must have request_id")
			}
		}
	}

	if expectedProblem, ok := expectedMap["problem"].(map[string]interface{}); ok {
		actualProblem, ok := actualMap["problem"].(map[string]interface{})
		require.True(t, ok, "Response must have problem block")

		// RFC7807 required fields
		require.Contains(t, actualProblem, "type", "Problem must have type")
		require.Contains(t, actualProblem, "title", "Problem must have title")
		require.Contains(t, actualProblem, "status", "Problem must have status")

		// Verify code
		if expectedCode, ok := expectedProblem["code"].(string); ok {
			actualCode, ok := actualProblem["code"].(string)
			require.True(t, ok, "Problem must have code")
			require.Equal(t, expectedCode, actualCode, "Problem code mismatch")
		}
	}
}
