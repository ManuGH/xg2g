package migration

import (
	"context"
	"encoding/json"

	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	bolt "go.etcd.io/bbolt"
)

const (
	resumeBucketName = "resume_v1"
)

// MigrateResume moves user playback progress from Bolt to SQLite.
func MigrateResume(ctx context.Context, boltDB *bolt.DB, sqliteStore *resume.SqliteStore, dryRun bool) (int, error) {
	count := 0

	err := boltDB.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket([]byte(resumeBucketName))
		if bkt == nil {
			return nil
		}

		return bkt.ForEach(func(k, v []byte) error {
			var state resume.State
			if err := json.Unmarshal(v, &state); err != nil {
				return nil
			}

			// Semantic Mapping: Resume uses time.Time and int seconds.
			// No unit conversion needed for xg2g 2.0 -> 3.0 resume,
			// but we use the explicit SqliteStore.Put to ensure SQLite format.
			if !dryRun {
				// Parse composite key (principal\x00recording)
				// For the Put method we need the separate IDs.
				// However, SqliteStore.Put takes principalID and recordingID.
				// We'll extract them if we can, or just store them as opaque if the store allows.
				// In sqlite_store.go: Put(ctx, principalID, recordingID, state)

				// Extract IDs
				principal, recording := splitResumeKey(string(k))
				if err := sqliteStore.Put(ctx, principal, recording, &state); err != nil {
					return err
				}
			}
			count++
			return nil
		})
	})

	return count, err
}

func splitResumeKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
