package amazon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/receipts"
)

func TestVerifierVerifyPurchasedReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version/1.0/verifyReceiptId/developer/test-shared-secret/user/amzn-user-1/receiptId/receipt-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"receiptId":       "receipt-1",
			"productId":       "xg2g.unlock.firetv",
			"purchaseDate":    time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC).UnixMilli(),
			"cancelDate":      nil,
			"testTransaction": true,
		})
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	result, err := verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-1",
		UserID:        "amzn-user-1",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.State != receipts.PurchaseStatePurchased {
		t.Fatalf("expected purchased state, got %s", result.State)
	}
	if !result.TestPurchase {
		t.Fatal("expected test purchase flag")
	}
	if result.PurchaseTime == nil {
		t.Fatal("expected purchase time")
	}
}

func TestVerifierVerifyCancelledReceiptMapsToCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"receiptId":    "receipt-2",
			"productId":    "xg2g.unlock.firetv",
			"purchaseDate": time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC).UnixMilli(),
			"cancelDate":   time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC).UnixMilli(),
		})
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	result, err := verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-2",
		UserID:        "amzn-user-1",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.State != receipts.PurchaseStateCancelled {
		t.Fatalf("expected cancelled state, got %s", result.State)
	}
}

func TestVerifierVerifyGoneReceiptMapsToRevoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	result, err := verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-gone",
		UserID:        "amzn-user-1",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.State != receipts.PurchaseStateRevoked {
		t.Fatalf("expected revoked state, got %s", result.State)
	}
}

func TestVerifierVerifyRejectsProductMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"receiptId": "receipt-3",
			"productId": "other.product",
		})
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	_, err = verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-3",
		UserID:        "amzn-user-1",
	})
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func TestVerifierVerifyRejectsEmptySuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	_, err = verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-empty",
		UserID:        "amzn-user-1",
	})
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func TestVerifierVerifyRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	secretFile := writeSharedSecret(t, "test-shared-secret")
	verifier, err := NewVerifier(Config{
		SharedSecretFile: secretFile,
		BaseURL:          server.URL,
		HTTPClient:       &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	_, err = verifier.Verify(context.Background(), receipts.VerifyRequest{
		Provider:      receipts.ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "receipt-plain",
		UserID:        "amzn-user-1",
	})
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func writeSharedSecret(t *testing.T, secret string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "amazon-rvs.secret")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatalf("write shared secret: %v", err)
	}
	return path
}
