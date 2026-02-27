package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCORS_OriginReflection_WithCredentials(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name              string
		origin            string
		expectACAO        string // Access-Control-Allow-Origin
		expectCredentials string // Access-Control-Allow-Credentials
		expectVary        string
		expectNoWildcard  bool // ACAO must NOT be "*" when credentials are sent
	}{
		{
			name:              "Specific Origin reflected with Credentials",
			origin:            "https://example.invalid",
			expectACAO:        "https://example.invalid",
			expectCredentials: "true",
			expectVary:        "Origin",
			expectNoWildcard:  true,
		},
		{
			name:              "Different Origin reflected correctly",
			origin:            "https://other.example.com",
			expectACAO:        "https://other.example.com",
			expectCredentials: "true",
			expectVary:        "Origin",
			expectNoWildcard:  true,
		},
		{
			name:              "No Origin = no CORS headers",
			origin:            "",
			expectACAO:        "",
			expectCredentials: "",
			expectVary:        "",
			expectNoWildcard:  true,
		},
	}

	for _, tt := range tests {
		t.Run("OPTIONS/"+tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, "/api/v3/recordings/test/resume", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()
			s.HandleRecordingResumeOptions(w, req)

			res := w.Result()
			assert.Equal(t, tt.expectACAO, res.Header.Get("Access-Control-Allow-Origin"),
				"ACAO must reflect the request Origin, never *")
			assert.Equal(t, tt.expectCredentials, res.Header.Get("Access-Control-Allow-Credentials"))
			assert.Equal(t, tt.expectVary, res.Header.Get("Vary"),
				"Vary: Origin is required to prevent cache-based origin leaks")

			if tt.expectNoWildcard {
				assert.NotEqual(t, "*", res.Header.Get("Access-Control-Allow-Origin"),
					"ACAO must NEVER be * when Credentials is true (Fetch spec violation)")
			}
		})
	}
}
