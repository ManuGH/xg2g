package household

import (
	"context"
	"errors"
	"testing"
)

func openHouseholdStore(t *testing.T, backend string) Store {
	t.Helper()

	store, err := NewStore(backend, t.TempDir())
	if err != nil {
		t.Fatalf("new %s household store: %v", backend, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close %s household store: %v", backend, err)
		}
	})
	return store
}

func TestStoreContract_ListNormalizesAndOrdersProfiles(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openHouseholdStore(t, backend)
			ctx := context.Background()

			maxFSK := -5
			for _, profile := range []Profile{
				{
					ID:   " adult-b ",
					Name: "  Alpha  ",
					Kind: ProfileKindAdult,
				},
				{
					ID:                  " CHILD-Z ",
					Name:                "  Zebra ",
					Kind:                ProfileKindChild,
					MaxFSK:              &maxFSK,
					AllowedBouquets:     []string{" Kids ", "kids"},
					AllowedServiceRefs:  []string{"1:0:1:abcd:", "1:0:1:ABCD"},
					FavoriteServiceRefs: []string{"1:0:1:ffff:", "1:0:1:FFFF"},
				},
				{
					ID:   "adult-a",
					Name: "alpha",
					Kind: ProfileKindAdult,
				},
			} {
				if err := store.Upsert(ctx, profile); err != nil {
					t.Fatalf("upsert profile %#v: %v", profile.ID, err)
				}
			}

			got, ok, err := store.Get(ctx, " child-z ")
			if err != nil {
				t.Fatalf("get normalized profile: %v", err)
			}
			if !ok {
				t.Fatal("expected normalized child-z profile")
			}
			if got.ID != "child-z" {
				t.Fatalf("expected normalized id child-z, got %q", got.ID)
			}
			if got.MaxFSK == nil || *got.MaxFSK != 0 {
				t.Fatalf("expected normalized max FSK 0, got %v", got.MaxFSK)
			}
			if len(got.AllowedBouquets) != 1 || got.AllowedBouquets[0] != "kids" {
				t.Fatalf("expected normalized bouquets, got %#v", got.AllowedBouquets)
			}
			if len(got.AllowedServiceRefs) != 1 || got.AllowedServiceRefs[0] != "1:0:1:ABCD" {
				t.Fatalf("expected canonical service refs, got %#v", got.AllowedServiceRefs)
			}
			if len(got.FavoriteServiceRefs) != 1 || got.FavoriteServiceRefs[0] != "1:0:1:FFFF" {
				t.Fatalf("expected canonical favorite refs, got %#v", got.FavoriteServiceRefs)
			}

			profiles, err := store.List(ctx)
			if err != nil {
				t.Fatalf("list profiles: %v", err)
			}
			wantIDs := []string{DefaultProfileID, "adult-a", "adult-b", "child-z"}
			if len(profiles) != len(wantIDs) {
				t.Fatalf("expected %d profiles, got %d", len(wantIDs), len(profiles))
			}
			for i, wantID := range wantIDs {
				if profiles[i].ID != wantID {
					t.Fatalf("expected profile %d to be %q, got %q", i, wantID, profiles[i].ID)
				}
			}
		})
	}
}

func TestStoreContract_UpsertCanonicalCollisionKeepsLatestProfile(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openHouseholdStore(t, backend)
			ctx := context.Background()

			firstMaxFSK := 6
			secondMaxFSK := 12
			if err := store.Upsert(ctx, Profile{
				ID:                  " Child-Room ",
				Name:                "Kinderzimmer Alt",
				Kind:                "CHILD",
				MaxFSK:              &firstMaxFSK,
				AllowedBouquets:     []string{"kids-alt"},
				AllowedServiceRefs:  []string{"1:0:1:AAAA"},
				FavoriteServiceRefs: []string{"1:0:1:BBBB"},
			}); err != nil {
				t.Fatalf("upsert first colliding profile: %v", err)
			}
			if err := store.Upsert(ctx, Profile{
				ID:                  "child-room",
				Name:                " Kinderzimmer Neu ",
				Kind:                ProfileKindChild,
				MaxFSK:              &secondMaxFSK,
				AllowedBouquets:     []string{" Kids-Neu ", "kids-neu"},
				AllowedServiceRefs:  []string{"1:0:1:cccc:", "1:0:1:CCCC"},
				FavoriteServiceRefs: []string{"1:0:1:dddd:", "1:0:1:DDDD"},
			}); err != nil {
				t.Fatalf("upsert second colliding profile: %v", err)
			}

			got, ok, err := store.Get(ctx, "CHILD-ROOM")
			if err != nil {
				t.Fatalf("get colliding profile: %v", err)
			}
			if !ok {
				t.Fatal("expected canonical child-room profile")
			}
			if got.Name != "Kinderzimmer Neu" {
				t.Fatalf("expected latest colliding profile name to win, got %#v", got)
			}
			if got.MaxFSK == nil || *got.MaxFSK != secondMaxFSK {
				t.Fatalf("expected latest colliding max FSK %d, got %v", secondMaxFSK, got.MaxFSK)
			}
			if len(got.AllowedBouquets) != 1 || got.AllowedBouquets[0] != "kids-neu" {
				t.Fatalf("expected latest colliding bouquets to win canonically, got %#v", got.AllowedBouquets)
			}
			if len(got.AllowedServiceRefs) != 1 || got.AllowedServiceRefs[0] != "1:0:1:CCCC" {
				t.Fatalf("expected latest colliding service refs to win canonically, got %#v", got.AllowedServiceRefs)
			}
			if len(got.FavoriteServiceRefs) != 1 || got.FavoriteServiceRefs[0] != "1:0:1:DDDD" {
				t.Fatalf("expected latest colliding favorite refs to win canonically, got %#v", got.FavoriteServiceRefs)
			}

			profiles, err := store.List(ctx)
			if err != nil {
				t.Fatalf("list profiles after collision upserts: %v", err)
			}
			if len(profiles) != 2 {
				t.Fatalf("expected default plus one canonical colliding profile, got %#v", profiles)
			}
			if profiles[1].ID != "child-room" {
				t.Fatalf("expected canonical child-room profile after collision, got %#v", profiles[1])
			}
		})
	}
}

func TestStoreContract_DeleteMissingIsNoopAndDeleteRemovesProfile(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openHouseholdStore(t, backend)
			ctx := context.Background()

			if err := store.Delete(ctx, "missing-profile"); err != nil {
				t.Fatalf("delete missing profile: %v", err)
			}
			if err := store.Upsert(ctx, Profile{ID: "room-1", Name: "Wohnzimmer"}); err != nil {
				t.Fatalf("upsert room-1: %v", err)
			}
			if err := store.Delete(ctx, " room-1 "); err != nil {
				t.Fatalf("delete room-1: %v", err)
			}

			_, ok, err := store.Get(ctx, "room-1")
			if err != nil {
				t.Fatalf("get deleted profile: %v", err)
			}
			if ok {
				t.Fatal("expected deleted profile to be absent")
			}

			_, ok, err = store.Get(ctx, DefaultProfileID)
			if err != nil {
				t.Fatalf("get default profile: %v", err)
			}
			if !ok {
				t.Fatal("expected default profile to remain")
			}
		})
	}
}

func TestStoreContract_UpsertRejectsEmptyProfileID(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openHouseholdStore(t, backend)
			err := store.Upsert(context.Background(), Profile{
				ID:   "   ",
				Name: "Invalid",
			})
			if !errors.Is(err, ErrInvalidProfileID) {
				t.Fatalf("expected ErrInvalidProfileID, got %v", err)
			}
		})
	}
}
