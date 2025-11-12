// SPDX-License-Identifier: MIT

package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRateLimiterGlobal(t *testing.T) {
	config := Config{
		GlobalRate:      10,
		GlobalBurst:     20,
		PerIPRate:       100,
		PerIPBurst:      200,
		ModeRates:       map[string]rate.Limit{"standard": 100},
		ModeBurst:       map[string]int{"standard": 200},
		CleanupInterval: 1 * time.Minute,
	}
	limiter := New(config)

	// First 20 should pass (burst)
	allowed := 0
	for i := 0; i < 25; i++ {
		if limiter.Allow("192.168.1.1", "standard") {
			allowed++
		}
	}

	// Should be around 20 (burst size)
	if allowed < 19 || allowed > 21 {
		t.Errorf("expected ~20 requests to pass with burst=20, got %d", allowed)
	}
}

func TestRateLimiterPerMode(t *testing.T) {
	config := Config{
		GlobalRate:  100,
		GlobalBurst: 200,
		PerIPRate:   100,
		PerIPBurst:  200,
		ModeRates: map[string]rate.Limit{
			"gpu": 5,
		},
		ModeBurst: map[string]int{
			"gpu": 10,
		},
		CleanupInterval: 1 * time.Minute,
	}
	limiter := New(config)

	// GPU mode has 5 req/s limit with burst 10
	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow("192.168.1.2", "gpu") {
			allowed++
		}
	}

	// Should be around 10 (burst size for GPU mode)
	if allowed < 9 || allowed > 11 {
		t.Errorf("expected ~10 GPU requests to pass with burst=10, got %d", allowed)
	}
}

func TestRateLimiterPerIP(t *testing.T) {
	config := Config{
		GlobalRate:      100,
		GlobalBurst:     200,
		PerIPRate:       5,
		PerIPBurst:      10,
		ModeRates:       map[string]rate.Limit{"standard": 100},
		ModeBurst:       map[string]int{"standard": 200},
		CleanupInterval: 1 * time.Minute,
	}
	limiter := New(config)

	// Each IP gets 5 req/s with burst 10
	ip := "192.168.1.3"
	allowed := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow(ip, "standard") {
			allowed++
		}
	}

	// Should be around 10 (burst size for per-IP)
	if allowed < 9 || allowed > 11 {
		t.Errorf("expected ~10 per-IP requests to pass with burst=10, got %d", allowed)
	}

	// Different IP should have its own bucket
	allowed2 := 0
	for i := 0; i < 20; i++ {
		if limiter.Allow("192.168.1.4", "standard") {
			allowed2++
		}
	}

	if allowed2 < 9 || allowed2 > 11 {
		t.Errorf("expected ~10 requests for second IP, got %d", allowed2)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		want       string
	}{
		{
			name:       "X-Forwarded-For single IP",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1"},
			remoteAddr: "192.168.1.1:12345",
			want:       "203.0.113.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 192.168.1.1, 10.0.0.1"},
			remoteAddr: "127.0.0.1:12345",
			want:       "203.0.113.1",
		},
		{
			name:       "X-Real-IP",
			headers:    map[string]string{"X-Real-IP": "203.0.113.2"},
			remoteAddr: "192.168.1.1:12345",
			want:       "203.0.113.2",
		},
		{
			name:       "Fallback to RemoteAddr",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.100:54321",
			want:       "192.168.1.100",
		},
		{
			name:       "X-Forwarded-For with spaces",
			headers:    map[string]string{"X-Forwarded-For": "  203.0.113.5  "},
			remoteAddr: "192.168.1.1:12345",
			want:       "203.0.113.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tt.remoteAddr

			got := GetClientIP(req)
			if got != tt.want {
				t.Errorf("GetClientIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	config := Config{
		GlobalRate:      100,
		GlobalBurst:     200,
		PerIPRate:       10,
		PerIPBurst:      20,
		ModeRates:       map[string]rate.Limit{"standard": 100},
		ModeBurst:       map[string]int{"standard": 200},
		CleanupInterval: 100 * time.Millisecond,
	}
	limiter := New(config)

	// Create limiters for multiple IPs
	for i := 0; i < 10; i++ {
		ip := "192.168.1." + string(rune(100+i))
		limiter.Allow(ip, "standard")
	}

	// Check that limiters were created
	limiter.mu.RLock()
	countBefore := len(limiter.perIP)
	limiter.mu.RUnlock()

	if countBefore != 10 {
		t.Errorf("expected 10 IP limiters, got %d", countBefore)
	}

	// Wait for cleanup interval to pass
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup by making a request
	// This should: 1) cleanup old limiters, 2) create new one for this IP
	limiter.Allow("192.168.1.200", "standard")

	// Check that old limiters were cleaned up and new one was created
	limiter.mu.RLock()
	countAfter := len(limiter.perIP)
	limiter.mu.RUnlock()

	// After cleanup and new request, should have exactly 1 limiter (the new one)
	if countAfter != 1 {
		t.Errorf("expected 1 IP limiter after cleanup (new request), got %d", countAfter)
	}
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	config := DefaultConfig()
	limiter := New(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("192.168.1.1", "standard")
	}
}

func BenchmarkGetClientIP(b *testing.B) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 192.168.1.1")
	req.RemoteAddr = "192.168.1.100:54321"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetClientIP(req)
	}
}
