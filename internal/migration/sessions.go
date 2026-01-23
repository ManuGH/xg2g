package migration

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// SessionMapper implements explicit semantic mapping for Session records.
// Constraint 3: Bolt seconds -> SQLite ms.
func MapSession(boltRec *model.SessionRecord) *model.SessionRecord {
	if boltRec == nil {
		return nil
	}

	// Create a clone to avoid side-effects on the source
	sqliteRec := *boltRec

	// Semantics: Bolt used seconds for Unix timestamps. SQLite Store (v3) uses ms.
	// We handle this conversion here.
	sqliteRec.CreatedAtUnix = s2ms(boltRec.CreatedAtUnix)
	sqliteRec.UpdatedAtUnix = s2ms(boltRec.UpdatedAtUnix)
	sqliteRec.ExpiresAtUnix = s2ms(boltRec.ExpiresAtUnix)
	sqliteRec.LeaseExpiresAtUnix = s2ms(boltRec.LeaseExpiresAtUnix)

	if boltRec.LastAccessUnix > 0 {
		sqliteRec.LastAccessUnix = s2ms(boltRec.LastAccessUnix)
	}
	if boltRec.LastHeartbeatUnix > 0 {
		sqliteRec.LastHeartbeatUnix = s2ms(boltRec.LastHeartbeatUnix)
	}
	if boltRec.FallbackAtUnix > 0 {
		sqliteRec.FallbackAtUnix = s2ms(boltRec.FallbackAtUnix)
	}

	return &sqliteRec
}

// MapLease handles semantic mapping for Lease records.
func MapLease(key, owner string, exp time.Time) (string, string, int64) {
	return key, owner, exp.UnixMilli()
}

// MapIdempotency handles semantic mapping for Idempotency records.
func MapIdempotency(key, sessionID string, exp time.Time) (string, string, int64) {
	return key, sessionID, exp.UnixMilli()
}

func s2ms(s int64) int64 {
	return s * 1000
}
