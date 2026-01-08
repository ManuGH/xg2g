// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketSessions  = []byte("b_sessions")
	bucketPipelines = []byte("b_pipelines")
	bucketIdempo    = []byte("b_idempo")
	bucketLeases    = []byte("b_leases")
)

type BoltStore struct {
	db *bolt.DB
}

type boltLease struct {
	store *BoltStore
	key   string
	owner string
	exp   time.Time
}

func (l *boltLease) Key() string          { return l.key }
func (l *boltLease) Owner() string        { return l.owner }
func (l *boltLease) ExpiresAt() time.Time { return l.exp }

// LeaseRecord stored in DB
type leaseRecord struct {
	Owner     string    `json:"owner"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IdemRecord stored in DB
type idemRecord struct {
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func OpenBoltStore(path string) (*BoltStore, error) {
	if path == "" {
		return nil, errors.New("bolt store path required")
	}

	// Ensure directory exists?
	// User Requirement: "nicht automatisch erstellen (klarer Operator contract), aber: wenn directory fehlt -> error"
	// os.MkdirAll(filepath.Dir(path), 0750)
	// We expect the operator to create the data directory.
	// But we should construct the full path if 'path' is a directory.

	info, err := os.Stat(path)
	dbPath := path
	if err == nil && info.IsDir() {
		dbPath = filepath.Join(path, "state.db")
	} else if os.IsNotExist(err) && filepath.Ext(path) == "" {
		// Assume directory if no extension? Or fail?
		// Plan says: "StorePath = Directory, DB Datei = ${StorePath}/state.db"
		// If path doesn't exist, we assume it's the directory that should have existed.
		return nil, fmt.Errorf("store directory does not exist: %s", path)
	}

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open bolt db: %w", err)
	}

	// Initialize buckets
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketSessions, bucketPipelines, bucketIdempo, bucketLeases} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init buckets: %w", err)
	}

	return &BoltStore{db: db}, nil
}

func (b *BoltStore) Close() error {
	return b.db.Close()
}

// --- Session CRUD ---

func (b *BoltStore) PutSession(ctx context.Context, s *model.SessionRecord) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		val, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketSessions).Put([]byte(s.SessionID), val)
	})
}

func (b *BoltStore) DeleteSession(ctx context.Context, id string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSessions).Delete([]byte(id))
	})
}

func (b *BoltStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) (string, bool, error) {
	var existingID string
	var exists bool

	err := b.db.Update(func(tx *bolt.Tx) error {
		// 1. Idempotency Check (Guard)
		if idemKey != "" {
			bucket := tx.Bucket(bucketIdempo)
			existing := bucket.Get([]byte(idemKey))
			if existing != nil {
				var rec idemRecord
				if err := json.Unmarshal(existing, &rec); err == nil {
					if time.Now().Before(rec.ExpiresAt) {
						// Valid idempotency key exists.
						existingID = rec.SessionID
						exists = true
						return nil
					}
				}
			}
		}

		// 2. Write Session
		sessionBytes, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketSessions).Put([]byte(s.SessionID), sessionBytes); err != nil {
			return err
		}

		// 3. Write Idempotency
		if idemKey != "" {
			rec := idemRecord{
				SessionID: s.SessionID,
				ExpiresAt: time.Now().Add(ttl),
			}
			idemBytes, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			if err := tx.Bucket(bucketIdempo).Put([]byte(idemKey), idemBytes); err != nil {
				return err
			}
		}

		return nil
	})

	return existingID, exists, err
}

func (b *BoltStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	var rec model.SessionRecord
	var found bool
	err := b.db.View(func(tx *bolt.Tx) error {
		val := tx.Bucket(bucketSessions).Get([]byte(id))
		if val == nil {
			return nil // Not Found
		}
		found = true
		return json.Unmarshal(val, &rec)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &rec, nil
}

func (b *BoltStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	var updated *model.SessionRecord
	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketSessions)
		val := bkt.Get([]byte(id))
		if val == nil {
			return ErrNotFound
		}

		var rec model.SessionRecord
		if err := json.Unmarshal(val, &rec); err != nil {
			return err
		}

		// Apply update
		if err := fn(&rec); err != nil {
			return err
		}

		// Write back
		newVal, err := json.Marshal(&rec)
		if err != nil {
			return err
		}
		if err := bkt.Put([]byte(id), newVal); err != nil {
			return err
		}
		updated = &rec
		return nil
	})
	return updated, err
}

func (b *BoltStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	var list []*model.SessionRecord
	err := b.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketSessions).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var rec model.SessionRecord
			if err := json.Unmarshal(v, &rec); err == nil {
				list = append(list, &rec)
			}
		}
		return nil
	})
	return list, err
}

// QuerySessions filters sessions (ADR-009 CTO Patch 2)
// Note: BoltStore implementation uses ListSessions + filter for now
// TODO: Optimize with index/cursor filtering in future
func (b *BoltStore) QuerySessions(ctx context.Context, filter SessionFilter) ([]*model.SessionRecord, error) {
	all, err := b.ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	// Build state map
	stateMatch := make(map[model.SessionState]bool)
	for _, state := range filter.States {
		stateMatch[state] = true
	}

	var result []*model.SessionRecord
	for _, rec := range all {
		// Filter by state
		if len(filter.States) > 0 && !stateMatch[rec.State] {
			continue
		}

		// Filter by lease expiry
		if filter.LeaseExpiresBefore > 0 && rec.LeaseExpiresAtUnix > filter.LeaseExpiresBefore {
			continue
		}

		result = append(result, rec)
	}

	return result, nil
}

func (b *BoltStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	return b.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketSessions).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var rec model.SessionRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				log.L().Warn().Str("key", string(k)).Err(err).Msg("failed to unmarshal session during scan")
				continue
			}
			if err := fn(&rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Idempotency ---

func (b *BoltStore) PutIdempotency(ctx context.Context, key, sessionID string, ttl time.Duration) error {
	// Standalone put only for cases outside atomic intent creation (rare?)
	// Or maybe for renewal?
	return b.db.Update(func(tx *bolt.Tx) error {
		rec := idemRecord{
			SessionID: sessionID,
			ExpiresAt: time.Now().Add(ttl),
		}
		val, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketIdempo).Put([]byte(key), val)
	})
}

func (b *BoltStore) GetIdempotency(ctx context.Context, key string) (string, bool, error) {
	var sessionID string
	var found bool
	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketIdempo)
		val := bkt.Get([]byte(key))
		if val == nil {
			return nil
		}
		var rec idemRecord
		if err := json.Unmarshal(val, &rec); err != nil {
			return nil // Cast as miss if corrupt?
		}
		if time.Now().After(rec.ExpiresAt) {
			// Expired: Check lazy delete
			_ = bkt.Delete([]byte(key))
			return nil
		}
		sessionID = rec.SessionID
		found = true
		return nil
	})
	return sessionID, found, err
}

// --- LEASES ---

func (b *BoltStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	var acquired bool
	var exp time.Time

	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketLeases)
		val := bkt.Get([]byte(key))
		now := time.Now()

		var rec leaseRecord
		if val != nil {
			if err := json.Unmarshal(val, &rec); err == nil {
				if now.Before(rec.ExpiresAt) && rec.Owner != owner {
					// Lease held by someone else
					// DEBUG: Log mismatch
					log.L().Debug().Str("key", key).Str("held_by", rec.Owner).Str("req_owner", owner).Msg("bolt: lease contention")
					return nil
				}
				// If lease expired or owned by us, we take it/renew it
			}
		}

		// Acquire
		exp = now.Add(ttl)
		newRec := leaseRecord{
			Owner:     owner,
			ExpiresAt: exp,
		}
		bytes, err := json.Marshal(newRec)
		if err != nil {
			return err
		}
		if err := bkt.Put([]byte(key), bytes); err != nil {
			return err
		}
		acquired = true
		return nil
	})

	if err != nil {
		return nil, false, err
	}
	if !acquired {
		return nil, false, nil
	}

	return &boltLease{store: b, key: key, owner: owner, exp: exp}, true, nil
}

func (b *BoltStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	var renewed bool
	var exp time.Time

	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketLeases)
		val := bkt.Get([]byte(key))
		if val == nil {
			return nil // Lost lease
		}
		var rec leaseRecord
		if err := json.Unmarshal(val, &rec); err != nil {
			return err // Corrupt
		}
		if rec.Owner != owner {
			return nil // Stolen
		}
		// Must-Fix: If expired, do NOT renew. Force recovery to take over.
		if time.Now().After(rec.ExpiresAt) {
			return nil
		}
		// Valid owner, renew
		exp = time.Now().Add(ttl)
		rec.ExpiresAt = exp
		bytes, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if err := bkt.Put([]byte(key), bytes); err != nil {
			return err
		}
		renewed = true
		return nil
	})

	if err != nil {
		return nil, false, err
	}
	if !renewed {
		return nil, false, nil
	}

	return &boltLease{store: b, key: key, owner: owner, exp: exp}, true, nil
}

func (b *BoltStore) ReleaseLease(ctx context.Context, key, owner string) error {
	return b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketLeases)
		val := bkt.Get([]byte(key))
		if val == nil {
			return nil
		}
		var rec leaseRecord
		if err := json.Unmarshal(val, &rec); err != nil {
			return nil
		}
		if rec.Owner == owner {
			return bkt.Delete([]byte(key))
		}
		// Not owner, no-op (or error if strictly desired, but no-op safest for generic release)
		return nil
	})
}

func (b *BoltStore) DeleteAllLeases(ctx context.Context) (int, error) {
	var count int
	err := b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketLeases)
		if bkt != nil {
			count = bkt.Stats().KeyN
			// Drop and Recreate bucket is fastest
			if err := tx.DeleteBucket(bucketLeases); err != nil {
				return err
			}
		}
		_, err := tx.CreateBucket(bucketLeases)
		return err
	})
	return count, err
}
