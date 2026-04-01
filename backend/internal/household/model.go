package household

import (
	"errors"
	"sort"
	"strings"
)

type ProfileKind string

const (
	ProfileKindAdult ProfileKind = "adult"
	ProfileKindChild ProfileKind = "child"

	DefaultProfileID = "household-default"
	ProfileHeader    = "X-Household-Profile"
)

var ErrInvalidProfileID = errors.New("household profile id must not be empty")

type Permissions struct {
	DVRPlayback bool
	DVRManage   bool
	Settings    bool
}

type Profile struct {
	ID                  string
	Name                string
	Kind                ProfileKind
	MaxFSK              *int
	AllowedBouquets     []string
	AllowedServiceRefs  []string
	FavoriteServiceRefs []string
	Permissions         Permissions
}

func CreateDefaultProfile() Profile {
	return Profile{
		ID:   DefaultProfileID,
		Name: "Haushalt",
		Kind: ProfileKindAdult,
		Permissions: Permissions{
			DVRPlayback: true,
			DVRManage:   true,
			Settings:    true,
		},
	}
}

func NormalizeProfile(profile Profile) Profile {
	normalizedKind := normalizeKind(profile.Kind)
	normalizedID := normalizeIdentifier(profile.ID)
	if normalizedID == "" {
		normalizedID = DefaultProfileID
	}

	normalized := Profile{
		ID:                  normalizedID,
		Name:                normalizeName(profile.Name),
		Kind:                normalizedKind,
		MaxFSK:              normalizeMaxFSK(profile.MaxFSK),
		AllowedBouquets:     normalizeIdentifierList(profile.AllowedBouquets),
		AllowedServiceRefs:  normalizeServiceRefList(profile.AllowedServiceRefs),
		FavoriteServiceRefs: normalizeServiceRefList(profile.FavoriteServiceRefs),
		Permissions: Permissions{
			DVRPlayback: profile.Permissions.DVRPlayback,
			DVRManage:   profile.Permissions.DVRManage,
			Settings:    profile.Permissions.Settings,
		},
	}

	if normalized.Name == "" {
		normalized.Name = defaultNameForProfile(normalizedID, normalizedKind)
	}

	return normalized
}

func PrepareProfile(profile Profile) (Profile, error) {
	if normalizeIdentifier(profile.ID) == "" {
		return Profile{}, ErrInvalidProfileID
	}
	return NormalizeProfile(profile), nil
}

func NormalizeProfiles(profiles []Profile) []Profile {
	if len(profiles) == 0 {
		return []Profile{CreateDefaultProfile()}
	}

	normalized := make([]Profile, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		entry := NormalizeProfile(profile)
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		normalized = append(normalized, entry)
	}
	if len(normalized) == 0 {
		return []Profile{CreateDefaultProfile()}
	}
	sortProfiles(normalized)
	return normalized
}

func HasServiceRestrictions(profile Profile) bool {
	return HasServiceRestrictionsNormalized(NormalizeProfile(profile))
}

func IsServiceAllowed(profile Profile, serviceRef, bouquet string) bool {
	return IsServiceAllowedNormalized(NormalizeProfile(profile), serviceRef, bouquet)
}

func HasServiceRestrictionsNormalized(profile Profile) bool {
	return len(profile.AllowedBouquets) > 0 || len(profile.AllowedServiceRefs) > 0
}

func IsServiceAllowedNormalized(profile Profile, serviceRef, bouquet string) bool {
	if !HasServiceRestrictionsNormalized(profile) {
		return true
	}

	normalizedServiceRef := normalizeServiceRef(serviceRef)
	if normalizedServiceRef != "" {
		for _, allowed := range profile.AllowedServiceRefs {
			if allowed == normalizedServiceRef {
				return true
			}
		}
	}

	normalizedBouquet := normalizeIdentifier(bouquet)
	if normalizedBouquet != "" {
		for _, allowed := range profile.AllowedBouquets {
			if allowed == normalizedBouquet {
				return true
			}
		}
	}

	return false
}

func CanAccessDVRPlayback(profile Profile) bool {
	return NormalizeProfile(profile).Permissions.DVRPlayback
}

func CanManageDVR(profile Profile) bool {
	return NormalizeProfile(profile).Permissions.DVRManage
}

func CanAccessSettings(profile Profile) bool {
	return NormalizeProfile(profile).Permissions.Settings
}

func CloneProfile(profile Profile) Profile {
	cloned := NormalizeProfile(profile)
	cloned.AllowedBouquets = append([]string(nil), cloned.AllowedBouquets...)
	cloned.AllowedServiceRefs = append([]string(nil), cloned.AllowedServiceRefs...)
	cloned.FavoriteServiceRefs = append([]string(nil), cloned.FavoriteServiceRefs...)
	cloned.MaxFSK = cloneIntPtr(cloned.MaxFSK)
	return cloned
}

func cloneProfiles(profiles []Profile) []Profile {
	cloned := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		cloned = append(cloned, CloneProfile(profile))
	}
	return cloned
}

func sortProfiles(profiles []Profile) {
	sort.SliceStable(profiles, func(i, j int) bool {
		left := profiles[i]
		right := profiles[j]
		if left.ID == DefaultProfileID && right.ID != DefaultProfileID {
			return true
		}
		if left.ID != DefaultProfileID && right.ID == DefaultProfileID {
			return false
		}

		leftName := strings.ToLower(strings.TrimSpace(left.Name))
		rightName := strings.ToLower(strings.TrimSpace(right.Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return left.ID < right.ID
	})
}

func defaultNameForProfile(id string, kind ProfileKind) string {
	if id == DefaultProfileID {
		return CreateDefaultProfile().Name
	}
	if kind == ProfileKindChild {
		return "Kinderprofil"
	}
	return "Profil"
}

func normalizeKind(kind ProfileKind) ProfileKind {
	if kind == ProfileKindChild {
		return ProfileKindChild
	}
	return ProfileKindAdult
}

func normalizeName(value string) string {
	return strings.TrimSpace(value)
}

func normalizeIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeServiceRef(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ":")
	if value == "" {
		return ""
	}
	if looksLikeHexColonServiceRef(value) {
		return strings.ToUpper(value)
	}
	return value
}

func looksLikeHexColonServiceRef(value string) bool {
	if !strings.Contains(value, ":") {
		return false
	}
	for _, ch := range value {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func normalizeIdentifierList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeIdentifier(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeServiceRefList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeServiceRef(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeMaxFSK(value *int) *int {
	if value == nil {
		return nil
	}
	normalized := *value
	if normalized < 0 {
		normalized = 0
	}
	return &normalized
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
