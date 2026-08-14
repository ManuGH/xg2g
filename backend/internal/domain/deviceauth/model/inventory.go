// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

// UnboundInventory is the Phase 0 census defined by ADR-032.
//
// Every device in the deviceauth store is cryptographically unbound by
// construction: the schema has no JWK and no thumbprint column, so there is
// nothing to distinguish a bound device from an unbound one. The census is
// therefore a straight count of what exists, and needs no schema change — which
// is precisely why it can run before any migration begins.
//
// The numbers exist to answer one question before anything is switched off: how
// many real devices would a cutoff affect, and when were they last used.
type UnboundInventory struct {
	// Devices not revoked.
	Devices int `json:"devices"`
	// Distinct owners across those devices.
	Owners int `json:"owners"`
	// Grants that are neither revoked nor expired — these are what a cutoff
	// would invalidate.
	ActiveGrants int `json:"activeGrants"`
	// Access sessions that are neither revoked nor expired.
	ActiveSessions int `json:"activeSessions"`
	// Epoch millis of the oldest still-valid grant, or 0 when there is none.
	OldestGrantIssuedAtMs int64 `json:"oldestGrantIssuedAtMs,omitempty"`
	// Epoch millis of the most recent use of any still-valid grant, or 0 when
	// none has ever been used. A fleet whose newest use is months old is a very
	// different cutoff decision from one in daily use.
	NewestGrantLastUsedAtMs int64 `json:"newestGrantLastUsedAtMs,omitempty"`
}

// IsEmpty reports whether a cutoff would affect nothing.
func (i UnboundInventory) IsEmpty() bool {
	return i.Devices == 0 && i.ActiveGrants == 0 && i.ActiveSessions == 0
}
