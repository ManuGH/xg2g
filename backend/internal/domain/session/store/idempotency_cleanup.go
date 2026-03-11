package store

import "context"

// IdempotencyCleaner supports compare-and-delete cleanup for stale replay mappings.
type IdempotencyCleaner interface {
	DeleteIdempotencyIfMatch(ctx context.Context, idemKey, sessionID string) (bool, error)
}
