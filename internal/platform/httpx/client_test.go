package httpx

import (
	"net/http"
	"testing"
	"time"
)

func TestNewClient_DefaultTimeoutAndTransport(t *testing.T) {
	client := NewClient(0)
	if client.Timeout != defaultClientTimeout {
		t.Fatalf("timeout = %v, want %v", client.Timeout, defaultClientTimeout)
	}
	if client.Transport == nil {
		t.Fatal("transport must not be nil")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.MaxIdleConns != defaultMaxIdleConns {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, defaultMaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != defaultMaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout != defaultIdleConnTimeout {
		t.Fatalf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, defaultIdleConnTimeout)
	}
}

func TestNewClient_CapsDialAndHeaderTimeouts(t *testing.T) {
	client := NewClient(10 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSHandshakeTimeout != defaultDialTimeout {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, defaultDialTimeout)
	}
	if transport.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}
}

func TestNewClient_UsesShortTimeoutAsProvided(t *testing.T) {
	want := 1500 * time.Millisecond
	client := NewClient(want)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if client.Timeout != want {
		t.Fatalf("timeout = %v, want %v", client.Timeout, want)
	}
	if transport.TLSHandshakeTimeout != want {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, want)
	}
	if transport.ResponseHeaderTimeout != want {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, want)
	}
}

