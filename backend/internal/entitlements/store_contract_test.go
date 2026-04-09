package entitlements

import (
	"context"
	"strings"
	"testing"
	"time"
)

func openEntitlementStore(t *testing.T, backend string) Store {
	t.Helper()

	store, err := NewStore(backend, t.TempDir())
	if err != nil {
		t.Fatalf("new %s entitlement store: %v", backend, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close %s entitlement store: %v", backend, err)
		}
	})
	return store
}

func TestStoreContract_ListByPrincipalNormalizesAndOrdersGrants(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openEntitlementStore(t, backend)
			ctx := context.Background()
			expiresAt := time.Date(2026, time.May, 1, 10, 0, 0, 0, time.UTC)

			for _, grant := range []Grant{
				{
					PrincipalID: " viewer ",
					Scope:       " XG2G:DVR ",
					Source:      " Amazon_Appstore ",
					GrantedAt:   time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC),
				},
				{
					PrincipalID: "viewer",
					Scope:       " xg2g:unlock ",
					Source:      " Google_Play ",
					GrantedAt:   time.Date(2026, time.April, 1, 11, 0, 0, 0, time.UTC),
					ExpiresAt:   &expiresAt,
				},
				{
					PrincipalID: "viewer",
					Scope:       "xg2g:dvr",
					Source:      " Admin_Override ",
					GrantedAt:   time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC),
				},
				{
					PrincipalID: "other",
					Scope:       "xg2g:dvr",
					Source:      SourceAdminOverride,
					GrantedAt:   time.Date(2026, time.April, 1, 13, 0, 0, 0, time.UTC),
				},
			} {
				if err := store.Upsert(ctx, grant); err != nil {
					t.Fatalf("upsert grant %#v: %v", grant, err)
				}
			}

			grants, err := store.ListByPrincipal(ctx, " viewer ")
			if err != nil {
				t.Fatalf("list grants: %v", err)
			}
			want := []struct {
				scope  string
				source string
			}{
				{scope: "xg2g:dvr", source: "admin_override"},
				{scope: "xg2g:dvr", source: "amazon_appstore"},
				{scope: "xg2g:unlock", source: "google_play"},
			}
			if len(grants) != len(want) {
				t.Fatalf("expected %d grants, got %d", len(want), len(grants))
			}
			for i, wantGrant := range want {
				if grants[i].PrincipalID != "viewer" {
					t.Fatalf("expected normalized principal viewer, got %q", grants[i].PrincipalID)
				}
				if grants[i].Scope != wantGrant.scope {
					t.Fatalf("expected grant %d scope %q, got %q", i, wantGrant.scope, grants[i].Scope)
				}
				if grants[i].Source != wantGrant.source {
					t.Fatalf("expected grant %d source %q, got %q", i, wantGrant.source, grants[i].Source)
				}
			}
			if grants[2].ExpiresAt == nil || !grants[2].ExpiresAt.Equal(expiresAt) {
				t.Fatalf("expected unlock grant expiry %v, got %v", expiresAt, grants[2].ExpiresAt)
			}
		})
	}
}

func TestStoreContract_UpsertCanonicalCollisionKeepsLatestGrant(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openEntitlementStore(t, backend)
			ctx := context.Background()

			firstGrantedAt := time.Date(2026, time.April, 3, 8, 0, 0, 0, time.UTC)
			secondGrantedAt := firstGrantedAt.Add(2 * time.Hour)
			secondExpiresAt := secondGrantedAt.Add(24 * time.Hour)
			if err := store.Upsert(ctx, Grant{
				PrincipalID: " viewer ",
				Scope:       " XG2G:DVR ",
				Source:      " ADMIN_OVERRIDE ",
				GrantedAt:   firstGrantedAt,
			}); err != nil {
				t.Fatalf("upsert first colliding grant: %v", err)
			}
			if err := store.Upsert(ctx, Grant{
				PrincipalID: "viewer",
				Scope:       "xg2g:dvr",
				Source:      SourceAdminOverride,
				GrantedAt:   secondGrantedAt,
				ExpiresAt:   &secondExpiresAt,
			}); err != nil {
				t.Fatalf("upsert second colliding grant: %v", err)
			}

			grants, err := store.ListByPrincipal(ctx, " viewer ")
			if err != nil {
				t.Fatalf("list grants after collision upserts: %v", err)
			}
			if len(grants) != 1 {
				t.Fatalf("expected one canonical colliding grant, got %#v", grants)
			}
			if !grants[0].GrantedAt.Equal(secondGrantedAt) {
				t.Fatalf("expected latest colliding grant to win, got %#v", grants[0])
			}
			if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(secondExpiresAt) {
				t.Fatalf("expected latest colliding expiry %v, got %v", secondExpiresAt, grants[0].ExpiresAt)
			}
		})
	}
}

func TestStoreContract_DeleteMissingIsNoopAndDeleteRemovesGrant(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openEntitlementStore(t, backend)
			ctx := context.Background()
			grant := Grant{
				PrincipalID: "viewer",
				Scope:       "xg2g:unlock",
				Source:      SourceGooglePlay,
				GrantedAt:   time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
			}
			if err := store.Delete(ctx, "viewer", grant.Scope, grant.Source); err != nil {
				t.Fatalf("delete missing grant: %v", err)
			}
			if err := store.Upsert(ctx, grant); err != nil {
				t.Fatalf("upsert grant: %v", err)
			}
			if err := store.Delete(ctx, " viewer ", " XG2G:UNLOCK ", " GOOGLE_PLAY "); err != nil {
				t.Fatalf("delete existing grant: %v", err)
			}

			grants, err := store.ListByPrincipal(ctx, "viewer")
			if err != nil {
				t.Fatalf("list grants after delete: %v", err)
			}
			if len(grants) != 0 {
				t.Fatalf("expected no grants after delete, got %#v", grants)
			}
		})
	}
}

func TestStoreContract_UpsertRejectsInvalidGrant(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openEntitlementStore(t, backend)
			ctx := context.Background()
			cases := []Grant{
				{PrincipalID: " ", Scope: "xg2g:unlock", Source: SourceGooglePlay, GrantedAt: time.Now().UTC()},
				{PrincipalID: "viewer", Scope: " ", Source: SourceGooglePlay, GrantedAt: time.Now().UTC()},
				{PrincipalID: "viewer", Scope: "xg2g:unlock", Source: " ", GrantedAt: time.Now().UTC()},
			}
			for _, grant := range cases {
				err := store.Upsert(ctx, grant)
				if err == nil {
					t.Fatalf("expected invalid grant %#v to fail", grant)
				}
				if !strings.Contains(err.Error(), "must not be empty") {
					t.Fatalf("expected validation error for %#v, got %v", grant, err)
				}
			}
		})
	}
}
