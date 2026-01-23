package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	bolt "go.etcd.io/bbolt"
)

// Buckets from session/store/bolt.go
var (
	sessBucketSessions = []byte("b_sessions")
	sessBucketIdempo   = []byte("b_idempo")
	sessBucketLeases   = []byte("b_leases")
)

// MigrateSessions moves session data from Bolt to SQLite.
func MigrateSessions(ctx context.Context, boltDB *bolt.DB, sqliteStore *store.SqliteStore, dryRun bool) (int, string, error) {
	count := 0
	var checksumData [][]byte

	err := boltDB.View(func(tx *bolt.Tx) error {
		// 1. Sessions
		bSessions := tx.Bucket(sessBucketSessions)
		if bSessions != nil {
			// To ensure deterministic checksum, we need stable ordering.
			// Bolt's ForEach is lexicographical by key, so it's already stable.
			err := bSessions.ForEach(func(k, v []byte) error {
				var rec model.SessionRecord
				if err := json.Unmarshal(v, &rec); err != nil {
					return nil // Skip corrupt
				}

				mapped := MapSession(&rec)
				if !dryRun {
					if err := sqliteStore.PutSession(ctx, mapped); err != nil {
						return err
					}
				}

				// Deep content integrity: checksum the mapped record (canonical JSON)
				serialized, _ := json.Marshal(mapped)
				checksumData = append(checksumData, serialized)

				count++
				return nil
			})
			if err != nil {
				return err
			}
		}

		// 2. Leases
		bLeases := tx.Bucket(sessBucketLeases)
		if bLeases != nil {
			err := bLeases.ForEach(func(k, v []byte) error {
				var bRec struct {
					Owner     string    `json:"owner"`
					ExpiresAt time.Time `json:"expires_at"`
				}
				if err := json.Unmarshal(v, &bRec); err != nil {
					return nil
				}

				key, owner, expMs := MapLease(string(k), bRec.Owner, bRec.ExpiresAt)
				if !dryRun {
					query := "INSERT OR REPLACE INTO leases (key, owner, expires_at_ms) VALUES (?, ?, ?)"
					if _, err := sqliteStore.DB.ExecContext(ctx, query, key, owner, expMs); err != nil {
						return err
					}
				}

				// Checksum leases too
				checksumData = append(checksumData, []byte(fmt.Sprintf("%s:%s:%d", key, owner, expMs)))
				return nil
			})
			if err != nil {
				return err
			}
		}

		// 3. Idempotency
		bIdem := tx.Bucket(sessBucketIdempo)
		if bIdem != nil {
			err := bIdem.ForEach(func(k, v []byte) error {
				var bRec struct {
					SessionID string    `json:"sessionId"`
					ExpiresAt time.Time `json:"expires_at"`
				}
				if err := json.Unmarshal(v, &bRec); err != nil {
					return nil
				}

				key, sid, expMs := MapIdempotency(string(k), bRec.SessionID, bRec.ExpiresAt)
				if !dryRun {
					query := "INSERT OR REPLACE INTO idempotency (key, session_id, expires_at_ms) VALUES (?, ?, ?)"
					if _, err := sqliteStore.DB.ExecContext(ctx, query, key, sid, expMs); err != nil {
						return err
					}
				}

				// Checksum idempotency
				checksumData = append(checksumData, []byte(fmt.Sprintf("%s:%s:%d", key, sid, expMs)))
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	return count, CalculateChecksum(checksumData), err
}
