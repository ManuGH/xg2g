package v3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
)

// jwtTestSecret references the SSOT default; never duplicate the literal.
var jwtTestSecret = auth.DefaultDecisionSecret

// intentReqWithValidJWT builds a POST /intents request with a valid JWT
// that passes the security gate. Parameters are exposed so callers can
// test specific claim variations (mode/capHash mismatch etc.).
//
// Time: uses time.Now() with generous offsets (Nbf -30s, Exp +120s)
// to absorb CI latency and NTP skew.
func intentReqWithValidJWT(t *testing.T, svcRef, capHash, mode string) *http.Request {
	t.Helper()

	now := time.Now().Unix()
	normRef := normalize.ServiceRef(svcRef)

	claims := auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normRef,
		Jti:     "test-" + t.Name() + "-" + fmt.Sprintf("%d", now),
		Iat:     now,
		Nbf:     now - 30,
		Exp:     now + 120,
		Mode:    mode,
		CapHash: capHash,
	}

	tok, err := auth.GenerateHS256(jwtTestSecret, claims, "kid-v1")
	if err != nil {
		t.Fatalf("intentReqWithValidJWT: generate token: %v", err)
	}

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            svcRef,
		PlaybackDecisionToken: &tok,
		Params:                map[string]string{"mode": mode},
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// intentBodyWithValidJWT returns the JSON body bytes for an intent request
// with a valid JWT. This is used by tests that go through the full router
// (auth middleware + handler) and need to control the raw body.
func intentBodyWithValidJWT(t *testing.T, svcRef, capHash, mode, correlationID string) []byte {
	t.Helper()

	now := time.Now().Unix()
	normRef := normalize.ServiceRef(svcRef)

	claims := auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normRef,
		Jti:     "test-body-" + t.Name() + "-" + fmt.Sprintf("%d", now),
		Iat:     now,
		Nbf:     now - 30,
		Exp:     now + 120,
		Mode:    mode,
		CapHash: capHash,
	}

	tok, err := auth.GenerateHS256(jwtTestSecret, claims, "kid-v1")
	if err != nil {
		t.Fatalf("intentBodyWithValidJWT: generate token: %v", err)
	}

	body := map[string]any{
		"type":                  "stream.start",
		"serviceRef":            svcRef,
		"playbackDecisionToken": tok,
		"params":                map[string]string{"mode": mode},
	}
	if correlationID != "" {
		body["correlationId"] = correlationID
	}
	b, _ := json.Marshal(body)
	return b
}
