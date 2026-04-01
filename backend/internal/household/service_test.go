package household

import (
	"context"
	"errors"
	"testing"
)

func TestServiceListReturnsSeededDefaultProfile(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)

	profiles, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected one profile, got %d", len(profiles))
	}
	if profiles[0].ID != DefaultProfileID {
		t.Fatalf("expected default profile id %q, got %q", DefaultProfileID, profiles[0].ID)
	}
	if !profiles[0].Permissions.Settings {
		t.Fatalf("expected default profile to have settings access")
	}

	stored, ok, err := store.Get(context.Background(), DefaultProfileID)
	if err != nil {
		t.Fatalf("get default profile from seeded store: %v", err)
	}
	if !ok {
		t.Fatal("expected default profile to be present in seeded store")
	}
	if stored.ID != DefaultProfileID {
		t.Fatalf("expected seeded default profile id %q, got %q", DefaultProfileID, stored.ID)
	}
}

func TestServiceSaveNormalizesProfile(t *testing.T) {
	service := NewService(NewMemoryStore())
	maxFSK := -3

	saved, err := service.Save(context.Background(), Profile{
		ID:                  " CHILD ",
		Name:                "  Kinderzimmer ",
		Kind:                ProfileKindChild,
		MaxFSK:              &maxFSK,
		AllowedBouquets:     []string{" Kids ", "kids"},
		AllowedServiceRefs:  []string{"1:0:1:abcd:", "1:0:1:ABCD"},
		FavoriteServiceRefs: []string{"1:0:1:ffff:", "1:0:1:FFFF"},
		Permissions: Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
	})
	if err != nil {
		t.Fatalf("save profile: %v", err)
	}

	if saved.ID != "child" {
		t.Fatalf("expected normalized id child, got %q", saved.ID)
	}
	if saved.Name != "Kinderzimmer" {
		t.Fatalf("expected trimmed name, got %q", saved.Name)
	}
	if saved.MaxFSK == nil || *saved.MaxFSK != 0 {
		t.Fatalf("expected normalized max FSK 0, got %v", saved.MaxFSK)
	}
	if len(saved.AllowedBouquets) != 1 || saved.AllowedBouquets[0] != "kids" {
		t.Fatalf("expected deduped bouquets, got %#v", saved.AllowedBouquets)
	}
	if len(saved.AllowedServiceRefs) != 1 || saved.AllowedServiceRefs[0] != "1:0:1:ABCD" {
		t.Fatalf("expected canonical service refs, got %#v", saved.AllowedServiceRefs)
	}
	if len(saved.FavoriteServiceRefs) != 1 || saved.FavoriteServiceRefs[0] != "1:0:1:FFFF" {
		t.Fatalf("expected canonical favorite refs, got %#v", saved.FavoriteServiceRefs)
	}
}

func TestServiceDeleteProtectsLastProfile(t *testing.T) {
	service := NewService(NewMemoryStore())

	err := service.Delete(context.Background(), DefaultProfileID)
	if !errors.Is(err, ErrLastProfile) {
		t.Fatalf("expected ErrLastProfile, got %v", err)
	}
}

func TestServiceDeleteRemovesExistingProfileWhenAnotherRemains(t *testing.T) {
	service := NewService(NewMemoryStore())

	if _, err := service.Save(context.Background(), CreateDefaultProfile()); err != nil {
		t.Fatalf("save default profile: %v", err)
	}
	if _, err := service.Save(context.Background(), Profile{
		ID:   "adult-2",
		Name: "Wohnzimmer",
		Kind: ProfileKindAdult,
		Permissions: Permissions{
			DVRPlayback: true,
			DVRManage:   true,
			Settings:    true,
		},
	}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}

	if err := service.Delete(context.Background(), DefaultProfileID); err != nil {
		t.Fatalf("delete default profile: %v", err)
	}

	profiles, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "adult-2" {
		t.Fatalf("expected remaining profile adult-2, got %#v", profiles)
	}
}
