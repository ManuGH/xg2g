//nolint:noctx // Benchmarks don't require context in HTTP requests
package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

// BenchmarkProxyRequest benchmarks basic proxy request handling
func BenchmarkProxyRequest(b *testing.B) {
	// Create backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create proxy
	proxy, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     backend.URL,
		Logger:        zerolog.New(io.Discard),
		AuthAnonymous: true,
	})
	if err != nil {
		b.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := http.Get(proxyServer.URL + "/test")
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
}

// BenchmarkProxyHeadRequest benchmarks HEAD request handling
func BenchmarkProxyHeadRequest(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     backend.URL,
		Logger:        zerolog.New(io.Discard),
		AuthAnonymous: true,
	})
	if err != nil {
		b.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	b.ResetTimer()
	b.ReportAllocs()

	client := &http.Client{}
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodHead, proxyServer.URL+"/stream", nil)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		_ = resp.Body.Close()
	}
}

// BenchmarkProxyLargeResponse benchmarks proxying large responses
func BenchmarkProxyLargeResponse(b *testing.B) {
	// 1MB response
	data := make([]byte, 1024*1024)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     backend.URL,
		Logger:        zerolog.New(io.Discard),
		AuthAnonymous: true,
	})
	if err != nil {
		b.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		resp, err := http.Get(proxyServer.URL + "/large")
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// BenchmarkProxyConcurrent benchmarks concurrent request handling
func BenchmarkProxyConcurrent(b *testing.B) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr:    ":0",
		TargetURL:     backend.URL,
		Logger:        zerolog.New(io.Discard),
		AuthAnonymous: true,
	})
	if err != nil {
		b.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := http.Get(proxyServer.URL + "/test")
			if err != nil {
				b.Fatalf("Request failed: %v", err)
			}
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
	})
}

// BenchmarkProxyNew benchmarks proxy creation
func BenchmarkProxyNew(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = New(Config{
			ListenAddr:    ":0",
			TargetURL:     "http://example.com:8080",
			Logger:        zerolog.New(io.Discard),
			AuthAnonymous: true,
		})
	}
}
