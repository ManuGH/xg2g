package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/receipts"
)

func TestVerifierVerifyPurchasedReceipt(t *testing.T) {
	var seenAuthorization string
	publisherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/androidpublisher/v3/applications/io.github.manugh.xg2g.android/purchases/productsv2/tokens/purchase-token-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"productLineItem": []map[string]any{
				{
					"productId": "xg2g.unlock",
					"productOfferDetails": map[string]any{
						"refundableQuantity": 1,
					},
				},
			},
			"purchaseStateContext": map[string]any{
				"purchaseState": "PURCHASED",
			},
			"orderId":                "order-123",
			"purchaseCompletionTime": "2026-04-01T10:00:00Z",
		})
	}))
	defer publisherServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("unexpected grant_type: %s", got)
		}
		if strings.TrimSpace(r.Form.Get("assertion")) == "" {
			t.Fatal("expected assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	credentialsFile := writeServiceAccountCredentials(t, tokenServer.URL)
	verifier, err := NewVerifier(Config{
		PackageName:                   "io.github.manugh.xg2g.android",
		ServiceAccountCredentialsFile: credentialsFile,
		TokenURL:                      tokenServer.URL,
		PublisherBaseURL:              publisherServer.URL,
		HTTPClient:                    &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	result, err := verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-1",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.State != receipts.PurchaseStatePurchased {
		t.Fatalf("expected purchased state, got %s", result.State)
	}
	if seenAuthorization != "Bearer test-access-token" {
		t.Fatalf("expected bearer access token, got %q", seenAuthorization)
	}
	if result.OrderID != "order-123" {
		t.Fatalf("unexpected order id: %s", result.OrderID)
	}
}

func TestVerifierVerifyRefundedReceiptMapsToRevoked(t *testing.T) {
	publisherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"productLineItem": []map[string]any{
				{
					"productId": "xg2g.unlock",
					"productOfferDetails": map[string]any{
						"refundableQuantity": 0,
					},
				},
			},
			"purchaseStateContext": map[string]any{
				"purchaseState": "PURCHASED",
			},
		})
	}))
	defer publisherServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	credentialsFile := writeServiceAccountCredentials(t, tokenServer.URL)
	verifier, err := NewVerifier(Config{
		PackageName:                   "io.github.manugh.xg2g.android",
		ServiceAccountCredentialsFile: credentialsFile,
		TokenURL:                      tokenServer.URL,
		PublisherBaseURL:              publisherServer.URL,
		HTTPClient:                    &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	result, err := verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-2",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.State != receipts.PurchaseStateRevoked {
		t.Fatalf("expected revoked state, got %s", result.State)
	}
}

func TestVerifierVerifyRejectsProductMismatch(t *testing.T) {
	publisherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"productLineItem": []map[string]any{
				{
					"productId": "other.product",
					"productOfferDetails": map[string]any{
						"refundableQuantity": 1,
					},
				},
			},
			"purchaseStateContext": map[string]any{
				"purchaseState": "PURCHASED",
			},
		})
	}))
	defer publisherServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	credentialsFile := writeServiceAccountCredentials(t, tokenServer.URL)
	verifier, err := NewVerifier(Config{
		PackageName:                   "io.github.manugh.xg2g.android",
		ServiceAccountCredentialsFile: credentialsFile,
		TokenURL:                      tokenServer.URL,
		PublisherBaseURL:              publisherServer.URL,
		HTTPClient:                    &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	_, err = verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-3",
	})
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func TestMapPurchaseStateRecognizesPendingAndCancelled(t *testing.T) {
	pendingState, err := mapPurchaseState("PENDING", 1)
	if err != nil {
		t.Fatalf("pending state: %v", err)
	}
	if pendingState != receipts.PurchaseStatePending {
		t.Fatalf("expected pending state, got %s", pendingState)
	}

	cancelledState, err := mapPurchaseState("CANCELLED", 1)
	if err != nil {
		t.Fatalf("cancelled state: %v", err)
	}
	if cancelledState != receipts.PurchaseStateCancelled {
		t.Fatalf("expected cancelled state, got %s", cancelledState)
	}
}

func writeServiceAccountCredentials(t *testing.T, tokenURL string) string {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	credentials := map[string]string{
		"client_email": "service-account@example.iam.gserviceaccount.com",
		"private_key":  string(privateKeyPEM),
		"token_uri":    tokenURL,
	}
	data, err := json.Marshal(credentials)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}

	path := filepath.Join(t.TempDir(), "google-play-service-account.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return path
}
