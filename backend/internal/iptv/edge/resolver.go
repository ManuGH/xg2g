// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package edge provides HTTP boundary resolution for IPTV service references.
package edge

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
)

// Kind classifies the outcome of an inbound reference resolution.
type Kind int

const (
	// KindPassThrough indicates a non-IPTV reference (e.g. DVB) that is passed through untouched.
	KindPassThrough Kind = iota
	// KindLegacyRaw indicates a raw IPTV reference (4097, 5001, 5002) passed through as legacy input and tracked.
	KindLegacyRaw
	// KindOpaque indicates a valid opaque IPTV ID (iptv_<id>) successfully resolved via the registry.
	KindOpaque
)

var (
	// ErrNotFound is returned when an opaque IPTV reference is not found in the registry,
	// or when the resolver is unconfigured/disabled.
	// It is a static error that never echoes the client-supplied input.
	ErrNotFound = errors.New("iptv source not found")

	// ErrInvalidID is returned when an input starts with the iptv_ prefix but fails format validation.
	// It is a static error that never echoes the client-supplied input.
	ErrInvalidID = errors.New("invalid iptv source id")
)

// Resolver maps client-supplied channel references at the HTTP edge to internal raw references.
type Resolver struct {
	reg           *sourceref.Registry
	parser        *sourceref.Parser
	legacyIngress func(endpoint string)
}

// NewResolver creates an inbound edge resolver.
func NewResolver(reg *sourceref.Registry, parser *sourceref.Parser, legacyIngress func(endpoint string)) *Resolver {
	return &Resolver{
		reg:           reg,
		parser:        parser,
		legacyIngress: legacyIngress,
	}
}

// ResolveInbound maps a client-supplied reference to the internal raw ref.
//
// Rules:
//  1. Non-IPTV refs (DVB 1:..., anything not starting with 4097:, 5001:, 5002:, or iptv_) => raw == ref, KindPassThrough, nil.
//     Never touched, never counted.
//  2. Raw IPTV refs (4097:, 5001:, 5002:) => raw == ref, KindLegacyRaw, nil; calls legacyIngress(endpoint) exactly once.
//     Never registers into the registry.
//  3. iptv_<id> in registry => raw == source.RawRef(), KindOpaque, nil.
//  4. iptv_ prefix with valid format but not in registry, OR resolver disabled (nil r, nil parser, or nil reg) => ErrNotFound.
//  5. iptv_ prefix with invalid format => ErrInvalidID.
//  6. Leading/trailing whitespace in ref is trimmed before classification.
//  7. A nil *Resolver is valid and behaves as disabled: pass-through and legacy-raw work, opaque => ErrNotFound.
//  8. Error strings never echo the client-supplied input.
func (r *Resolver) ResolveInbound(endpoint, ref string) (raw string, kind Kind, err error) {
	trimmed := strings.TrimSpace(ref)

	// Check for opaque IPTV identifier
	if strings.HasPrefix(trimmed, sourceref.IDPrefix) {
		id := sourceref.ID(trimmed)
		if !id.IsValid() {
			return "", 0, ErrInvalidID
		}
		if r == nil || r.reg == nil || r.parser == nil {
			return "", 0, ErrNotFound
		}
		src, lookupErr := r.reg.Lookup(id)
		if lookupErr != nil {
			return "", 0, ErrNotFound
		}
		return src.RawRef(), KindOpaque, nil
	}

	// Check for raw IPTV references (4097:, 5001:, 5002:)
	if strings.HasPrefix(trimmed, "4097:") || strings.HasPrefix(trimmed, "5001:") || strings.HasPrefix(trimmed, "5002:") {
		if r != nil && r.legacyIngress != nil {
			r.legacyIngress(endpoint)
		}
		return ref, KindLegacyRaw, nil
	}

	// Non-IPTV reference (DVB 1:..., etc.)
	return ref, KindPassThrough, nil
}

// Registry returns the underlying sourceref.Registry, if configured.
func (r *Resolver) Registry() *sourceref.Registry {
	if r == nil {
		return nil
	}
	return r.reg
}

// Parser returns the underlying sourceref.Parser, if configured.
func (r *Resolver) Parser() *sourceref.Parser {
	if r == nil {
		return nil
	}
	return r.parser
}

// EdgeTranslator defines outbound masking and inbound resolution methods at the HTTP edge.
type EdgeTranslator interface {
	MaskServiceRef(rawRef string) string
	MaskServiceRefPtr(ref *string) *string
	MaskLogoURL(logoURL, rawRef string) string
	MaskPiconURL(rawRef string) string
	ResolveOpaqueID(opaqueID string) (rawRef string, ok bool)
}

var _ EdgeTranslator = (*Resolver)(nil)

// MaskServiceRef converts an internal raw reference to an opaque iptv_<id> at the HTTP boundary.
// Non-IPTV references (such as DVB 1:...) and references that are already opaque (iptv_...)
// are preserved untouched.
//
// For IPTV references (4097:, 5001:, 5002:):
//   - If the resolver is unconfigured, disabled, or missing a parser, it fails closed and returns "".
//   - If parsing fails, it fails closed and returns "".
//   - If parsing succeeds, it dynamically registers the parsed Source into the registry
//     (mask-on-emit idempotent registration) and returns string(src.ID()).
func (r *Resolver) MaskServiceRef(rawRef string) string {
	trimmed := strings.TrimSpace(rawRef)
	if trimmed == "" {
		return rawRef
	}
	if strings.HasPrefix(trimmed, sourceref.IDPrefix) {
		return trimmed
	}
	if !isIPTVRef(trimmed) {
		return rawRef
	}

	// Fail-closed for IPTV if resolver or parser is nil
	if r == nil || r.parser == nil {
		return ""
	}

	src, err := r.parser.Parse(trimmed)
	if err != nil {
		return ""
	}

	if r.reg != nil {
		_ = r.reg.Register(src)
	}

	return string(src.ID())
}

// MaskServiceRefPtr masks an optional outbound *string service reference.
// If ref is nil, it returns nil.
// If *ref is an IPTV reference that fails to parse or is unconfigured, it fails closed and returns nil.
func (r *Resolver) MaskServiceRefPtr(ref *string) *string {
	if ref == nil {
		return nil
	}
	masked := r.MaskServiceRef(*ref)
	if masked == "" && *ref != "" && isIPTVRef(*ref) {
		return nil
	}
	return &masked
}

// MaskLogoURL rewrites a logo or picon URL for IPTV services to /logos/iptv_<id>.png.
// Non-IPTV (DVB) logo URLs are preserved untouched.
// Query parameters (e.g. ?v=...) on the original logoURL are preserved.
func (r *Resolver) MaskLogoURL(logoURL, rawRef string) string {
	trimmedRef := strings.TrimSpace(rawRef)
	if trimmedRef == "" || !isIPTVRef(trimmedRef) {
		return logoURL
	}
	maskedID := r.MaskServiceRef(trimmedRef)
	if maskedID == "" {
		return ""
	}
	query := ""
	if idx := strings.Index(logoURL, "?"); idx != -1 {
		query = logoURL[idx:]
	}
	return fmt.Sprintf("/logos/%s.png%s", maskedID, query)
}

// MaskPiconURL returns /logos/iptv_<id>.png for IPTV or an empty string/untouched for DVB.
func (r *Resolver) MaskPiconURL(rawRef string) string {
	return r.MaskLogoURL("", rawRef)
}

// ResolveOpaqueID resolves an opaque ID (iptv_<id>) to its internal raw reference.
func (r *Resolver) ResolveOpaqueID(opaqueID string) (rawRef string, ok bool) {
	raw, kind, err := r.ResolveInbound("", opaqueID)
	if err != nil || kind != KindOpaque {
		return "", false
	}
	return raw, true
}

func isIPTVRef(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	parts := strings.Split(trimmed, ":")
	return len(parts) > 0 && sourceref.IsIPTVServiceType(parts[0])
}
