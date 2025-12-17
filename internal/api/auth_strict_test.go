// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_FailClosed(t *testing.T) {
	tests := []struct {
		name           string
		cfg            config.AppConfig
		headerKey      string
		headerVal      string
		expectedStatus int
	}{
		{
			name:           "No Token, No AuthAnonymous -> Fail Closed (401)",
			cfg:            config.AppConfig{APIToken: "", AuthAnonymous: false},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "No Token, AuthAnonymous=True -> Allow (200)",
			cfg:            config.AppConfig{APIToken: "", AuthAnonymous: true},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Token Set, No Header -> Fail (401)",
			cfg:            config.AppConfig{APIToken: "secret"},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Token Set, Wrong Header -> Make sure it doesn't bypass",
			cfg:            config.AppConfig{APIToken: "secret"},
			headerKey:      "Authorization",
			headerVal:      "Bearer wrong",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Token Set, Correct Header -> Allow (200)",
			cfg:            config.AppConfig{APIToken: "secret"},
			headerKey:      "Authorization",
			headerVal:      "Bearer secret",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg, nil)
			// Apply the middleware to a dummy handler
			handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			require.NoError(t, err)

			if tt.headerKey != "" {
				req.Header.Set(tt.headerKey, tt.headerVal)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
		})
	}
}
