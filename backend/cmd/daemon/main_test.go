// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaskURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "http URL without credentials",
			rawURL: "http://example.com:8080",
			want:   "http://example.com:8080",
		},
		{
			name:   "https URL without credentials",
			rawURL: "https://example.com:443/path",
			want:   "https://example.com:443/path",
		},
		{
			name:   "URL with username and password",
			rawURL: "http://user:pass@example.com:8080",
			want:   "http://example.com:8080",
		},
		{
			name:   "URL with only username",
			rawURL: "http://user@example.com:8080/path",
			want:   "http://example.com:8080/path",
		},
		{
			name:   "URL with complex credentials",
			rawURL: "https://admin:secret123@192.168.1.100:8080/api",
			want:   "https://192.168.1.100:8080/api",
		},
		{
			name:   "URL with special characters in password",
			rawURL: "http://user:p@ss%20word@example.com",
			want:   "http://example.com",
		},
		{
			name:   "plain text (parsed as relative path)",
			rawURL: "not a url",
			want:   "not%20a%20url", // url.Parse encodes spaces but doesn't error
		},
		{
			name:   "empty URL",
			rawURL: "",
			want:   "", // Empty URLs parse successfully as relative URLs
		},
		{
			name:   "IPv6 address",
			rawURL: "http://[::1]:8080/path",
			want:   "http://[::1]:8080/path",
		},
		{
			name:   "URL with fragment",
			rawURL: "http://user:pass@example.com:8080/path#fragment",
			want:   "http://example.com:8080/path#fragment",
		},
		{
			name:   "URL with query parameters",
			rawURL: "http://user:pass@example.com:8080/path?key=value",
			want:   "http://example.com:8080/path?key=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskURL(tt.rawURL)
			if got != tt.want {
				t.Errorf("maskURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestMainUsageListsExecutableCommands(t *testing.T) {
	var out bytes.Buffer
	printMainUsage(&out)

	for _, command := range []string{"version", "status", "report"} {
		if !strings.Contains(out.String(), "  "+command) {
			t.Fatalf("main help does not list %q:\n%s", command, out.String())
		}
	}
}

func TestHandleImmediateMainCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantHandled bool
		wantCode    int
		wantStdout  string
		wantStderr  string
	}{
		{
			name:        "version command",
			args:        []string{"version"},
			wantHandled: true,
			wantCode:    0,
			wantStdout:  "commit:",
		},
		{
			name:        "version rejects arguments",
			args:        []string{"version", "extra"},
			wantHandled: true,
			wantCode:    2,
			wantStderr:  "accepts no arguments",
		},
		{
			name:        "unknown command fails closed",
			args:        []string{"statuz"},
			wantHandled: true,
			wantCode:    2,
			wantStderr:  "Unknown command.",
		},
		{
			name:        "known command continues to dispatcher",
			args:        []string{"status"},
			wantHandled: false,
		},
		{
			name:        "daemon flags continue to flag parser",
			args:        []string{"--config", "/etc/xg2g/config.yaml"},
			wantHandled: false,
		},
		{
			name:        "empty args continue to daemon",
			args:        nil,
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			handled, code := handleImmediateMainCommand(&stdout, &stderr, tt.args)
			if handled != tt.wantHandled || code != tt.wantCode {
				t.Fatalf("handleImmediateMainCommand(%q) = (%v, %d), want (%v, %d)",
					tt.args, handled, code, tt.wantHandled, tt.wantCode)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Fatalf("stdout missing %q: %q", tt.wantStdout, stdout.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr missing %q: %q", tt.wantStderr, stderr.String())
			}
		})
	}
}

func TestRejectDaemonPositionals(t *testing.T) {
	var stderr bytes.Buffer
	if code := rejectDaemonPositionals(&stderr, []string{"unexpected"}); code != 2 {
		t.Fatalf("rejectDaemonPositionals() exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "daemon mode accepts flags only") {
		t.Fatalf("missing positional-argument error: %q", stderr.String())
	}

	stderr.Reset()
	if code := rejectDaemonPositionals(&stderr, nil); code != 0 {
		t.Fatalf("rejectDaemonPositionals(nil) exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("rejectDaemonPositionals(nil) wrote stderr: %q", stderr.String())
	}
}

func TestRunHelpSupportsStatusAndReport(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{topic: "status", want: "status [flags]"},
		{topic: "report", want: "report [flags]"},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if code := runHelpTo(&stdout, &stderr, []string{tt.topic}); code != 0 {
				t.Fatalf("runHelpTo(%q) exit code = %d, stderr = %q", tt.topic, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("help for %q missing %q:\n%s", tt.topic, tt.want, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("help for %q wrote stderr: %q", tt.topic, stderr.String())
			}
		})
	}
}
