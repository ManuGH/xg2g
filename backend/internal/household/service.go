package household

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNotFound    = errors.New("household profile not found")
	ErrLastProfile = errors.New("cannot delete the last household profile")
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context) ([]Profile, error) {
	if s == nil || s.store == nil {
		return []Profile{CreateDefaultProfile()}, nil
	}

	profiles, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return NormalizeProfiles(profiles), nil
}

func (s *Service) Resolve(ctx context.Context, id string) (Profile, error) {
	if s == nil || s.store == nil {
		return CreateDefaultProfile(), nil
	}

	normalizedID := normalizeIdentifier(id)
	if normalizedID == "" || normalizedID == DefaultProfileID {
		stored, ok, err := s.store.Get(ctx, DefaultProfileID)
		if err != nil {
			return Profile{}, err
		}
		if ok {
			return CloneProfile(stored), nil
		}
		return CloneProfile(CreateDefaultProfile()), nil
	}

	profile, ok, err := s.store.Get(ctx, normalizedID)
	if err != nil {
		return Profile{}, err
	}
	if !ok {
		return Profile{}, fmt.Errorf("%w: %s", ErrNotFound, normalizedID)
	}
	return CloneProfile(profile), nil
}

func (s *Service) Save(ctx context.Context, profile Profile) (Profile, error) {
	if s == nil || s.store == nil {
		return Profile{}, fmt.Errorf("household service unavailable")
	}

	normalized, err := PrepareProfile(profile)
	if err != nil {
		return Profile{}, err
	}
	if err := s.store.Upsert(ctx, normalized); err != nil {
		return Profile{}, err
	}
	return CloneProfile(normalized), nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("household service unavailable")
	}

	normalizedID := normalizeIdentifier(id)
	if normalizedID == "" {
		return ErrInvalidProfileID
	}

	profiles, err := s.List(ctx)
	if err != nil {
		return err
	}

	found := false
	for _, profile := range profiles {
		if profile.ID == normalizedID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, normalizedID)
	}
	if len(profiles) <= 1 {
		return ErrLastProfile
	}
	return s.store.Delete(ctx, normalizedID)
}
