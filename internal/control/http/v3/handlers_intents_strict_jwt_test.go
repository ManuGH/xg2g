package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func generateTestToken(t *testing.T, claims auth.TokenClaims, secret []byte) string {
	t.Helper()
	token, err := auth.GenerateHS256(secret, claims, "kid-v1")
	if err != nil {
		t.Fatalf("failed to generate test token: %v", err)
	}
	return token
}

func startStreamReqWithToken(svcRef string, token *string) *http.Request {
	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            svcRef,
		PlaybackDecisionToken: token,
		Params:                map[string]string{"mode": "live"},
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHandleV3Intents_FailClosed_Validation(t *testing.T) {
	// 1. Setup minimal fake server environment
	cfg := config.AppConfig{}

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.DefaultDecisionSecret,
	}
	s.SetDependencies(Dependencies{
		Bus:               &dummyBus{},
		Store:             &dummyStore{},
		PreflightProvider: &dummyPreflight{},
		Scan:              &dummyScanner{},
	})
	// Note: We're isolating the handler's Phase 1 & 2 logic, so we don't need a full Store/Bus here.
	// But because handleV3Intents checks for nil deps, we must provide dummy ones to get past line 52.

	// Create a wrapper test handler. We'll simply let the handler hit the validation
	// rules. As soon as it fails, it will write the problem. If it passes validation,
	// it drops down to the mock logic (which might panic or fail, but we only care about 401/403 gates).

	jwtSecret := auth.DefaultDecisionSecret
	validRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	normValidRef := normalize.ServiceRef(validRef)
	now := time.Now().Unix()

	validClaims := auth.TokenClaims{
		Iss:  "xg2g",
		Aud:  "xg2g/v3/intents",
		Sub:  normValidRef,
		Jti:  "test-uuid-1",
		Iat:  now,
		Nbf:  now - 10,
		Exp:  now + 60,
		Mode: "live",
	}

	tests := []struct {
		name           string
		reqFunc        func() *http.Request
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "Missing Token -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				return startStreamReqWithToken(validRef, nil)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_MISSING",
		},
		{
			name: "Expired Token -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				c := validClaims
				c.Nbf = now - 100
				c.Iat = now - 100
				c.Exp = now - 40 // Past skew
				tok := generateTestToken(t, c, jwtSecret)
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "Algorithm None (Manipulated) -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				// Base64 encode header {"alg":"none"}, claims, no signature
				tok := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJpc3MiOiJ4ZzJnIiwiYXVkIjoieGcyZy92My9pbnRlbnRzIiwic3ViIjoiMSIsImV4cCI6MjAwMDAwMDAwMH0."
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "Mismatched Issuer -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				c := validClaims
				c.Iss = "hacker"
				tok := generateTestToken(t, c, jwtSecret)
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "ServiceRef Match Fails (Sub Claim) -> 403 Forbidden",
			reqFunc: func() *http.Request {
				c := validClaims
				c.Sub = normalize.ServiceRef("1:0:19:DIFFERENT:CHANNEL:")
				tok := generateTestToken(t, c, jwtSecret)
				return startStreamReqWithToken(validRef, &tok) // sending validRef but token is for DIFFERENT
			},
			expectedStatus: http.StatusForbidden,
			expectedCode:   "CLAIM_MISMATCH",
		},
		{
			name: "Mode Overridden by Client -> 403 Forbidden",
			reqFunc: func() *http.Request {
				c := validClaims
				c.Mode = "direct_stream" // Server decided direct_stream
				tok := generateTestToken(t, c, jwtSecret)

				// Client attempts to force hlsjs despite token saying direct_stream
				req := startStreamReqWithToken(validRef, &tok)
				return req
			},
			expectedStatus: http.StatusForbidden,
			expectedCode:   "CLAIM_MISMATCH",
		},
		{
			name: "Capabilities_Manipulated_->_403_Forbidden",
			reqFunc: func() *http.Request {
				// Token bounded to capHash but we send random intent parameters not matching it
				modClaims := validClaims
				modClaims.CapHash = "some-expected-hash"
				tokenStr, _ := v3auth.GenerateHS256(jwtSecret, modClaims, "kid-v1")

				reqBody := v3api.IntentRequest{
					Type:                  "stream.start",
					ServiceRef:            "1:0:1:C35C:271A:F001:FFFF0000:0:0:0:",
					PlaybackDecisionToken: &tokenStr,
					Params:                map[string]string{"mode": "live", "profile": "HD"},
				}
				b, _ := json.Marshal(reqBody)
				return httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(b))
			},
			expectedStatus: http.StatusForbidden,
			expectedCode:   "CLAIM_MISMATCH",
		},

		// ---- Gate B: JWT Parser Strictness (no panic on malformed input) ----
		{
			name: "Empty Token String -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				empty := ""
				return startStreamReqWithToken(validRef, &empty)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_MISSING",
		},
		{
			name: "Garbage Base64 Token -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				garbage := "not.a.jwt!@#$%^&*()"
				return startStreamReqWithToken(validRef, &garbage)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "Missing Alg In Header -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				// Header: {"typ":"JWT"} (no alg), valid base64 but alg missing
				// eyJ0eXAiOiJKV1QifQ == {"typ":"JWT"}
				tok := "eyJ0eXAiOiJKV1QifQ.eyJpc3MiOiJ4ZzJnIn0.invalidsig"
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "Token With Trailing Whitespace -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				c := validClaims
				tok := generateTestToken(t, c, jwtSecret) + "   "
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},
		{
			name: "Token Signed With Wrong Secret -> 401 Unauthorized",
			reqFunc: func() *http.Request {
				wrongSecret := []byte("completely-different-secret-key!!")
				tok := generateTestToken(t, validClaims, wrongSecret)
				return startStreamReqWithToken(validRef, &tok)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "TOKEN_INVALID",
		},

		// ---- Gate A: E2E Cross-Ref Claim Mismatch ----
		{
			name: "Token for RefA used with RefB -> 403 Forbidden (E2E Claim Mismatch)",
			reqFunc: func() *http.Request {
				// Token was issued for validRef (channel A)
				c := validClaims
				tok := generateTestToken(t, c, jwtSecret)
				// But the intent request sends a DIFFERENT serviceRef (channel B)
				differentRef := "1:0:19:AAAA:BBBB:1:C00000:0:0:0:"
				return startStreamReqWithToken(differentRef, &tok)
			},
			expectedStatus: http.StatusForbidden,
			expectedCode:   "CLAIM_MISMATCH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.reqFunc()
			w := httptest.NewRecorder()

			// Instead of full deps, we inject directly into handleV3Intents logic
			// by just calling the route. Note: if deps are nil, lines 43-52 might fail
			// with IntentErrV3Unavailable. Let's patch that check out or mock deps if necessary.

			// Actually handleV3Intents requires valid state/bus. The simplest way to test
			// is invoking the function that handles it if we can wire a dummy.
			s.handleV3Intents(w, req)

			res := w.Result()
			if res.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, res.StatusCode)
			}

			// If we expect V3Unavailable because we didn't mock deps, that's a 503 instead of 401.
			// Let's verify our gate hits first before the deps check, or we must mock deps.
			// Ah, the JWT gate is right after the deps check in the code snippet.

			var prob map[string]interface{}
			_ = json.NewDecoder(res.Body).Decode(&prob)

			// If it hit the deps check, it'll be 503. So we see our code failed.
			if code, ok := prob["code"].(string); ok {
				if code != tt.expectedCode {
					t.Errorf("expected problem code %q, got %q", tt.expectedCode, code)
				}
			} else {
				t.Errorf("missing problem code in response: %v", prob)
			}
		})
	}
}

// Dummy implementions to bypass the deps check
type dummyBus struct{}

func (d *dummyBus) Publish(ctx context.Context, topic string, payload interface{}) error {
	return nil
}
func (d *dummyBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	return &dummySubscriber{}, nil
}

type dummySubscriber struct{}

func (d *dummySubscriber) C() <-chan bus.Message { return nil }
func (d *dummySubscriber) Close() error          { return nil }

type dummyStore struct{}

func (d *dummyStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}

func (d *dummyStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return nil, nil
}

func (d *dummyStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, nil
}

func (d *dummyStore) PutSessionWithIdempotency(ctx context.Context, session *model.SessionRecord, idempotencyKey string, expiration time.Duration) (string, bool, error) {
	return "", false, nil
}

type dummyScanner struct{}

func (s *dummyScanner) GetCapability(ref string) (scan.Capability, bool) {
	return scan.Capability{}, true
}

func (s *dummyScanner) RunBackground() bool { return false }

type dummyPreflight struct{}

func (p *dummyPreflight) Check(ctx context.Context, ref preflight.SourceRef) (preflight.PreflightResult, error) {
	return preflight.PreflightResult{Outcome: preflight.PreflightOK}, nil
}
