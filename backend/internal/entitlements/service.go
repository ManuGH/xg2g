package entitlements

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultCacheTTL = 5 * time.Second

type GrantStatus struct {
	Scope     string
	Source    string
	GrantedAt time.Time
	ExpiresAt *time.Time
	Active    bool
}

type StatusRequest struct {
	PrincipalID    string
	BaseScopes     []string
	RequiredScopes []string
	Model          string
	ProductName    string
	PurchaseURL    string
	Enforcement    string
}

type Status struct {
	PrincipalID    string
	Model          string
	ProductName    string
	PurchaseURL    string
	Enforcement    string
	RequiredScopes []string
	GrantedScopes  []string
	MissingScopes  []string
	Unlocked       bool
	Grants         []GrantStatus
}

type Service struct {
	store    Store
	clock    func() time.Time
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]cachedGrantSet
}

type cachedGrantSet struct {
	fetchedAt time.Time
	grants    []Grant
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func WithCacheTTL(ttl time.Duration) Option {
	return func(s *Service) {
		s.cacheTTL = ttl
	}
}

func NewService(store Store, opts ...Option) *Service {
	svc := &Service{
		store:    store,
		clock:    func() time.Time { return time.Now().UTC() },
		cacheTTL: defaultCacheTTL,
		cache:    make(map[string]cachedGrantSet),
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func (s *Service) EffectiveScopes(ctx context.Context, principalID string, baseScopes, allowedScopes []string) ([]string, error) {
	normalizedBase := normalizeScopes(baseScopes)
	normalizedAllowed := normalizeScopes(allowedScopes)
	if s == nil || s.store == nil || len(normalizedAllowed) == 0 {
		return normalizedBase, nil
	}

	grants, err := s.loadGrants(ctx, principalID)
	if err != nil {
		return nil, err
	}
	entitlementScopes := activeGrantedScopes(grants, normalizedAllowed, s.clock())
	return mergeScopes(normalizedBase, entitlementScopes), nil
}

func (s *Service) Status(ctx context.Context, req StatusRequest) (Status, error) {
	status := Status{
		PrincipalID:    normalizePrincipalID(req.PrincipalID),
		Model:          strings.TrimSpace(req.Model),
		ProductName:    strings.TrimSpace(req.ProductName),
		PurchaseURL:    strings.TrimSpace(req.PurchaseURL),
		Enforcement:    strings.TrimSpace(req.Enforcement),
		RequiredScopes: normalizeScopes(req.RequiredScopes),
	}

	effectiveScopes, err := s.EffectiveScopes(ctx, req.PrincipalID, req.BaseScopes, req.RequiredScopes)
	if err != nil {
		return Status{}, err
	}

	status.GrantedScopes = intersectScopes(status.RequiredScopes, effectiveScopes)
	status.MissingScopes = differenceScopes(status.RequiredScopes, effectiveScopes)
	status.Unlocked = len(status.MissingScopes) == 0

	if s == nil || s.store == nil {
		return status, nil
	}

	grants, err := s.loadGrants(ctx, req.PrincipalID)
	if err != nil {
		return Status{}, err
	}

	matchingGrants := filterGrantsByScopes(grants, status.RequiredScopes)
	status.Grants = make([]GrantStatus, 0, len(matchingGrants))
	now := s.clock()
	for _, grant := range matchingGrants {
		status.Grants = append(status.Grants, GrantStatus{
			Scope:     grant.Scope,
			Source:    grant.Source,
			GrantedAt: grant.GrantedAt,
			ExpiresAt: cloneTimePtr(grant.ExpiresAt),
			Active:    isGrantActive(grant, now),
		})
	}
	sortGrantStatuses(status.Grants)
	return status, nil
}

func (s *Service) Grant(ctx context.Context, grant Grant) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("entitlement service unavailable")
	}
	normalized, err := normalizeGrant(grant)
	if err != nil {
		return err
	}
	if normalized.GrantedAt.IsZero() {
		normalized.GrantedAt = s.clock()
	}
	if err := s.store.Upsert(ctx, normalized); err != nil {
		return err
	}
	s.invalidatePrincipal(normalized.PrincipalID)
	return nil
}

func (s *Service) Revoke(ctx context.Context, principalID, scope, source string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("entitlement service unavailable")
	}
	normalizedPrincipalID := normalizePrincipalID(principalID)
	if normalizedPrincipalID == "" {
		return fmt.Errorf("principal id must not be empty")
	}
	if normalizeScope(scope) == "" {
		return fmt.Errorf("scope must not be empty")
	}
	if normalizeSource(source) == "" {
		return fmt.Errorf("source must not be empty")
	}
	if err := s.store.Delete(ctx, normalizedPrincipalID, scope, source); err != nil {
		return err
	}
	s.invalidatePrincipal(normalizedPrincipalID)
	return nil
}

func (s *Service) loadGrants(ctx context.Context, principalID string) ([]Grant, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	normalizedPrincipalID := normalizePrincipalID(principalID)
	if normalizedPrincipalID == "" {
		return nil, nil
	}

	if cached, ok := s.getCachedGrants(normalizedPrincipalID); ok {
		return cloneGrants(cached), nil
	}

	grants, err := s.store.ListByPrincipal(ctx, normalizedPrincipalID)
	if err != nil {
		return nil, err
	}
	s.setCachedGrants(normalizedPrincipalID, grants)
	return cloneGrants(grants), nil
}

func (s *Service) getCachedGrants(principalID string) ([]Grant, bool) {
	s.mu.RLock()
	entry, ok := s.cache[principalID]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if s.cacheTTL > 0 && s.clock().Sub(entry.fetchedAt) > s.cacheTTL {
		s.invalidatePrincipal(principalID)
		return nil, false
	}
	return entry.grants, true
}

func (s *Service) setCachedGrants(principalID string, grants []Grant) {
	if s.cacheTTL == 0 {
		return
	}
	s.mu.Lock()
	s.cache[principalID] = cachedGrantSet{
		fetchedAt: s.clock(),
		grants:    cloneGrants(grants),
	}
	s.mu.Unlock()
}

func (s *Service) invalidatePrincipal(principalID string) {
	s.mu.Lock()
	delete(s.cache, principalID)
	s.mu.Unlock()
}

func normalizeGrant(grant Grant) (Grant, error) {
	normalized := Grant{
		PrincipalID: normalizePrincipalID(grant.PrincipalID),
		Scope:       normalizeScope(grant.Scope),
		Source:      normalizeSource(grant.Source),
		GrantedAt:   grant.GrantedAt.UTC(),
		ExpiresAt:   cloneTimePtr(grant.ExpiresAt),
	}
	if normalized.PrincipalID == "" {
		return Grant{}, fmt.Errorf("principal id must not be empty")
	}
	if normalized.Scope == "" {
		return Grant{}, fmt.Errorf("scope must not be empty")
	}
	if normalized.Source == "" {
		return Grant{}, fmt.Errorf("source must not be empty")
	}
	return normalized, nil
}

func normalizePrincipalID(principalID string) string {
	return strings.TrimSpace(principalID)
}

func normalizeScope(scope string) string {
	return strings.ToLower(strings.TrimSpace(scope))
}

func normalizeSource(source string) string {
	return strings.ToLower(strings.TrimSpace(source))
}

func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		normalized := normalizeScope(scope)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func mergeScopes(baseScopes, extraScopes []string) []string {
	merged := append([]string{}, baseScopes...)
	merged = append(merged, extraScopes...)
	return normalizeScopes(merged)
}

func activeGrantedScopes(grants []Grant, allowedScopes []string, now time.Time) []string {
	if len(allowedScopes) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(allowedScopes))
	for _, scope := range allowedScopes {
		allowed[scope] = struct{}{}
	}

	active := make([]string, 0, len(grants))
	for _, grant := range grants {
		if !isGrantActive(grant, now) {
			continue
		}
		if _, ok := allowed[grant.Scope]; !ok {
			continue
		}
		active = append(active, grant.Scope)
	}
	return normalizeScopes(active)
}

func filterGrantsByScopes(grants []Grant, scopes []string) []Grant {
	if len(scopes) == 0 {
		return cloneGrants(grants)
	}
	allowed := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		allowed[scope] = struct{}{}
	}

	filtered := make([]Grant, 0, len(grants))
	for _, grant := range grants {
		if _, ok := allowed[grant.Scope]; !ok {
			continue
		}
		filtered = append(filtered, cloneGrant(grant))
	}
	sortGrants(filtered)
	return filtered
}

func differenceScopes(requiredScopes, effectiveScopes []string) []string {
	effective := make(map[string]struct{}, len(effectiveScopes))
	for _, scope := range effectiveScopes {
		effective[scope] = struct{}{}
	}
	out := make([]string, 0)
	for _, scope := range requiredScopes {
		if _, ok := effective[scope]; ok {
			continue
		}
		out = append(out, scope)
	}
	return out
}

func intersectScopes(requiredScopes, effectiveScopes []string) []string {
	effective := make(map[string]struct{}, len(effectiveScopes))
	for _, scope := range effectiveScopes {
		effective[scope] = struct{}{}
	}
	out := make([]string, 0, len(requiredScopes))
	for _, scope := range requiredScopes {
		if _, ok := effective[scope]; ok {
			out = append(out, scope)
		}
	}
	return out
}

func isGrantActive(grant Grant, now time.Time) bool {
	if !grant.GrantedAt.IsZero() && grant.GrantedAt.After(now) {
		return false
	}
	return grant.ExpiresAt == nil || grant.ExpiresAt.After(now)
}

func cloneGrants(grants []Grant) []Grant {
	if grants == nil {
		return nil
	}
	out := make([]Grant, len(grants))
	for i, grant := range grants {
		out[i] = cloneGrant(grant)
	}
	return out
}

func cloneGrant(grant Grant) Grant {
	return Grant{
		PrincipalID: grant.PrincipalID,
		Scope:       grant.Scope,
		Source:      grant.Source,
		GrantedAt:   grant.GrantedAt,
		ExpiresAt:   cloneTimePtr(grant.ExpiresAt),
	}
}

func cloneTimePtr(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	cloned := ts.UTC()
	return &cloned
}

func sortGrants(grants []Grant) {
	slices.SortFunc(grants, func(a, b Grant) int {
		if cmp := strings.Compare(a.Scope, b.Scope); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Source, b.Source)
	})
}

func sortGrantStatuses(grants []GrantStatus) {
	slices.SortFunc(grants, func(a, b GrantStatus) int {
		if cmp := strings.Compare(a.Scope, b.Scope); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Source, b.Source)
	})
}

func grantKey(principalID, scope, source string) string {
	return principalID + "\x00" + scope + "\x00" + source
}
