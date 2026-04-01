package amazon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/receipts"
)

const (
	defaultBaseURL        = "https://appstore-sdk.amazon.com"
	defaultSandboxBaseURL = "https://appstore-sdk.amazon.com/sandbox"
)

type Config struct {
	SharedSecretFile string
	UseSandbox       bool
	BaseURL          string
	HTTPClient       *http.Client
}

type Verifier struct {
	sharedSecret string
	baseURL      string
	httpClient   *http.Client
}

type receiptResponse struct {
	ReceiptID       string `json:"receiptId"`
	ProductID       string `json:"productId"`
	PurchaseDateMS  *int64 `json:"purchaseDate"`
	CancelDateMS    *int64 `json:"cancelDate"`
	TestTransaction bool   `json:"testTransaction"`
}

func NewVerifier(cfg Config) (*Verifier, error) {
	sharedSecretFile := strings.TrimSpace(cfg.SharedSecretFile)
	if sharedSecretFile == "" {
		return nil, fmt.Errorf("amazon shared secret file must not be empty")
	}
	data, err := os.ReadFile(sharedSecretFile)
	if err != nil {
		return nil, fmt.Errorf("read amazon shared secret file: %w", err)
	}
	sharedSecret := strings.TrimSpace(string(data))
	if sharedSecret == "" {
		return nil, fmt.Errorf("amazon shared secret file must not be empty")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		if cfg.UseSandbox {
			baseURL = defaultSandboxBaseURL
		} else {
			baseURL = defaultBaseURL
		}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// Keep the default client transport plain here. Amazon RVS embeds the shared
		// secret in the request path, so any injected client/transport must not log
		// full request URLs without first redacting that segment.
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Verifier{
		sharedSecret: sharedSecret,
		baseURL:      baseURL,
		httpClient:   httpClient,
	}, nil
}

func (v *Verifier) Provider() string {
	return receipts.ProviderAmazonAppstore
}

func (v *Verifier) Verify(ctx context.Context, req receipts.VerifyRequest) (receipts.VerifyResult, error) {
	productID := strings.TrimSpace(req.ProductID)
	if productID == "" {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: "productId must not be empty"}
	}
	purchaseToken := strings.TrimSpace(req.PurchaseToken)
	if purchaseToken == "" {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: "purchaseToken must not be empty"}
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: "userId must not be empty for amazon_appstore receipt verification"}
	}

	endpoint := fmt.Sprintf(
		"%s/version/1.0/verifyReceiptId/developer/%s/user/%s/receiptId/%s",
		v.baseURL,
		url.PathEscape(v.sharedSecret),
		url.PathEscape(userID),
		url.PathEscape(purchaseToken),
	)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "create Amazon Appstore verification request", Err: err}
	}
	reqHTTP.Header.Set("Accept", "application/json")

	resp, err := v.httpClient.Do(reqHTTP)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUnavailable, Message: "contact Amazon Appstore verification API", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "read Amazon Appstore verification response", Err: err}
	}

	if resp.StatusCode == http.StatusGone {
		return receipts.VerifyResult{
			Provider:  receipts.ProviderAmazonAppstore,
			ProductID: productID,
			Source:    entitlements.SourceAmazonAppstore,
			State:     receipts.PurchaseStateRevoked,
		}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return receipts.VerifyResult{}, classifyAmazonAPIError(resp.StatusCode, body)
	}

	var payload receiptResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "decode Amazon Appstore verification response", Err: err}
	}
	if strings.TrimSpace(payload.ProductID) == "" {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "Amazon Appstore verification response did not include productId"}
	}
	if strings.TrimSpace(payload.ProductID) != productID {
		return receipts.VerifyResult{}, &receipts.Error{
			Kind:    receipts.ErrorKindInvalidInput,
			Message: fmt.Sprintf("Amazon Appstore receipt does not contain productId %q", productID),
		}
	}

	state := receipts.PurchaseStatePurchased
	if payload.CancelDateMS != nil {
		state = receipts.PurchaseStateCancelled
	}

	return receipts.VerifyResult{
		Provider:     receipts.ProviderAmazonAppstore,
		ProductID:    productID,
		Source:       entitlements.SourceAmazonAppstore,
		State:        state,
		PurchaseTime: parseOptionalUnixMillis(payload.PurchaseDateMS),
		TestPurchase: payload.TestTransaction,
	}, nil
}

func parseOptionalUnixMillis(raw *int64) *time.Time {
	if raw == nil {
		return nil
	}
	ts := time.UnixMilli(*raw).UTC()
	return &ts
}

func classifyAmazonAPIError(statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}

	switch statusCode {
	case http.StatusBadRequest:
		return &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: message}
	case http.StatusTooManyRequests, 496:
		// 496: Amazon-specific non-standard status for shared-secret/app identity
		// mismatch or temporarily unavailable receipt verification credentials.
		return &receipts.Error{Kind: receipts.ErrorKindUnavailable, Message: message}
	case 497:
		// 497: Amazon-specific non-standard status for invalid or unknown userId.
		return &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: message}
	default:
		return &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: message}
	}
}
