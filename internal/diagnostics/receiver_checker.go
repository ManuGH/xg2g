package diagnostics

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ReceiverChecker implements HealthChecker for the Receiver subsystem.
type ReceiverChecker struct {
	receiverURL string // e.g., "http://10.10.55.14:80"
	httpClient  *http.Client
}

// NewReceiverChecker creates a new Receiver health checker.
func NewReceiverChecker(receiverURL string) *ReceiverChecker {
	return &ReceiverChecker{
		receiverURL: receiverURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // Per ADR-SRE-002 P0-D: 2s ok, 5s degraded cutoff
		},
	}
}

// Check probes the receiver's /api/statusinfo endpoint.
// Per ADR-SRE-002:
//   - ok: HTTP 200 within 2s
//   - degraded: Slow response (2-5s)
//   - unavailable: Timeout, connection refused, or HTTP 5xx
func (r *ReceiverChecker) Check(ctx context.Context) SubsystemHealth {
	health := SubsystemHealth{
		Subsystem:   SubsystemReceiver,
		MeasuredAt:  time.Now(),
		Source:      SourceProbe,
		Criticality: Critical,
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "GET", r.receiverURL+"/api/statusinfo", nil)
	if err != nil {
		health.Status = Unavailable
		health.ErrorCode = ErrReceiverUnreachable
		health.ErrorMessage = ErrorMessages[ErrReceiverUnreachable]
		return health
	}

	resp, err := r.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		health.Status = Unavailable
		health.ErrorCode = ErrReceiverUnreachable
		health.ErrorMessage = ErrorMessages[ErrReceiverUnreachable]
		return health
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		health.Status = Unavailable
		health.ErrorCode = ErrReceiverHTTPError
		health.ErrorMessage = fmt.Sprintf("Receiver returned error: %d", resp.StatusCode)
		return health
	}

	if resp.StatusCode != 200 {
		health.Status = Degraded
		health.ErrorCode = ErrReceiverHTTPError
		health.ErrorMessage = fmt.Sprintf("Receiver returned: %d", resp.StatusCode)
		health.Details = ReceiverDetails{
			ReceiverID:     "primary", // TODO: Make configurable
			ResponseTimeMS: elapsed.Milliseconds(),
		}
		return health
	}

	// Success: determine ok vs degraded based on response time
	now := time.Now()
	health.LastOK = &now

	if elapsed > 2*time.Second {
		health.Status = Degraded
		health.ErrorCode = ErrReceiverTimeout
		health.ErrorMessage = ErrorMessages[ErrReceiverTimeout]
	} else {
		health.Status = OK
	}

	health.Details = ReceiverDetails{
		ReceiverID:     "primary",
		ResponseTimeMS: elapsed.Milliseconds(),
		// Version parsing would require reading response body
	}

	return health
}
