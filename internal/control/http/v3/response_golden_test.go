package v3

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV3_ResponseGolden(t *testing.T) {

	t.Run("PlaybackInfo_JSON_Shape", func(t *testing.T) {
		reqID := "req_test_123"
		sessionID := "sess_abc"

		// Create a representative PlaybackInfo DTO
		// posSeconds=120, durationSeconds=3600
		info := PlaybackInfo{
			RequestId: reqID,
			SessionId: sessionID,
			Mode:      PlaybackInfoModeHls,
			Url:       strPtr("/api/v3/recordings/test/playlist.m3u8"),
			Resume: &ResumeSummary{
				PosSeconds:      120, // int64
				DurationSeconds: int64Ptr(3600),
			},
		}

		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(info)
		require.NoError(t, err)

		responseJSON := w.Body.String()

		// 1. Assert physical shape (regex check for integer vs float)
		// We expect "posSeconds":120 not "posSeconds":120.0
		assert.Contains(t, responseJSON, `"posSeconds":120`)
		assert.NotContains(t, responseJSON, `"posSeconds":120.0`)

		// 2. Assert requestId presence
		assert.Contains(t, responseJSON, `"requestId":"req_test_123"`)

		// 3. Round-trip validation
		var decoded map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &decoded)
		require.NoError(t, err)

		resume, ok := decoded["resume"].(map[string]any)
		require.True(t, ok, "resume should be an object")

		pos, ok := resume["posSeconds"].(float64) // JSON numbers decode to float64 in generic maps
		require.True(t, ok)
		assert.Equal(t, float64(120), pos)

		// Check that it's an integer in the raw JSON
		// (json.Decoder can use UseNumber() for more precision, but raw string check is better for proof)
		assert.False(t, strings.Contains(responseJSON, `:120.`)) // Check for decimal point specifically in the value
	})

	t.Run("ProblemDetails_JSON_Shape", func(t *testing.T) {
		prob := ProblemDetails{
			Type:      "test/error",
			Title:     "Test Error",
			Status:    500,
			RequestId: "req_error_456",
			Code:      strPtr("INTERNAL_ERROR"),
		}

		w := httptest.NewRecorder()
		err := json.NewEncoder(w).Encode(prob)
		require.NoError(t, err)

		responseJSON := w.Body.String()

		// Assert mandatory structure
		assert.Contains(t, responseJSON, `"type":"test/error"`)
		assert.Contains(t, responseJSON, `"title":"Test Error"`)
		assert.Contains(t, responseJSON, `"status":500`)
		assert.Contains(t, responseJSON, `"requestId":"req_error_456"`)
	})
}
