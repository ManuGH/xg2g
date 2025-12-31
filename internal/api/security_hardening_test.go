package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestCreateSession_SecurityCheck validates token-only session creation.
func TestCreateSession_SecurityCheck(t *testing.T) {
	tests := []struct {
		name           string
		cfg            config.AppConfig
		reqHeader      string
		expectedStatus int
		expectCookie   bool
		expectSecure   bool
	}{
		{
			name: "NoTokenConfigured_NoToken",
			cfg: config.AppConfig{
				APIToken: "",
			},
			expectedStatus: http.StatusUnauthorized,
			expectCookie:   false,
		},
		{
			name: "AuthRequired_NoToken",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{"v3:read"},
			},
			expectedStatus: http.StatusUnauthorized,
			expectCookie:   false,
		},
		{
			name: "AuthRequired_ValidToken",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{"v3:read"},
			},
			reqHeader:      "Bearer secret",
			expectedStatus: http.StatusOK,
			expectCookie:   true,
			expectSecure:   false, // Default
		},
		{
			name: "AuthRequired_ValidToken_ForceHTTPS",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{"v3:read"},
				ForceHTTPS:     true,
			},
			reqHeader:      "Bearer secret",
			expectedStatus: http.StatusOK,
			expectCookie:   true,
			expectSecure:   true,
		},
		{
			name: "AuthRequired_InvalidToken",
			cfg: config.AppConfig{
				APIToken:       "secret",
				APITokenScopes: []string{"v3:read"},
			},
			reqHeader:      "Bearer wrong",
			expectedStatus: http.StatusUnauthorized,
			expectCookie:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{
				cfg: tt.cfg,
			}
			// Important: s.tokenScopes uses s.mu.RLock so we need to initialize lock or Server properly
			// However s.tokenScopes reads s.cfg directly in our refactored code (via GetConfig)
			// Wait, the refactored code uses GetConfig() which reads s.mu.RLock().
			// We need to use a real server or mock GetConfig?
			// The Server struct has s.mu RWMutex embedded. Zero struct is fine.

			req := httptest.NewRequest("POST", "/api/v3/auth/session", nil)
			if tt.reqHeader != "" {
				req.Header.Set("Authorization", tt.reqHeader)
			}
			w := httptest.NewRecorder()

			s.CreateSession(w, req)
			assert.Equal(t, tt.expectedStatus, w.Code)

			cookies := w.Result().Cookies()
			if tt.expectCookie {
				assert.NotEmpty(t, cookies, "Expected session cookie")
				found := false
				for _, c := range cookies {
					if c.Name == "xg2g_session" {
						found = true
						assert.True(t, c.HttpOnly, "Cookie must be HttpOnly")
						assert.Equal(t, tt.expectSecure, c.Secure, "Cookie Secure flag mismatch")

						// Strict check: cookie value must equal raw token
						expectedToken := strings.TrimPrefix(tt.reqHeader, "Bearer ")
						assert.Equal(t, expectedToken, c.Value, "Cookie value must match presented token")
					}
				}
				assert.True(t, found, "xg2g_session cookie not found")
			} else {
				assert.Empty(t, cookies, "Expected no cookies in unauthenticated path")
			}
		})
	}
}

func TestReloadRequiresRestart_Hardening(t *testing.T) {
	baseCfg := config.AppConfig{
		APIToken:       "A",
		APITokenScopes: []string{"v3:write"},
		APITokens: []config.ScopedToken{
			{Token: "T1", Scopes: []string{"v3:read"}},
		},
	}

	tests := []struct {
		name     string
		mod      func(*config.AppConfig)
		mustRest bool
	}{
		{
			name:     "NoChange",
			mod:      func(c *config.AppConfig) {},
			mustRest: false,
		},
		{
			name: "TokenRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokens[0].Token = "T2" // content changed
			},
			mustRest: true,
		},
		{
			name: "ScopeRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokens[0].Scopes[0] = "v3:admin" // content changed
			},
			mustRest: true,
		},
		{
			name: "TopLevelScopeRotation_SameLen",
			mod: func(c *config.AppConfig) {
				c.APITokenScopes[0] = "v3:admin" // content changed
			},
			mustRest: true,
		},
		{
			name: "NewToken",
			mod: func(c *config.AppConfig) {
				c.APITokens = append(c.APITokens, config.ScopedToken{Token: "T2", Scopes: []string{"v3:read"}})
			},
			mustRest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newCfg := baseCfg
			// We need deep copy for base config slices to simulate real world,
			// otherwise we mutate baseCfg across tests in memory.
			// Helper to deep copy struct manually for test
			newCfg.APITokenScopes = append([]string(nil), baseCfg.APITokenScopes...)
			newCfg.APITokens = make([]config.ScopedToken, len(baseCfg.APITokens))
			for i, v := range baseCfg.APITokens {
				newCfg.APITokens[i] = v
				newCfg.APITokens[i].Scopes = append([]string(nil), v.Scopes...)
			}

			tt.mod(&newCfg)

			// reloadRequiresRestart is unexported. We are in package api so we can call it.
			got := reloadRequiresRestart(baseCfg, newCfg)
			assert.Equal(t, tt.mustRest, got)
		})
	}
}
