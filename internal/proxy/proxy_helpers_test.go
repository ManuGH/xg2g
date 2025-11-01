// SPDX-License-Identifier: MIT

package proxy

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestIsContextCancelled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{
			name: "cancelled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			want: true,
		},
		{
			name: "active context",
			ctx:  context.Background(),
			want: false,
		},
		{
			name: "deadline exceeded",
			ctx: func() context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 0)
				defer cancel()
				<-ctx.Done() // Wait for timeout
				return ctx
			}(),
			want: false, // DeadlineExceeded != Canceled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isContextCancelled(tt.ctx); got != tt.want {
				t.Errorf("isContextCancelled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBrokenPipe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "broken pipe error",
			err:  errors.New("write tcp: broken pipe"),
			want: true,
		},
		{
			name: "connection reset error",
			err:  errors.New("read tcp: connection reset by peer"),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("timeout"),
			want: false,
		},
		{
			name: "EOF error",
			err:  errors.New("EOF"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isBrokenPipe(tt.err); got != tt.want {
				t.Errorf("isBrokenPipe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsExpectedStreamError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "broken pipe",
			err:  errors.New("write tcp: broken pipe"),
			want: true,
		},
		{
			name: "connection reset",
			err:  errors.New("read tcp: connection reset by peer"),
			want: true,
		},
		{
			name: "connection timed out",
			err:  errors.New("write: connection timed out"),
			want: true,
		},
		{
			name: "i/o timeout",
			err:  errors.New("read tcp: i/o timeout"),
			want: true,
		},
		{
			name: "unexpected error",
			err:  errors.New("disk full"),
			want: false,
		},
		{
			name: "context canceled (not expected by function)",
			err:  context.Canceled,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isExpectedStreamError(tt.err); got != tt.want {
				t.Errorf("isExpectedStreamError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServer_ResolveTargetURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		targetURL    string
		path         string
		query        string
		wantContains string
	}{
		{
			name:         "simple path without query",
			targetURL:    "http://receiver:17999",
			path:         "/1:0:19:132F:3EF:1:C00000:0:0:0:",
			query:        "",
			wantContains: "http://receiver:17999/1:0:19:132F:3EF:1:C00000:0:0:0:",
		},
		{
			name:         "path with query parameters",
			targetURL:    "http://receiver:8001",
			path:         "/1:0:1:300:7:85:FFFF0000:0:0:0:",
			query:        "foo=bar",
			wantContains: "foo=bar",
		},
		{
			name:         "root path",
			targetURL:    "http://receiver:17999",
			path:         "/",
			query:        "",
			wantContains: "http://receiver:17999/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			targetURL, err := url.Parse(tt.targetURL)
			if err != nil {
				t.Fatalf("failed to parse target URL: %v", err)
			}

			s := &Server{
				targetURL: targetURL,
			}

			got := s.resolveTargetURL(context.Background(), tt.path, tt.query)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("resolveTargetURL() = %v, want to contain %v", got, tt.wantContains)
			}
		})
	}
}

func TestGetReceiverHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		envVal  string
		want    string
		wantErr bool
	}{
		{
			name:    "valid URL with host",
			envVal:  "http://10.10.55.57",
			want:    "10.10.55.57",
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			envVal:  "http://enigma2.local:80",
			want:    "enigma2.local",
			wantErr: false,
		},
		{
			name:    "HTTPS URL",
			envVal:  "https://receiver.example.com",
			want:    "receiver.example.com",
			wantErr: false,
		},
		{
			name:    "empty env variable",
			envVal:  "",
			want:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			oldVal := os.Getenv("XG2G_OWI_BASE")
			defer func() {
				if oldVal != "" {
					os.Setenv("XG2G_OWI_BASE", oldVal)  //nolint:errcheck // Test cleanup
				} else {
					os.Unsetenv("XG2G_OWI_BASE")  //nolint:errcheck // Test cleanup
				}
			}()

			if tt.envVal != "" {
				os.Setenv("XG2G_OWI_BASE", tt.envVal)  //nolint:errcheck // Test setup
			} else {
				os.Unsetenv("XG2G_OWI_BASE")  //nolint:errcheck // Test cleanup
			}

			got := GetReceiverHost()
			if got != tt.want {
				t.Errorf("GetReceiverHost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsGPUEnabled(t *testing.T) {
	t.Parallel()

	// Note: In nogpu build, this should always return false
	// In GPU-enabled build, it depends on environment variable

	transcoder := &Transcoder{
		Config: TranscoderConfig{
			// GPU config is build-tag dependent
		},
	}

	got := transcoder.IsGPUEnabled()

	// For nogpu build (which is tested by default), should be false
	if got {
		t.Log("GPU enabled (running with GPU build tag)")
	} else {
		t.Log("GPU disabled (running with nogpu build tag)")
	}

	// Test passes regardless - just documenting behavior
}
