package google

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/receipts"
)

const (
	defaultTokenURL         = "https://oauth2.googleapis.com/token"
	defaultPublisherBaseURL = "https://androidpublisher.googleapis.com"
	androidPublisherScope   = "https://www.googleapis.com/auth/androidpublisher"
)

type Config struct {
	PackageName                   string
	ServiceAccountCredentialsFile string
	TokenURL                      string
	PublisherBaseURL              string
	HTTPClient                    *http.Client
	Now                           func() time.Time
}

type Verifier struct {
	packageName      string
	tokenURL         string
	publisherBaseURL string
	httpClient       *http.Client
	now              func() time.Time
	creds            serviceAccountCredentials
	privateKey       *rsa.PrivateKey

	mu          sync.Mutex
	accessToken cachedAccessToken
}

type cachedAccessToken struct {
	value     string
	expiresAt time.Time
}

type serviceAccountCredentials struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type productPurchaseV2Response struct {
	ProductLineItem      []productLineItem `json:"productLineItem"`
	PurchaseStateContext struct {
		PurchaseState string `json:"purchaseState"`
	} `json:"purchaseStateContext"`
	TestPurchaseContext *struct {
		FopType string `json:"fopType"`
	} `json:"testPurchaseContext,omitempty"`
	OrderID                     string `json:"orderId"`
	ObfuscatedExternalAccountID string `json:"obfuscatedExternalAccountId"`
	ObfuscatedExternalProfileID string `json:"obfuscatedExternalProfileId"`
	PurchaseCompletionTime      string `json:"purchaseCompletionTime"`
}

type productLineItem struct {
	ProductID           string `json:"productId"`
	ProductOfferDetails struct {
		RefundableQuantity int `json:"refundableQuantity"`
	} `json:"productOfferDetails"`
}

type googleAPIErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Errors  []struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"error"`
}

func NewVerifier(cfg Config) (*Verifier, error) {
	packageName := strings.TrimSpace(cfg.PackageName)
	if packageName == "" {
		return nil, fmt.Errorf("google play package name must not be empty")
	}
	credentialsFile := strings.TrimSpace(cfg.ServiceAccountCredentialsFile)
	if credentialsFile == "" {
		return nil, fmt.Errorf("google play service account credentials file must not be empty")
	}

	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read google play service account credentials: %w", err)
	}

	var creds serviceAccountCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse google play service account credentials: %w", err)
	}
	if strings.TrimSpace(creds.ClientEmail) == "" || strings.TrimSpace(creds.PrivateKey) == "" {
		return nil, fmt.Errorf("google play service account credentials must include client_email and private_key")
	}

	privateKey, err := parseRSAPrivateKey(creds.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse google play service account private key: %w", err)
	}

	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if tokenURL == "" {
		tokenURL = strings.TrimSpace(creds.TokenURI)
	}
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}

	publisherBaseURL := strings.TrimRight(strings.TrimSpace(cfg.PublisherBaseURL), "/")
	if publisherBaseURL == "" {
		publisherBaseURL = defaultPublisherBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return &Verifier{
		packageName:      packageName,
		tokenURL:         tokenURL,
		publisherBaseURL: publisherBaseURL,
		httpClient:       httpClient,
		now:              now,
		creds:            creds,
		privateKey:       privateKey,
	}, nil
}

func (v *Verifier) Provider() string {
	return receipts.ProviderGooglePlay
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

	accessToken, err := v.getAccessToken(ctx)
	if err != nil {
		return receipts.VerifyResult{}, err
	}

	endpoint := fmt.Sprintf(
		"%s/androidpublisher/v3/applications/%s/purchases/productsv2/tokens/%s",
		v.publisherBaseURL,
		url.PathEscape(v.packageName),
		url.PathEscape(purchaseToken),
	)
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "create Google Play verification request", Err: err}
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+accessToken)
	reqHTTP.Header.Set("Accept", "application/json")

	resp, err := v.httpClient.Do(reqHTTP)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUnavailable, Message: "contact Google Play verification API", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "read Google Play verification response", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return receipts.VerifyResult{}, classifyGoogleAPIError(resp.StatusCode, body)
	}

	var payload productPurchaseV2Response
	if err := json.Unmarshal(body, &payload); err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "decode Google Play verification response", Err: err}
	}

	matchingLineItem, ok := findMatchingLineItem(payload.ProductLineItem, productID)
	if !ok {
		return receipts.VerifyResult{}, &receipts.Error{
			Kind:    receipts.ErrorKindInvalidInput,
			Message: fmt.Sprintf("Google Play purchase token does not contain productId %q", productID),
		}
	}

	state, err := mapPurchaseState(payload.PurchaseStateContext.PurchaseState, matchingLineItem.ProductOfferDetails.RefundableQuantity)
	if err != nil {
		return receipts.VerifyResult{}, &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "unsupported Google Play purchase state", Err: err}
	}

	return receipts.VerifyResult{
		Provider:            receipts.ProviderGooglePlay,
		ProductID:           productID,
		Source:              entitlements.SourceGooglePlay,
		State:               state,
		OrderID:             strings.TrimSpace(payload.OrderID),
		PurchaseTime:        parseOptionalTimestamp(payload.PurchaseCompletionTime),
		TestPurchase:        payload.TestPurchaseContext != nil,
		ObfuscatedAccountID: strings.TrimSpace(payload.ObfuscatedExternalAccountID),
		ObfuscatedProfileID: strings.TrimSpace(payload.ObfuscatedExternalProfileID),
	}, nil
}

func (v *Verifier) getAccessToken(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	if strings.TrimSpace(v.accessToken.value) != "" && now.Before(v.accessToken.expiresAt.Add(-1*time.Minute)) {
		return v.accessToken.value, nil
	}

	assertion, err := v.buildJWTAssertion(now)
	if err != nil {
		return "", &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "build Google OAuth assertion", Err: err}
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "create Google OAuth token request", Err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", &receipts.Error{Kind: receipts.ErrorKindUnavailable, Message: "request Google OAuth access token", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "read Google OAuth token response", Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &receipts.Error{
			Kind:    receipts.ErrorKindUpstream,
			Message: fmt.Sprintf("Google OAuth token request failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "decode Google OAuth token response", Err: err}
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: "Google OAuth token response did not include an access token"}
	}

	expiresAt := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	v.accessToken = cachedAccessToken{
		value:     tokenResp.AccessToken,
		expiresAt: expiresAt,
	}
	return tokenResp.AccessToken, nil
}

func (v *Verifier) buildJWTAssertion(now time.Time) (string, error) {
	headerJSON, err := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(map[string]any{
		"iss":   v.creds.ClientEmail,
		"scope": androidPublisherScope,
		"aud":   v.tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}

	unsignedToken := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	hashed := sha256.Sum256([]byte(unsignedToken))
	signature, err := rsa.SignPKCS1v15(rand.Reader, v.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	return unsignedToken + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseRSAPrivateKey(pemEncoded string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemEncoded))
	if block == nil {
		return nil, fmt.Errorf("PEM decode failed")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func findMatchingLineItem(lineItems []productLineItem, productID string) (productLineItem, bool) {
	for _, lineItem := range lineItems {
		if strings.TrimSpace(lineItem.ProductID) == productID {
			return lineItem, true
		}
	}
	return productLineItem{}, false
}

func mapPurchaseState(rawState string, refundableQuantity int) (receipts.PurchaseState, error) {
	switch strings.TrimSpace(rawState) {
	case "PURCHASED":
		if refundableQuantity == 0 {
			return receipts.PurchaseStateRevoked, nil
		}
		return receipts.PurchaseStatePurchased, nil
	case "CANCELLED":
		return receipts.PurchaseStateCancelled, nil
	case "PENDING":
		return receipts.PurchaseStatePending, nil
	default:
		return "", fmt.Errorf("unknown Google Play purchase state %q", rawState)
	}
}

func parseOptionalTimestamp(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	ts := parsed.UTC()
	return &ts
}

func classifyGoogleAPIError(statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}

	var apiErr googleAPIErrorEnvelope
	if err := json.Unmarshal(body, &apiErr); err == nil {
		if strings.TrimSpace(apiErr.Error.Message) != "" {
			message = strings.TrimSpace(apiErr.Error.Message)
		}
		for _, item := range apiErr.Error.Errors {
			switch strings.TrimSpace(item.Reason) {
			case "productNotOwnedByUser":
				return &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: "Google Play purchase is no longer owned by the user"}
			}
		}
	}

	switch statusCode {
	case http.StatusBadRequest, http.StatusNotFound:
		return &receipts.Error{Kind: receipts.ErrorKindInvalidInput, Message: message}
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return &receipts.Error{Kind: receipts.ErrorKindUnavailable, Message: message}
	default:
		return &receipts.Error{Kind: receipts.ErrorKindUpstream, Message: message}
	}
}
