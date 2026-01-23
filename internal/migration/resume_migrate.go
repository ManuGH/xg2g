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
func MigrateResume(ctx context.Context, boltDB *bolt.DB, sqliteStore *resume.SqliteStore, dryRun bool) (int, string, error) {
	count := 0
	var checksumData [][]byte

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

			if !dryRun {
				principal, recording := splitResumeKey(string(k))
				if err := sqliteStore.Put(ctx, principal, recording, &state); err != nil {
					return err
				}
			}

			// Deep content integrity: checksum key + raw value
			checksumData = append(checksumData, k)
			checksumData = append(checksumData, v)

			count++
			return nil
		})
	})

	return count, CalculateChecksum(checksumData), err
}

func splitResumeKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
