package preflight

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

// HTTPPreflightProvider checks source availability using HTTP requests.
// Timeout is enforced via context.WithTimeout; http.Client.Timeout is left unset.
type HTTPPreflightProvider struct {
	client         *http.Client
	timeout        time.Duration
	outboundPolicy platformnet.OutboundPolicy
}

func NewHTTPPreflightProvider(client *http.Client, timeout time.Duration, outboundPolicy platformnet.OutboundPolicy) *HTTPPreflightProvider {
	if client == nil {
		client = &http.Client{}
	} else {
		// Shallow copy to avoid mutating the passed client
		c := *client
		client = &c
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPPreflightProvider{
		client:         client,
		timeout:        timeout,
		outboundPolicy: cloneOutboundPolicy(outboundPolicy),
	}
}

func (p *HTTPPreflightProvider) Check(ctx context.Context, src SourceRef) (PreflightResult, error) {
	if strings.TrimSpace(src.URL) == "" {
		return PreflightResult{Outcome: PreflightInternal}, fmt.Errorf("preflight url empty")
	}
	if p == nil || p.client == nil {
		return PreflightResult{Outcome: PreflightInternal}, fmt.Errorf("preflight client not configured")
	}

	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	validatedURL, err := platformnet.ValidateOutboundURL(ctx, src.URL, p.outboundPolicy)
	if err != nil {
		return PreflightResult{Outcome: PreflightInternal}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validatedURL, nil)
	if err != nil {
		return PreflightResult{Outcome: PreflightInternal}, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return PreflightResult{Outcome: PreflightTimeout}, nil
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return PreflightResult{Outcome: PreflightTimeout}, nil
		}
		return PreflightResult{Outcome: PreflightUnreachable}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	result := PreflightResult{
		HTTPStatus: resp.StatusCode,
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		result.Outcome = PreflightOK
	case resp.StatusCode == http.StatusUnauthorized:
		result.Outcome = PreflightUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		result.Outcome = PreflightForbidden
	case resp.StatusCode == http.StatusNotFound:
		result.Outcome = PreflightNotFound
	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		result.Outcome = PreflightBadGateway
	default:
		result.Outcome = PreflightInternal
	}
	return result, nil
}

func cloneOutboundPolicy(policy platformnet.OutboundPolicy) platformnet.OutboundPolicy {
	return platformnet.OutboundPolicy{
		Enabled: policy.Enabled,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   append([]string(nil), policy.Allow.Hosts...),
			CIDRs:   append([]string(nil), policy.Allow.CIDRs...),
			Ports:   append([]int(nil), policy.Allow.Ports...),
			Schemes: append([]string(nil), policy.Allow.Schemes...),
		},
	}
}
