package httpx

import (
	"net"
	"net/http"
	"time"
)

const (
	defaultClientTimeout         = 5 * time.Second
	defaultDialTimeout           = 3 * time.Second
	defaultResponseHeaderTimeout = 3 * time.Second
	defaultIdleConnTimeout       = 30 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 16
	defaultMaxIdleConnsPerHost   = 4
)

// NewClient returns a hardened HTTP client for runtime and ops probes.
func NewClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultClientTimeout
	}

	dialTimeout := timeout
	if dialTimeout > defaultDialTimeout {
		dialTimeout = defaultDialTimeout
	}

	responseHeaderTimeout := timeout
	if responseHeaderTimeout > defaultResponseHeaderTimeout {
		responseHeaderTimeout = defaultResponseHeaderTimeout
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          defaultMaxIdleConns,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			ExpectContinueTimeout: defaultExpectContinueTimeout,
		},
	}
}
