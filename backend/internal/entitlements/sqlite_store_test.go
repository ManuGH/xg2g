package entitlements

import (
	"context"
	"testing"
	"time"
)

func TestSqliteStoreUpsertIsIdempotentPerPrincipalScopeSource(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	firstGrantedAt := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	secondGrantedAt := firstGrantedAt.Add(2 * time.Hour)
	secondExpiresAt := secondGrantedAt.Add(24 * time.Hour)

	firstGrant := Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      SourceGooglePlay,
		GrantedAt:   firstGrantedAt,
	}
	secondGrant := Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      SourceGooglePlay,
		GrantedAt:   secondGrantedAt,
		ExpiresAt:   &secondExpiresAt,
	}

	if err := store.Upsert(context.Background(), firstGrant); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.Upsert(context.Background(), secondGrant); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected exactly one upserted grant, got %d", len(grants))
	}
	if !grants[0].GrantedAt.Equal(secondGrantedAt) {
		t.Fatalf("expected upsert to keep latest grantedAt %s, got %s", secondGrantedAt, grants[0].GrantedAt)
	}
	if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(secondExpiresAt) {
		t.Fatalf("expected upsert to keep latest expiresAt %v, got %v", secondExpiresAt, grants[0].ExpiresAt)
	}
}
