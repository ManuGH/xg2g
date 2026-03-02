// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package net

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateOutboundURL(t *testing.T) {
	baseAllow := OutboundAllowlist{
		Hosts:   []string{"192.0.2.10"},
		CIDRs:   []string{},
		Ports:   []int{80, 443},
		Schemes: []string{"http", "https"},
	}

	cases := []struct {
		name     string
		policy   OutboundPolicy
		rawURL   string
		wantErr  bool
		errMatch func(error) bool
	}{
		// === Fail-closed behavior ===
		{
			name:    "disabled",
			policy:  OutboundPolicy{Enabled: false, Allow: baseAllow},
			rawURL:  "http://example.com",
			wantErr: true,
			errMatch: func(err error) bool {
				return errors.Is(err, ErrOutboundDisabled)
			},
		},
		// === IPv4 blocked IPs ===
		{
			name:    "reject metadata ip",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://169.254.169.254",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "blocked ip")
			},
		},
		{
			name:    "reject loopback ip",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://127.0.0.1",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "blocked ip")
			},
		},
		{
			name:    "reject private ip not allowlisted",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://10.10.55.64",
			wantErr: true,
			errMatch: func(err error) bool {
				return errors.Is(err, ErrOutboundNotAllowed)
			},
		},
		// === IPv6 blocked IPs ===
		{
			name:    "reject IPv6 loopback ::1",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://[::1]",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "blocked ip")
			},
		},
		{
			name:    "reject IPv4-mapped IPv6 ::ffff:127.0.0.1",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://[::ffff:127.0.0.1]",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "blocked ip")
			},
		},
		{
			name:    "reject IPv6 link-local fe80::1",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://[fe80::1]",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "blocked ip")
			},
		},
		// === Userinfo rejection ===
		{
			name:    "reject userinfo in URL",
			policy:  OutboundPolicy{Enabled: true, Allow: baseAllow},
			rawURL:  "http://user:pass@192.0.2.10",
			wantErr: true,
			errMatch: func(err error) bool {
				return strings.Contains(err.Error(), "userinfo not allowed")
			},
		},
		// === Host normalization edge cases ===
		{
			name: "normalize trailing dot",
			policy: OutboundPolicy{Enabled: true, Allow: OutboundAllowlist{
				Hosts:   []string{"192.0.2.10"},
				Ports:   []int{80},
				Schemes: []string{"http"},
			}},
			rawURL:  "http://192.0.2.10.",
			wantErr: false,
		},
		{
			name: "normalize port :80 to default",
			policy: OutboundPolicy{Enabled: true, Allow: OutboundAllowlist{
				Hosts:   []string{"192.0.2.10"},
				Ports:   []int{80},
				Schemes: []string{"http"},
			}},
			rawURL:  "http://192.0.2.10:80",
			wantErr: false,
		},
		// === Positive cases ===
		{
			name: "allow allowlisted host+port+scheme",
			policy: OutboundPolicy{Enabled: true, Allow: OutboundAllowlist{
				Hosts:   []string{"192.0.2.10"},
				Ports:   []int{80},
				Schemes: []string{"http"},
			}},
			rawURL:  "http://192.0.2.10",
			wantErr: false,
		},
		{
			name: "allow allowlisted cidr",
			policy: OutboundPolicy{Enabled: true, Allow: OutboundAllowlist{
				CIDRs:   []string{"127.0.0.0/8"},
				Ports:   []int{80},
				Schemes: []string{"http"},
			}},
			rawURL:  "http://127.0.0.1",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateOutboundURL(context.Background(), tc.rawURL, tc.policy)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errMatch != nil && !tc.errMatch(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
