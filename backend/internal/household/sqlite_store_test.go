package household

import (
	"context"
	"testing"
)

func TestSqliteStoreRoundTrip(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	maxFSK := 12
	input := Profile{
		ID:                  "child-room",
		Name:                "Kinderzimmer",
		Kind:                ProfileKindChild,
		MaxFSK:              &maxFSK,
		AllowedBouquets:     []string{"kids"},
		AllowedServiceRefs:  []string{"1:0:1:ABCD"},
		FavoriteServiceRefs: []string{"1:0:1:FFFF"},
		Permissions: Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
	}

	if err := store.Upsert(context.Background(), input); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	profile, ok, err := store.Get(context.Background(), "child-room")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if !ok {
		t.Fatal("expected stored profile to exist")
	}
	if profile.ID != input.ID || profile.Name != input.Name {
		t.Fatalf("unexpected stored profile: %#v", profile)
	}
	if len(profile.AllowedServiceRefs) != 1 || profile.AllowedServiceRefs[0] != "1:0:1:ABCD" {
		t.Fatalf("unexpected service refs: %#v", profile.AllowedServiceRefs)
	}

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles including default, got %d", len(profiles))
	}
}

func TestSqliteStoreSeedsDefaultProfileOnInit(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	profile, ok, err := store.Get(context.Background(), DefaultProfileID)
	if err != nil {
		t.Fatalf("get default profile: %v", err)
	}
	if !ok {
		t.Fatal("expected sqlite store to seed default profile")
	}
	if profile.ID != DefaultProfileID {
		t.Fatalf("expected default profile id %q, got %q", DefaultProfileID, profile.ID)
	}
}
