// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package p4_1_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
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
			// TODO: Replace with actual handler
			actualJSON := stubDecisionResponse(testCase.Input)

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
func stubDecisionResponse(inputJSON []byte) []byte {
	// Parse input
	var input map[string]interface{}
	err := json.Unmarshal(inputJSON, &input)
	if err != nil {
		panic("failed to parse input: " + err.Error())
	}

	// Extract fields for decision engine
	source, _ := input["source"].(map[string]interface{})
	capsRaw, hasCaps := input["capabilities"]
	policy, _ := input["policy"].(map[string]interface{})
	apiVersion, _ := input["api_version"].(string)

	// Build decision.DecisionInput
	decInput := decision.DecisionInput{
		RequestID:  "stub-request-id",
		APIVersion: apiVersion,
	}

	// Source (media truth)
	if source != nil {
		decInput.Source = decision.Source{
			Container:   getStringOrEmpty(source, "container"),
			VideoCodec:  getStringOrEmpty(source, "video_codec"),
			AudioCodec:  getStringOrEmpty(source, "audio_codec"),
			BitrateKbps: getIntOrZero(source, "bitrate_kbps"),
		}
	}

	// Capabilities
	if hasCaps {
		if capsMap, ok := capsRaw.(map[string]interface{}); ok {
			decInput.Capabilities = decision.Capabilities{
				Version:     getIntOrZero(capsMap, "capabilities_version"),
				Containers:  getStringSlice(capsMap, "container"),
				VideoCodecs: getStringSlice(capsMap, "video_codecs"),
				AudioCodecs: getStringSlice(capsMap, "audio_codecs"),
				SupportsHLS: getBoolOrFalse(capsMap, "supports_hls"),
				DeviceType:  getStringOrEmpty(capsMap, "device_type"),
			}
		}
	}

	// Policy
	if policy != nil {
		decInput.Policy = decision.Policy{
			AllowTranscode: getBoolOrFalse(policy, "allow_transcode"),
		}
	}

	// Call decision engine
	status, dec, prob := decision.Decide(context.Background(), decInput, "test")

	// Marshal response
	var response interface{}
	if prob != nil {
		response = map[string]interface{}{
			"status":  status,
			"problem": prob,
		}
	} else {
		decisionMap := make(map[string]any)
		decBytes, _ := json.Marshal(dec)
		json.Unmarshal(decBytes, &decisionMap)
		normalizeDecisionKeys(decisionMap)

		response = map[string]interface{}{
			"status":   status,
			"decision": decisionMap,
		}
	}

	responseJSON, err := json.MarshalIndent(response, "\t\t\t\t", "\t")
	if err != nil {
		panic("failed to marshal response: " + err.Error())
	}

	return responseJSON
}

// Helper functions to extract typed values from map[string]interface{}
func getStringOrEmpty(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getIntOrZero(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getBoolOrFalse(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return []string{}
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

		// Verify trace.requestIdPresent if specified
		if trace, ok := expectedDecision["trace"].(map[string]interface{}); ok {
			if reqIDPresent, ok := trace["requestIdPresent"].(bool); ok && reqIDPresent {
				actualTrace, ok := actualDecision["trace"].(map[string]interface{})
				require.True(t, ok, "Trace must be object")
				_, hasReqID := actualTrace["requestId"]
				require.True(t, hasReqID, "Trace must have requestId")
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
func normalizeDecisionKeys(m map[string]interface{}) {
	remap := map[string]string{
		"audioCodec":         "audio_codec",
		"videoCodec":         "video_codec",
		"selectedOutputUrl":  "selected_output_url",
		"selectedOutputKind": "selected_output_kind",
	}

	for oldKey, newKey := range remap {
		if val, ok := m[oldKey]; ok {
			m[newKey] = val
			delete(m, oldKey)
		}
	}

	// Recursive for nested maps
	for _, v := range m {
		if nm, ok := v.(map[string]interface{}); ok {
			normalizeDecisionKeys(nm)
		}
	}
}
