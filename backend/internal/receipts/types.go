package receipts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ProviderGooglePlay = "google_play"

type PurchaseState string

const (
	PurchaseStatePurchased PurchaseState = "purchased"
	PurchaseStatePending   PurchaseState = "pending"
	PurchaseStateCancelled PurchaseState = "cancelled"
	PurchaseStateRevoked   PurchaseState = "revoked"
)

type ApplyAction string

const (
	ApplyActionGranted ApplyAction = "granted"
	ApplyActionRevoked ApplyAction = "revoked"
	ApplyActionNone    ApplyAction = "none"
)

type VerifyRequest struct {
	Provider      string
	ProductID     string
	PurchaseToken string
}

type VerifyResult struct {
	Provider            string
	ProductID           string
	Source              string
	State               PurchaseState
	OrderID             string
	PurchaseTime        *time.Time
	TestPurchase        bool
	ObfuscatedAccountID string
	ObfuscatedProfileID string
}

type ApplyRequest struct {
	PrincipalID   string
	Provider      string
	ProductID     string
	PurchaseToken string
}

type ApplyResult struct {
	Verification VerifyResult
	MappedScopes []string
	Action       ApplyAction
}

type Verifier interface {
	Provider() string
	Verify(ctx context.Context, req VerifyRequest) (VerifyResult, error)
}

type ErrorKind string

const (
	ErrorKindInvalidInput ErrorKind = "invalid_input"
	ErrorKindUnavailable  ErrorKind = "unavailable"
	ErrorKindUpstream     ErrorKind = "upstream"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "receipt verification failed"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newError(kind ErrorKind, message string, err error) *Error {
	return &Error{
		Kind:    kind,
		Message: strings.TrimSpace(message),
		Err:     err,
	}
}

var (
	ErrUnknownProductMapping = errors.New("unknown monetization product mapping")
	ErrVerifierUnavailable   = errors.New("receipt verifier unavailable")
)

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func normalizeProductID(productID string) string {
	return strings.TrimSpace(productID)
}

func normalizePrincipalID(principalID string) string {
	return strings.TrimSpace(principalID)
}

func cloneTime(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	cloned := ts.UTC()
	return &cloned
}

func wrapUnexpectedState(state PurchaseState) error {
	return fmt.Errorf("unexpected purchase state %q", state)
}
