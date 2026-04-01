package entitlements

import (
	"context"
	"testing"
	"time"
)

func TestServiceEffectiveScopesIncludesActiveAllowedEntitlements(t *testing.T) {
	now := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service := NewService(store, WithClock(func() time.Time { return now }), WithCacheTTL(time.Hour))

	if err := service.Grant(context.Background(), Grant{
		PrincipalID: "alice",
		Scope:       "xg2g:unlock",
		Source:      SourceAdminOverride,
	}); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}
	if err := service.Grant(context.Background(), Grant{
		PrincipalID: "alice",
		Scope:       "v3:admin",
		Source:      SourceAdminOverride,
	}); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}

	scopes, err := service.EffectiveScopes(context.Background(), "alice", []string{"v3:read"}, []string{"xg2g:unlock"})
	if err != nil {
		t.Fatalf("effective scopes: %v", err)
	}

	if got, want := len(scopes), 2; got != want {
		t.Fatalf("expected %d scopes, got %d (%v)", want, got, scopes)
	}
	if scopes[0] != "v3:read" || scopes[1] != "xg2g:unlock" {
		t.Fatalf("unexpected scopes: %v", scopes)
	}
}

func TestServiceStatusIgnoresExpiredGrants(t *testing.T) {
	now := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Hour)
	store := NewMemoryStore()
	service := NewService(store, WithClock(func() time.Time { return now }), WithCacheTTL(time.Hour))

	if err := service.Grant(context.Background(), Grant{
		PrincipalID: "alice",
		Scope:       "xg2g:unlock",
		Source:      SourceAdminOverride,
		GrantedAt:   now.Add(-2 * time.Hour),
		ExpiresAt:   &expiredAt,
	}); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}

	status, err := service.Status(context.Background(), StatusRequest{
		PrincipalID:    "alice",
		RequiredScopes: []string{"xg2g:unlock"},
		ProductName:    "xg2g Unlock",
	})
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Unlocked {
		t.Fatal("expected expired grant to keep principal locked")
	}
	if len(status.MissingScopes) != 1 || status.MissingScopes[0] != "xg2g:unlock" {
		t.Fatalf("unexpected missing scopes: %v", status.MissingScopes)
	}
	if len(status.Grants) != 1 || status.Grants[0].Active {
		t.Fatalf("expected one inactive grant, got %+v", status.Grants)
	}
}

func TestServiceRevokeInvalidatesCache(t *testing.T) {
	now := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service := NewService(store, WithClock(func() time.Time { return now }), WithCacheTTL(time.Hour))

	if err := service.Grant(context.Background(), Grant{
		PrincipalID: "alice",
		Scope:       "xg2g:unlock",
		Source:      SourceAdminOverride,
	}); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}

	if _, err := service.EffectiveScopes(context.Background(), "alice", nil, []string{"xg2g:unlock"}); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	if err := service.Revoke(context.Background(), "alice", "xg2g:unlock", SourceAdminOverride); err != nil {
		t.Fatalf("revoke entitlement: %v", err)
	}

	scopes, err := service.EffectiveScopes(context.Background(), "alice", nil, []string{"xg2g:unlock"})
	if err != nil {
		t.Fatalf("effective scopes: %v", err)
	}
	if len(scopes) != 0 {
		t.Fatalf("expected revoked entitlement to be removed, got %v", scopes)
	}
}
